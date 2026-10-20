import { expect, mock, test } from 'claude-code/testing'
import { parseRun, summarizeRun } from './livedoc.ts'
import { mdToHtml, runPage } from './runhtml.ts'
import { fitParts, MAX_CHARS } from './runview.tsx'
import { serial } from './runstate.ts'
import { buildWrapper, runDirPath } from './wrap.ts'

// /codex-runs as the person types it
const RUNS_CMD = { command: 'codex-runs', args: '', origin: { kind: 'composer' }, presentation: { isFullscreen: false, columns: 80 } } as const
const answer = { result: { stdout: '', stderr: '', interrupted: false } }
const HOME = '/home/u'
const CLEAN = [
  '### Assistant', '', 'I will list the files.', '',
  '<!-- fold closed: Bash: ls · exit 0 · 2 lines -->', '**Bash** `ls`', '', '```', 'a', 'b', '```', '<!-- /fold -->', '',
  '<!-- fold open: Bash: cat x · exit 1 · 1 line -->', '**Bash** `cat x`', '', '> **error** exit 1', '', '```', 'nope', '```', '<!-- /fold -->', '',
].join('\n')
const dirOf = (command: string) => runDirPath(HOME, /codex-herdr-run\.([A-Za-z0-9]+)\.sh/.exec(command)?.[1] ?? '')!

type Opts = { interrupted?: boolean; raceTick?: boolean; placed?: boolean; ran?: (dir: string) => Record<string, string>; surfaces?: string[]; gate?: Promise<unknown>; deny?: boolean; background?: boolean }

// A delegation through the mod: the files the run writes are `files`; panes opened and closed and
// writes of run.html are recorded. `ran` is what the run left when the call returns; `gate` holds
// the call open (an approval prompt).
async function delegate($: any, on: any, o: Opts = {}) {
  const clock = mock.clock(on)
  mock.env(on, { TMPDIR: '/tmp/t', HOME })
  const files: Record<string, string> = {}
  const opened: string[] = [], closed: string[] = [], pages: string[] = []
  on('fs.write', {}, (_$: unknown, e: { path: string; text: string }) => {
    if (e.path.endsWith('/run.html')) pages.push(e.path)
    files[e.path] = e.text
    return { value: undefined }
  })
  on('fs.exists', {}, (_$: unknown, e: { path: string }) => ({ value: e.path in files }))
  on('fs.read', {}, (_$: unknown, e: { path: string }) => {
    if (!(e.path in files)) throw new Error('ENOENT')
    return { value: files[e.path] }
  })
  on('fs.stat', {}, (_$: unknown, e: { path: string }) => {
    if (!(e.path in files)) throw new Error('ENOENT')
    return { value: { kind: 'file', size: files[e.path]!.length, mtimeMs: 0, isLink: false } }
  })
  on('ui.open', {}, (_$: unknown, e: { id: string }) => {
    opened.push(e.id)
    return { value: o.placed === false ? { isPlaced: false, reason: 'no-surface: no surface is attached' } : { isPlaced: true } }
  })
  on('ui.close', {}, (_$: unknown, e: { id: string }) => { closed.push(e.id); return { value: undefined } })
  on('session.surfaces', {}, () => ({ value: o.surfaces ?? ['terminal'] }))
  let command = ''
  on('tool.call', { tool: 'Bash' }, async (_$: unknown, e: { command: string }) => {
    command = e.command
    if (o.gate) await o.gate
    if (o.deny) return { deny: 'refused' }
    if (o.ran) Object.assign(files, o.ran(dirOf(command)))
    if (o.raceTick) { void clock.advance(700); void clock.advance(700) } // poll ticks as the call returns
    return o.interrupted ? { result: { ...answer.result, interrupted: true } } : answer
  })
  const call = $.tool.call({ tool: 'Bash', command: 'codex exec "list the files"', ...(o.background ? { run_in_background: true } : {}) })
  const result = o.gate ? undefined : await call
  return { clock, files, opened, closed, pages, call, result, cmd: () => command, dir: () => dirOf(command), id: () => /codex-herdr-run\.([A-Za-z0-9]+)\.sh/.exec(command)?.[1] ?? '' }
}

const PANE = (id: string, surface: 'terminal' | 'desktop' | 'vscode' | 'mobile') => ({
  plugin: 'codex-herdr-pane', component: 'Pane', requestId: `codex-run-${id}`, surface,
  props: { title: 'cdx', isFocused: false, bodyColumns: 80, placement: 'dock', scroll: { offset: 0, bodyRows: 40 }, view: {} },
}) as const
const md = async (ui: any) => (await ui.findAll({ type: 'Markdown' })).map((m: any) => String(m.props.text))
const note = (r: unknown) => ((r as { context?: string[] })?.context ?? []).join('\n')

test('a run outside herdr is followed in a Claude Code pane, with folds and the answer; it outlives its pane', { options: { claudePane: 'auto', closeDelay: 3 } }, async ($, on) => {
  const d = await delegate($, on)
  const dir = d.dir(), id = d.id()
  expect(dir.startsWith(`${HOME}/.cache/codex-herdr-pane/runs/`)).toBe(true)
  const script = d.files[`/tmp/t/codex-herdr-run.${id}.sh`] ?? ''
  expect(script).toContain(`__D='${dir}'; { mkdir -p "$__R" && chmod 700 "$__R" && mkdir -m 700 "$__D"; }`)
  expect(script).toContain('echo claude > "$__D/host"')
  await d.clock.advance(800)
  expect(d.opened).toEqual([])
  Object.assign(d.files, { [`${dir}/host`]: 'claude\n', [`${dir}/label`]: 'cdx: list the files', [`${dir}/start`]: String(Math.floor(d.clock.now() / 1000)), [`${dir}/clean.md`]: CLEAN })
  await d.clock.advance(1500)
  expect(d.opened).toEqual([`codex-run-${id}`])
  for (const surface of ['terminal', 'desktop', 'vscode', 'mobile'] as const) {
    const ui = await $.ui.mount(PANE(id, surface))
    const texts = await md(ui)
    expect(texts.some((t: string) => t.includes('I will list the files.'))).toBe(true)
    expect(texts.some((t: string) => t.includes('nope'))).toBe(true) // the failed result starts open
    expect(texts.some((t: string) => t.includes('\na\nb\n'))).toBe(false) // the passing one is closed
    expect(texts.some((t: string) => t.includes('**Bash**'))).toBe(false)
    await ui.unmount()
  }
  const ui = await $.ui.mount(PANE(id, 'terminal'))
  await ui.press({ key: 'fold-1' })
  expect((await md(ui)).some((t: string) => t.includes('a\nb'))).toBe(true)
  await ui.press({ key: 'folds' })
  expect((await md(ui)).some((t: string) => t.includes('nope'))).toBe(false)
  await ui.unmount()
  Object.assign(d.files, { [`${dir}/exit`]: '0', [`${dir}/end`]: String(Math.floor(d.clock.now() / 1000) + 5), [`${dir}/usage`]: 'input 10, output 5', [`${dir}/last.md`]: '# Answer\n\nthe files are a and b\n' })
  await d.clock.advance(800)
  const done = await $.ui.mount(PANE(id, 'desktop'))
  expect((await done.find({ type: 'Text', text: /finished in/ }))?.text).toContain('input 10, output 5')
  expect((await md(done)).some((t: string) => t.includes('the files are a and b'))).toBe(true)
  await done.unmount()
  expect(d.pages).toEqual([`${dir}/run.html`])
  await d.clock.advance(3100)
  expect(d.closed).toEqual([`codex-run-${id}`])
  expect(String((await $.command.run(RUNS_CMD) as { text?: string }).text)).toContain('Showing cdx: list the files') // still followed
})

test('a run herdr shows opens no Claude Code pane; only its end is read, for the page', { options: { claudePane: 'auto' } }, async ($, on) => {
  const d = await delegate($, on)
  const dir = d.dir()
  Object.assign(d.files, { [`${dir}/host`]: 'herdr\n', [`${dir}/clean.md`]: CLEAN })
  await d.clock.advance(1500)
  expect(String((await $.command.run(RUNS_CMD) as { text?: string }).text)).toContain('is running in its herdr pane')
  Object.assign(d.files, { [`${dir}/exit`]: '0', [`${dir}/last.md`]: 'done' })
  await d.clock.advance(1500)
  expect(d.opened).toEqual([])
  expect(d.files[`${dir}/run.html`]).toContain('<details class="failed" open><summary>Bash: cat x · exit 1 · 1 line</summary>')
})

const FINISHED = (dir: string) => ({ [`${dir}/host`]: 'herdr\n', [`${dir}/label`]: 'cdx: list the files', [`${dir}/clean.md`]: CLEAN,
  [`${dir}/exit`]: '0', [`${dir}/start`]: '100', [`${dir}/end`]: '130', [`${dir}/last.md`]: '# Done\n\nall good' })

test('a foreground call where nothing draws panes returns with its page written once and a note to send it', async ($, on) => {
  const d = await delegate($, on, { ran: FINISHED, surfaces: [] })
  expect(d.files[`${d.dir()}/run.html`]).toContain('<h1>cdx: list the files</h1>')
  expect(note(d.result)).toContain(`show the run to the user through its page, ${d.dir()}/run.html`)
  await d.clock.advance(2000)
  expect(d.pages.length).toBe(1) // the poller and the call's settle never both write it
})

test('the poller and the call\'s own settle racing on one run finish it once: one page, one close', { options: { claudePane: 'auto', closeDelay: 0 } }, async ($, on) => {
  const d = await delegate($, on, { ran: (dir) => ({ ...FINISHED(dir), [`${dir}/host`]: 'claude\n' }), raceTick: true, surfaces: [] })
  await d.clock.advance(2000)
  expect(d.pages.length).toBe(1)
  expect(d.closed.length).toBe(1)
  expect(note(d.result)).toContain('/run.html')
})

test('serial: work on one key never overlaps and runs in call order; other keys run alongside', async () => {
  const log: string[] = []
  let live = 0, most = 0
  const work = (name: string, ticks: number) => async () => {
    live++; most = Math.max(most, live); log.push(`start ${name}`)
    for (let i = 0; i < ticks; i++) await Promise.resolve()
    log.push(`end ${name}`); live--
  }
  await Promise.all([serial('a', work('a1', 5)), serial('a', work('a2', 1)), serial('a', work('a3', 3))])
  expect(most).toBe(1)
  expect(log).toEqual(['start a1', 'end a1', 'start a2', 'end a2', 'start a3', 'end a3'])
  log.length = 0; most = 0
  await Promise.all([serial('b', work('b1', 5)), serial('c', work('c1', 5))])
  expect(most).toBe(2)
  await serial('d', async () => { throw new Error('x') }).catch(() => {})
  await serial('d', work('d2', 1)) // a failed one does not block the next
  expect(log.at(-1)).toBe('end d2')
})

test('a foreground call cut off by its timeout leaves its herdr run followed to its real end', async ($, on) => {
  const d = await delegate($, on, { interrupted: true, surfaces: [], ran: (dir) => ({ [`${dir}/host`]: 'herdr\n', [`${dir}/clean.md`]: CLEAN }) })
  expect(note(d.result)).toContain(`its page is ${d.dir()}/run.html`) // no report will name it
  expect(note(d.result)).not.toContain('"run page:" line')
  await d.clock.advance(1500)
  expect(`${d.dir()}/run.html` in d.files).toBe(false) // not ended as gone
  Object.assign(d.files, { [`${d.dir()}/exit`]: '0', [`${d.dir()}/last.md`]: 'the real answer' })
  await d.clock.advance(1500)
  expect(d.files[`${d.dir()}/run.html`]).toContain('the real answer')
  expect(d.files[`${d.dir()}/run.html`]).not.toContain('ended without an exit status')
})

test('where a surface draws panes no note is added (the report names the page)', async ($, on) => {
  const d = await delegate($, on, { ran: FINISHED, surfaces: ['terminal'] })
  expect(d.files[`${d.dir()}/run.html`]).toBeDefined()
  expect(note(d.result)).toBe('')
})

test('a background call where nothing draws panes is told how to show a page, without a promise', async ($, on) => {
  const d = await delegate($, on, { background: true, surfaces: [] })
  expect(note(d.result)).toContain('If this delegation\'s report ends with a "run page:" line')
})

test('a bare run (outside herdr, claudePane off) adds no note and is dropped once its call returned', async ($, on) => {
  const d = await delegate($, on, { surfaces: [] })
  expect(note(d.result)).toBe('')
  await d.clock.advance(31000)
  expect(String((await $.command.run(RUNS_CMD) as { text?: string }).text)).toContain('No delegation run is followed')
})

test('a refused call is dropped at once', { options: { claudePane: 'auto' } }, async ($, on) => {
  const d = await delegate($, on, { deny: true })
  Object.assign(d.files, { [`${d.dir()}/host`]: 'claude\n' })
  await d.clock.advance(1500)
  expect(d.opened).toEqual([])
})

test('a call approved after more than a minute is still followed', { options: { claudePane: 'auto' } }, async ($, on) => {
  let approve = () => {}
  const d = await delegate($, on, { gate: new Promise<void>((r) => { approve = r }) })
  await d.clock.advance(90000) // at the approval prompt
  Object.assign(d.files, { [`${d.dir()}/host`]: 'claude\n', [`${d.dir()}/clean.md`]: CLEAN })
  await d.clock.advance(800)
  expect(d.opened).toEqual([`codex-run-${d.id()}`])
  approve()
  await d.call
})

test('a run whose log stops growing for 30 minutes is ended as gone, with its page', { options: { claudePane: 'auto', closePane: 'never' } }, async ($, on) => {
  const d = await delegate($, on)
  Object.assign(d.files, { [`${d.dir()}/host`]: 'claude\n', [`${d.dir()}/clean.md`]: CLEAN })
  await d.clock.advance(1500)
  await d.clock.advance(31 * 60000)
  expect(d.files[`${d.dir()}/run.html`]).toContain('ended without an exit status')
})

test('the wrapper: private run directory, herdr host only once a pane exists, an early close leaves an exit, the page line', () => {
  const o = {
    direction: 'right', layout: 'tile', maxPanes: 6, closePane: 'always', closeDelay: 3, tailLines: 50, viewer: 'textual',
    leanProfile: false, leanProfileName: 'lean', root: '/mod', id: 'x1', runDir: '/home/u/.cache/codex-herdr-pane/runs/x1',
  } as const
  const off = buildWrapper('codex exec "x"', o)
  expect(off).toContain(`__R='/home/u/.cache/codex-herdr-pane/runs'; __D='/home/u/.cache/codex-herdr-pane/runs/x1'`)
  expect(off.indexOf('if [ -z "$__P" ]; then exec bash "$__D/orig.sh"; fi')).toBeLessThan(off.indexOf('echo herdr > "$__D/host"'))
  expect(off.indexOf('then exec bash "$__D/orig.sh"; fi')).toBeLessThan(off.indexOf('echo herdr > "$__D/host"'))
  expect(off).toContain('date +%s > "$__D/end"; echo 130 > "$__D/exit"; exit 130; }')
  expect(off).toContain('[ -f "$__D/run.html" ] && echo "run page: $__D/run.html"')
  expect(off).toContain('-mindepth 1 -maxdepth 1 -type d -mtime +"$K" -exec rm -rf {} +') // the runs folder is swept
  const on = buildWrapper('codex exec "x"', { ...o, claudePane: true })
  expect(on).toContain(`bash "$__D/pane.sh" "$__D" '/mod' none 0 </dev/null >/dev/null 2>&1`)
  expect(on.indexOf('echo claude > "$__D/host"')).toBeLessThan(on.indexOf('herdr pane split'))
  expect(runDirPath(undefined, 'x')).toBeUndefined()
})

test('runPage escapes everything from the run and opens on the answer only when it succeeded', () => {
  const evil = '<script>alert(1)</script> "x" & <img src=x onerror=1>'
  const html = runPage({ label: evil, status: evil, ok: false, answer: evil,
    parts: [{ kind: 'prose', text: evil }, { kind: 'fold', title: evil, body: '```\n' + evil + '\n```', failed: true }], logPath: '/d/<x>' })
  expect(html).not.toContain('<script>alert')
  expect(html).not.toContain('<img')
  expect(html.match(/<script>/g)?.length).toBe(1)
  expect(html).toContain('data-first="live"')
  expect(runPage({ label: 'l', status: 's', ok: true, answer: 'a', parts: [], logPath: '/d' })).toContain('data-first="answer"')
})

test('mdToHtml: emphasis outside code spans only; the pane\'s fence rule', () => {
  expect(mdToHtml('**b** `c` *i*\n\n- one\n\n> q\n\n```\n<x>\n```')).toBe('<p><strong>b</strong> <code>c</code> <em>i</em></p>\n<ul><li>one</li></ul>\n<blockquote>q</blockquote>\n<pre>&lt;x&gt;</pre>')
  expect(mdToHtml('run `glob **/*.go` and `a*b*c`')).toBe('<p>run <code>glob **/*.go</code> and <code>a*b*c</code></p>')
  expect(mdToHtml('```js``` is the tag\n\nnext')).toBe('<p><code>js</code> is the tag</p>\n<p>next</p>') // a backtick in the info: no fence, a code span
  expect(mdToHtml('``a `b` c`` d')).toBe('<p><code>a `b` c</code> d</p>')
  expect(mdToHtml('````\n```\nin\n````\nout')).toBe('<pre>```\nin</pre>\n<p>out</p>')
})

test('fitParts: the newest that fit; one part larger than a drawing is cut to its end', () => {
  const big = [{ kind: 'prose' as const, text: Array.from({ length: 30000 }, (_, i) => `line ${i}`).join('\n') }]
  const { first, shown } = fitParts(big, () => false)
  expect(first).toBe(0)
  const t = shown[0]!.kind === 'prose' ? shown[0]!.text : ''
  expect(t.length).toBeLessThanOrEqual(MAX_CHARS)
  expect(t.startsWith('*... earlier text cut*')).toBe(true)
  expect(t.endsWith('line 29999')).toBe(true)
})

test('parseRun: prose and folds, markers in fences are text, an unclosed fold waits', () => {
  const src = '### A\n\nhi\n\n<!-- fold closed: T · exit 0 · 1 line -->\n**Bash** `t`\n\n```\n<!-- /fold -->\n```\n<!-- /fold -->\n\n```\n<!-- fold closed: fake -->\n```\n'
  const parts = parseRun(src)
  expect(parts.map((p) => p.kind)).toEqual(['prose', 'fold', 'prose'])
  expect(parts[1]!.kind === 'fold' && parts[1]!.body).toBe('```\n<!-- /fold -->\n```')
  const open = '<!-- fold closed: U · exit 0 · 1 line -->\n```\nz\n```\n'
  expect(parseRun(open)).toEqual([])
  expect(parseRun(open, true).map((p) => p.kind)).toEqual(['fold'])
  expect(parseRun('one\ntw').map((p) => p.kind === 'prose' && p.text)).toEqual(['one'])
})

test('/codex-runs puts the run in the chat when no surface draws panes', { options: { claudePane: 'auto', closePane: 'never' } }, async ($, on) => {
  const d = await delegate($, on, { placed: false })
  const dir = d.dir()
  Object.assign(d.files, { [`${dir}/host`]: 'claude\n', [`${dir}/label`]: 'cdx: list the files', [`${dir}/clean.md`]: CLEAN,
    [`${dir}/exit`]: '1', [`${dir}/end`]: String(Math.floor(d.clock.now() / 1000)), [`${dir}/last.md`]: '' })
  await d.clock.advance(1600)
  const text = String((await $.command.run(RUNS_CMD) as { text?: string }).text ?? '')
  expect(text).toContain('cdx: list the files: failed with exit 1')
  expect(text).toContain('✗ Bash: cat x · exit 1 · 1 line\n    nope')
  expect(text).toContain(`As a page with collapsible tool results: ${dir}/run.html`)
  expect(text).not.toContain('<!--')
})

test('/codex-runs with no run followed says so', { options: { claudePane: 'auto' } }, async ($) => {
  expect(String((await $.command.run(RUNS_CMD) as { text?: string }).text)).toContain('No delegation run is followed')
})

test('summarizeRun: text, one line per result, failed output cut, the answer last, the newest kept', () => {
  const text = summarizeRun(parseRun(CLEAN, true), 'head', 'the answer', '/d/clean.md')
  expect(text.startsWith('head\n\nI will list the files.\n\n✓ Bash: ls')).toBe(true)
  expect(text).toContain('--- answer ---\nthe answer\n\nFull log: /d/clean.md')
  const long = parseRun(Array.from({ length: 400 }, (_, i) => `<!-- fold closed: Bash: step ${i} · exit 0 · 1 line -->\n\`\`\`\nx\n\`\`\`\n<!-- /fold -->\n`).join(''), true)
  const cut = summarizeRun(long, 'h', '', '/d')
  expect(cut).toContain('... (earlier steps cut)')
  expect(cut).toContain('step 399')
})

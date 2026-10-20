import { expect, mock, test } from 'claude-code/testing'
import { parseRun, summarizeRun } from './livedoc.ts'
import { mdToHtml, runPage } from './runhtml.ts'
import { buildWrapper, runDirPath } from './wrap.ts'

// /codex-runs as the person types it
const RUNS_CMD = { command: 'codex-runs', args: '', origin: { kind: 'composer' }, presentation: { isFullscreen: false, columns: 80 } } as const
const answer = { result: { stdout: '', stderr: '', interrupted: false } }
const TMP = '/tmp/t'
const CLEAN = [
  '### Assistant', '', 'I will list the files.', '',
  '<!-- fold closed: Bash: ls · exit 0 · 2 lines -->', '**Bash** `ls`', '', '```', 'a', 'b', '```', '<!-- /fold -->', '',
  '<!-- fold open: Bash: cat x · exit 1 · 1 line -->', '**Bash** `cat x`', '', '> **error** exit 1', '', '```', 'nope', '```', '<!-- /fold -->', '',
].join('\n')

// A delegation through the mod with claudePane on: the files the run writes are `files`, the
// panes opened and closed are recorded.
async function delegate($: any, on: any, placed = true, ran?: (dir: string) => Record<string, string>, surfaces: string[] = ['terminal']) {
  const clock = mock.clock(on)
  mock.env(on, { TMPDIR: TMP })
  const files: Record<string, string> = {}
  const opened: string[] = [], closed: string[] = []
  on('fs.write', {}, (_$: unknown, e: { path: string; text: string }) => { files[e.path] = e.text; return { value: undefined } })
  on('fs.exists', {}, (_$: unknown, e: { path: string }) => ({ value: e.path in files }))
  on('fs.read', {}, (_$: unknown, e: { path: string }) => {
    if (!(e.path in files)) throw new Error('ENOENT')
    return { value: files[e.path] }
  })
  on('ui.open', {}, (_$: unknown, e: { id: string }) => {
    opened.push(e.id)
    return { value: placed ? { isPlaced: true } : { isPlaced: false, reason: 'no-surface: no surface is attached' } }
  })
  on('ui.close', {}, (_$: unknown, e: { id: string }) => { closed.push(e.id); return { value: undefined } })
  on('session.surfaces', {}, () => ({ value: surfaces }))
  let command = ''
  on('tool.call', { tool: 'Bash' }, (_$: unknown, e: { command: string }) => {
    command = e.command
    const runId = /codex-herdr-run\.([A-Za-z0-9]+)\.sh/.exec(command)?.[1] ?? ''
    if (ran) Object.assign(files, ran(runDirPath(TMP, runId))) // what the run left when the call returns
    return answer
  })
  const result = await $.tool.call({ tool: 'Bash', command: 'codex exec "list the files"' })
  const id = /codex-herdr-run\.([A-Za-z0-9]+)\.sh/.exec(command)?.[1] ?? ''
  const dir = runDirPath(TMP, id)
  return { clock, files, opened, closed, id, dir, command, result }
}

const PANE = (id: string, surface: 'terminal' | 'desktop' | 'vscode' | 'mobile') => ({
  plugin: 'codex-herdr-pane', component: 'Pane', requestId: `codex-run-${id}`, surface,
  props: { title: 'Codex task', isFocused: false, bodyColumns: 80, placement: 'dock', scroll: { offset: 0, bodyRows: 40 }, view: {} },
}) as const

test('a run outside herdr is followed in a Claude Code pane, with folds and the answer', { options: { claudePane: 'auto', closeDelay: 3 } }, async ($, on) => {
  const { clock, files, opened, closed, id, dir, command } = await delegate($, on)
  expect(id).not.toBe('')
  const script = Object.entries(files).find(([p]) => p.endsWith(`codex-herdr-run.${id}.sh`))?.[1] ?? ''
  expect(script).toContain(`__D='${dir}'; mkdir -m 700 "$__D"`)
  expect(script).toContain('echo claude > "$__D/host"')
  expect(command).toContain('exec bash')

  await clock.advance(800)
  expect(opened).toEqual([]) // no host file yet: the script has not run
  Object.assign(files, { [`${dir}/host`]: 'claude\n', [`${dir}/label`]: 'Codex task: list the files', [`${dir}/start`]: String(Math.floor(clock.now() / 1000)), [`${dir}/clean.md`]: CLEAN })
  await clock.advance(800)
  expect(opened).toEqual([`codex-run-${id}`])
  await clock.advance(800)

  for (const surface of ['terminal', 'desktop', 'vscode', 'mobile'] as const) {
    const ui = await $.ui.mount(PANE(id, surface))
    expect((await ui.find({ type: 'Text', text: /running for/ }))).toBeDefined()
    const prose = await ui.findAll({ type: 'Markdown' })
    expect(prose.some((m) => String(m.props.text).includes('I will list the files.'))).toBe(true)
    expect(prose.some((m) => String(m.props.text).includes('nope'))).toBe(true) // the failed result starts open
    expect(prose.some((m) => String(m.props.text).includes('\na\nb\n'))).toBe(false) // the passing one is closed
    expect(prose.some((m) => String(m.props.text).includes('**Bash**'))).toBe(false) // the label repeating the title is hidden
    await ui.unmount()
  }

  const ui = await $.ui.mount(PANE(id, 'terminal'))
  await ui.press({ key: 'fold-1' })
  expect((await ui.findAll({ type: 'Markdown' })).some((m) => String(m.props.text).includes('a\nb'))).toBe(true)
  await ui.press({ key: 'folds' }) // all were open: closes every fold
  expect((await ui.findAll({ type: 'Markdown' })).some((m) => String(m.props.text).includes('nope'))).toBe(false)
  await ui.unmount()

  Object.assign(files, { [`${dir}/exit`]: '0', [`${dir}/end`]: String(Math.floor(clock.now() / 1000) + 5), [`${dir}/usage`]: 'input 10, output 5', [`${dir}/last.md`]: '# Answer\n\nthe files are a and b\n' })
  await clock.advance(800)
  const done = await $.ui.mount(PANE(id, 'desktop'))
  expect((await done.find({ type: 'Text', text: /finished in/ }))?.text).toContain('input 10, output 5')
  expect((await done.findAll({ type: 'Markdown' })).some((m) => String(m.props.text).includes('the files are a and b'))).toBe(true) // Answer shown on success
  await done.press({ key: 'tab-live' })
  expect((await done.findAll({ type: 'Markdown' })).some((m) => String(m.props.text).includes('I will list the files.'))).toBe(true)
  await done.unmount()

  await clock.advance(3100)
  expect(closed).toEqual([`codex-run-${id}`])
})

test('a run herdr shows opens no Claude Code pane; its page is written when it ends', { options: { claudePane: 'auto' } }, async ($, on) => {
  const { clock, files, opened, dir } = await delegate($, on)
  Object.assign(files, { [`${dir}/host`]: 'herdr\n', [`${dir}/clean.md`]: CLEAN })
  await clock.advance(1500)
  expect(`${dir}/run.html` in files).toBe(false)
  Object.assign(files, { [`${dir}/exit`]: '0', [`${dir}/last.md`]: 'done' })
  await clock.advance(1500)
  expect(opened).toEqual([])
  expect(files[`${dir}/run.html`]).toContain('<details class="failed" open><summary>Bash: cat x · exit 1 · 1 line</summary>')
})

const FINISHED = (dir: string) => ({ [`${dir}/host`]: 'herdr\n', [`${dir}/label`]: 'cdx: list the files', [`${dir}/clean.md`]: CLEAN,
  [`${dir}/exit`]: '0', [`${dir}/start`]: '100', [`${dir}/end`]: '130', [`${dir}/last.md`]: '# Done\n\nall good' })

test('a foreground call returns with its page written and a note for the model where nothing draws panes', async ($, on) => {
  const { files, dir, result } = await delegate($, on, true, FINISHED, [])
  expect(files[`${dir}/run.html`]).toContain('<h1>cdx: list the files</h1>')
  const note = (result as { context?: string[] }).context?.join('\n') ?? ''
  expect(note).toContain(`is at ${dir}/run.html`)
  expect(note).toContain('copy that file to a folder they can open')
})

test('where a surface draws panes the note only names the page', async ($, on) => {
  const { dir, result } = await delegate($, on, true, FINISHED, ['terminal'])
  const note = (result as { context?: string[] }).context?.join('\n') ?? ''
  expect(note).toContain(`is at ${dir}/run.html`)
  expect(note).not.toContain('copy that file')
})

test('a run that left no end (bare, outside herdr) adds no note', async ($, on) => {
  const { result } = await delegate($, on)
  expect((result as { context?: string[] }).context ?? []).toEqual([])
})

test('runPage escapes everything from the run and opens on the answer only when it succeeded', () => {
  const evil = '<script>alert(1)</script> "x" & <img src=x onerror=1>'
  const html = runPage({ label: evil, status: evil, ok: false, answer: evil,
    parts: [{ kind: 'prose', text: evil }, { kind: 'fold', title: evil, body: '```\n' + evil + '\n```', failed: true }], logPath: '/d/<x>' })
  expect(html).not.toContain('<script>alert')
  expect(html).not.toContain('<img')
  expect(html.match(/<script>/g)?.length).toBe(1) // the page's own
  expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
  expect(html).toContain('data-first="live"')
  expect(runPage({ label: 'l', status: 's', ok: true, answer: 'a', parts: [], logPath: '/d' })).toContain('data-first="answer"')
  expect(mdToHtml('### H\n\n**b** `c` *i*\n\n- one\n- two\n\n> q\n\n```\n<x>\n```')).toBe(
    '<h3>H</h3>\n<p><strong>b</strong> <code>c</code> <em>i</em></p>\n<ul><li>one</li><li>two</li></ul>\n<blockquote>q</blockquote>\n<pre>&lt;x&gt;</pre>')
})

test('a call that never runs (denied) is dropped after a minute', { options: { claudePane: 'auto' } }, async ($, on) => {
  const { clock, opened } = await delegate($, on)
  await clock.advance(61000)
  expect(opened).toEqual([])
})

test('claudePane off (the default) keeps the bare run outside herdr; herdr runs are still followed for the page', async ($, on) => {
  const { files, id } = await delegate($, on)
  const script = Object.entries(files).find(([p]) => p.endsWith(`codex-herdr-run.${id}.sh`))?.[1] ?? ''
  expect(script).toContain('then exec bash "$__D/orig.sh"; fi')
  expect(script).not.toContain('echo claude')
  expect(script.indexOf('then exec bash "$__D/orig.sh"; fi')).toBeLessThan(script.indexOf('echo herdr > "$__D/host"'))
})

test('the headless run uses the same pipeline with no viewer, then the usual report', () => {
  const o = {
    direction: 'right', layout: 'tile', maxPanes: 6, closePane: 'always', closeDelay: 3, tailLines: 50, viewer: 'textual',
    leanProfile: false, leanProfileName: 'lean', root: '/mod', id: 'x1', claudePane: true, runDir: '/tmp/codex-herdr.x1',
  } as const
  const text = buildWrapper('codex exec "x"', o)
  expect(text).toContain(`bash "$__D/pane.sh" "$__D" '/mod' none 0 </dev/null >/dev/null 2>&1`)
  expect(text).toContain('[ "$VIEW" = none ] || printf')
  expect(text).toContain('if [ "$VIEW" = none ]; then :')
  expect(text.indexOf('echo claude > "$__D/host"')).toBeLessThan(text.indexOf('herdr pane split'))
  expect(text).toContain('mkdir -m 700 "$__D" 2>/dev/null || { __D=$(mktemp -d')
})

test('parseRun: prose and folds, markers in fences are text, an unclosed fold waits', () => {
  const md = '### A\n\nhi\n\n<!-- fold closed: T · exit 0 · 1 line -->\n**Bash** `t`\n\n```\n<!-- /fold -->\n```\n<!-- /fold -->\n\n```\n<!-- fold closed: fake -->\n```\n'
  const parts = parseRun(md)
  expect(parts.map((p) => p.kind)).toEqual(['prose', 'fold', 'prose'])
  const fold = parts[1]!
  expect(fold.kind === 'fold' && fold.body).toBe('```\n<!-- /fold -->\n```')
  expect(parts[2]!.kind === 'prose' && parts[2]!.text.includes('fake')).toBe(true)
  const open = '<!-- fold closed: U · exit 0 · 1 line -->\n```\nz\n```\n'
  expect(parseRun(open)).toEqual([])
  expect(parseRun(open, true).map((p) => p.kind)).toEqual(['fold'])
  expect(parseRun('one\ntw').map((p) => p.kind === 'prose' && p.text)).toEqual(['one']) // a partial line waits
  const cut = parseRun('<!-- fold closed: Bash: long... · exit 0 · 1 line -->\n**Bash** `long command`\n\n```\ny\n```\n<!-- /fold -->\n')[0]!
  expect(cut.kind === 'fold' && cut.body.startsWith('**Bash** `long command`')).toBe(true) // a cut title keeps the label
})

test('/codex-runs puts the run in the chat when no surface draws panes', { options: { claudePane: 'auto', closePane: 'never' } }, async ($, on) => {
  const { clock, files, dir } = await delegate($, on, false)
  Object.assign(files, { [`${dir}/host`]: 'claude\n', [`${dir}/label`]: 'Codex task: list the files', [`${dir}/clean.md`]: CLEAN,
    [`${dir}/exit`]: '1', [`${dir}/end`]: String(Math.floor(clock.now() / 1000)), [`${dir}/last.md`]: '' })
  await clock.advance(800)
  await clock.advance(800)
  const out = await $.command.run(RUNS_CMD)
  const text = String((out as { text?: string }).text ?? '')
  expect(text).toContain('Codex task: list the files: failed with exit 1')
  expect(text).toContain('I will list the files.')
  expect(text).toContain('✓ Bash: ls · exit 0 · 2 lines')
  expect(text).toContain('✗ Bash: cat x · exit 1 · 1 line\n    nope')
  expect(text).toContain(`Full log: ${dir}/clean.md`)
  expect(text).not.toContain('<!--')
})

test('/codex-runs with no run followed says so', { options: { claudePane: 'auto' } }, async ($) => {
  const out = await $.command.run(RUNS_CMD)
  expect(String((out as { text?: string }).text)).toContain('No delegation run is followed')
})

test('summarizeRun: text, one line per result, failed output cut, the answer last, the newest kept', () => {
  const parts = parseRun(CLEAN, true)
  const text = summarizeRun(parts, 'head', 'the answer', '/d/clean.md')
  expect(text.startsWith('head\n\nI will list the files.\n\n✓ Bash: ls')).toBe(true)
  expect(text).not.toContain('**Bash**')
  expect(text).not.toContain('```')
  expect(text).toContain('--- answer ---\nthe answer\n\nFull log: /d/clean.md')
  const long = parseRun(Array.from({ length: 400 }, (_, i) => `<!-- fold closed: Bash: step ${i} · exit 0 · 1 line -->\n\`\`\`\nx\n\`\`\`\n<!-- /fold -->\n`).join(''), true)
  const cut = summarizeRun(long, 'h', '', '/d')
  expect(cut).toContain('... (earlier steps cut)')
  expect(cut).toContain('step 399')
  expect(cut).not.toContain('step 0 ')
})

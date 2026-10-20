// A finished run as one HTML page (run.html beside the run's logs): the answer first, then the
// run with each tool result in a <details> fold (closed when it passed, open when it failed).
// For surfaces that draw no mod panes: Claude sends the page to the person as a file. Everything
// taken from the run is escaped; the Markdown is a small subset (headings, fences, quotes, lists,
// bold, italic and inline code), enough for what codex.jq, stream.jq and an answer write.

import type { RunPart } from './livedoc.ts'

export type RunPageInput = {
  label: string
  status: string // "finished in 22s · input ..." as runpane.tsx says it
  ok: boolean
  parts: RunPart[]
  answer: string
  logPath: string
}

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

const inline = (t: string) => esc(t)
  .replace(/`([^`]+)`/g, '<code>$1</code>')
  .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  .replace(/(^|[^*])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>')

export function mdToHtml(src: string): string {
  const lines = src.replace(/\r/g, '').split('\n')
  const out: string[] = []
  let i = 0
  while (i < lines.length) {
    const l = lines[i]!
    const fence = /^ {0,3}(`{3,}|~{3,})/.exec(l)
    if (fence) {
      const close = new RegExp(`^ {0,3}${fence[1]![0] === '`' ? '`' : '~'}{${fence[1]!.length},}\\s*$`)
      const body: string[] = []
      for (i++; i < lines.length && !close.test(lines[i]!); i++) body.push(lines[i]!)
      i++
      out.push(`<pre>${esc(body.join('\n'))}</pre>`)
      continue
    }
    const h = /^(#{1,6}) (.*)$/.exec(l)
    if (h) {
      if (h[2] !== 'Turn') out.push(`<h3>${inline(h[2]!)}</h3>`) // codex.jq's turn marker says nothing on a page
      i++
      continue
    }
    if (l.startsWith('> ')) { out.push(`<blockquote>${inline(l.slice(2))}</blockquote>`); i++; continue }
    if (/^[-*] /.test(l)) {
      const items: string[] = []
      while (i < lines.length && /^[-*] /.test(lines[i]!)) items.push(`<li>${inline(lines[i++]!.slice(2))}</li>`)
      out.push(`<ul>${items.join('')}</ul>`)
      continue
    }
    if (!l.trim()) { i++; continue }
    const para: string[] = []
    while (i < lines.length && lines[i]!.trim() && !/^( {0,3}(`{3,}|~{3,})|#{1,6} |> |[-*] )/.test(lines[i]!)) para.push(lines[i++]!)
    out.push(`<p>${inline(para.join(' '))}</p>`)
  }
  return out.join('\n')
}

const CSS = `:root{--bg:#fbfaf7;--fg:#1f1f1c;--muted:#6b6a64;--line:#e4e1d8;--card:#fff;--ok:#2f7d4f;--bad:#b3362b;--code:#f3f1ea}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#171715;--fg:#ecebe6;--muted:#a09e96;--line:#34332f;--card:#1f1f1c;--ok:#6cc08b;--bad:#ef7b6f;--code:#262622}}
:root[data-theme="dark"]{--bg:#171715;--fg:#ecebe6;--muted:#a09e96;--line:#34332f;--card:#1f1f1c;--ok:#6cc08b;--bad:#ef7b6f;--code:#262622}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,sans-serif}
main{max-width:860px;margin:0 auto;padding:24px 16px 48px}
header{border-left:4px solid var(--state);padding:4px 0 4px 14px;margin-bottom:20px}
header h1{font-size:18px;margin:0 0 2px;overflow-wrap:anywhere}header p{margin:0;color:var(--muted);font-size:13px}
.tabs{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 16px}.tabs button{font:inherit;border:1px solid var(--line);background:var(--card);color:var(--fg);padding:5px 12px;border-radius:6px;cursor:pointer}
.tabs button[aria-pressed="true"]{border-color:var(--fg);font-weight:600}
h3{font-size:13px;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);margin:18px 0 6px}
pre{background:var(--code);padding:10px 12px;border-radius:6px;overflow-x:auto;font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;margin:8px 0}
code{background:var(--code);padding:1px 4px;border-radius:4px;font-size:.92em}
blockquote{margin:8px 0;padding:4px 10px;border-left:3px solid var(--bad);color:var(--bad)}
details{border:1px solid var(--line);background:var(--card);border-radius:8px;margin:8px 0;padding:0 12px}
details>summary{cursor:pointer;padding:8px 0;font:13px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere}
details.failed{border-color:var(--bad)}details.failed>summary{color:var(--bad)}
details[open]>summary{border-bottom:1px solid var(--line);margin-bottom:6px}
.answer{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:4px 16px}
.hidden{display:none}footer{margin-top:24px;color:var(--muted);font-size:12px;overflow-wrap:anywhere}`

const SCRIPT = `const show=t=>{for(const n of['live','answer']){document.getElementById(n).classList.toggle('hidden',n!==t);document.getElementById('t-'+n).setAttribute('aria-pressed',String(n===t))}document.getElementById('t-all').classList.toggle('hidden',t!=='live')};
document.getElementById('t-live').onclick=()=>show('live');document.getElementById('t-answer').onclick=()=>show('answer');
document.getElementById('t-all').onclick=e=>{const f=[...document.querySelectorAll('details')];const open=f.some(d=>!d.open);f.forEach(d=>d.open=open);e.target.textContent=open?'Close all':'Open all'};
show(document.body.dataset.first);`

export function runPage(p: RunPageInput): string {
  const live = p.parts.map((part) => part.kind === 'prose'
    ? `<section>${mdToHtml(part.text)}</section>`
    : `<details${part.failed ? ' class="failed" open' : ''}><summary>${esc(part.title)}</summary>${mdToHtml(part.body)}</details>`).join('\n')
  const answer = p.answer.trim() ? mdToHtml(p.answer) : '<p><em>No answer was written; see Live.</em></p>'
  const first = p.ok && p.answer.trim() ? 'answer' : 'live'
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Delegation Run</title><style>${CSS}</style></head>
<body data-first="${first}" style="--state:var(${p.ok ? '--ok' : '--bad'})"><main>
<header><h1>${esc(p.label)}</h1><p>${esc(p.status)}</p></header>
<div class="tabs"><button id="t-live" aria-pressed="false">Live</button><button id="t-answer" aria-pressed="false">Answer</button><button id="t-all">Open all</button></div>
<div id="live">${live || '<p><em>No events were logged.</em></p>'}</div>
<div id="answer" class="answer">${answer}</div>
<footer>Full log: ${esc(p.logPath)}</footer>
<script>${SCRIPT}</script></main></body></html>
`
}

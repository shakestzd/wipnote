package render

// The run page's frame, styles and script, as the mod's runhtml.ts writes
// them. Arguments: CSS, first tab, state colour, label, group tag, status, live parts,
// answer, log path, script.
const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Delegation Run</title><style>%s</style></head>
<body data-first="%s" style="--state:var(%s)"><main>
<header><h1>%s</h1><p>%s%s</p></header>
<div class="tabs"><button id="t-live" aria-pressed="false">Live</button><button id="t-answer" aria-pressed="false">Answer</button><button id="t-all">Open all</button></div>
<div id="live">%s</div>
<div id="answer" class="answer">%s</div>
<footer>Full log: %s</footer>
<script>%s</script></main></body></html>
`

const pageCSS = `:root{--bg:#fbfaf7;--fg:#1f1f1c;--muted:#6b6a64;--line:#e4e1d8;--card:#fff;--ok:#2f7d4f;--bad:#b3362b;--code:#f3f1ea}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#171715;--fg:#ecebe6;--muted:#a09e96;--line:#34332f;--card:#1f1f1c;--ok:#6cc08b;--bad:#ef7b6f;--code:#262622}}
:root[data-theme="dark"]{--bg:#171715;--fg:#ecebe6;--muted:#a09e96;--line:#34332f;--card:#1f1f1c;--ok:#6cc08b;--bad:#ef7b6f;--code:#262622}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.55 ui-sans-serif,system-ui,-apple-system,sans-serif}
main{max-width:860px;margin:0 auto;padding:24px 16px 48px}
header{border-left:4px solid var(--state);padding:4px 0 4px 14px;margin-bottom:20px}
header h1{font-size:18px;margin:0 0 2px;overflow-wrap:anywhere}header p{margin:0;color:var(--muted);font-size:13px}
.group{color:var(--state);font-weight:600}
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

const pageScript = `const show=t=>{for(const n of['live','answer']){document.getElementById(n).classList.toggle('hidden',n!==t);document.getElementById('t-'+n).setAttribute('aria-pressed',String(n===t))}document.getElementById('t-all').classList.toggle('hidden',t!=='live')};
document.getElementById('t-live').onclick=()=>show('live');document.getElementById('t-answer').onclick=()=>show('answer');
document.getElementById('t-all').onclick=e=>{const f=[...document.querySelectorAll('details')];const open=f.some(d=>!d.open);f.forEach(d=>d.open=open);e.target.textContent=open?'Close all':'Open all'};
show(document.body.dataset.first);`

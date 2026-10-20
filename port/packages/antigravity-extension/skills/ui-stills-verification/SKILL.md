---
name: ui-stills-verification
description: "Verifies web UI changes with headless stills: probes the DOM, captures one component with shot-scraper, reads the PNG back, and judges it. Use when a task touches templates, CSS, dashboards, or any rendered page, before claiming UI work is done, and as the researcher's visual-QA method when no browser MCP is available."
disable-model-invocation: true
---

# UI Stills Verification

Use `shot-scraper` (headless Playwright) to look at the UI you changed: probe the page,
capture one component, read the PNG back with your image tool, judge, adjust.

Copy this checklist and tick it off:

```
UI stills:
- [ ] 1. Page probed with `shot-scraper javascript` (Step 1)
- [ ] 2. One component captured with `--selector` and `--wait` (Step 2)
- [ ] 3. Every PNG read back with the image tool (Step 3)
- [ ] 4. Per-image judgement with severity; fixes re-captured (Step 4)
- [ ] 5. Demo or fixture data only (Safety)
```

---

## Setup (once per machine)

```bash
uv tool install shot-scraper     # or: pipx install shot-scraper
shot-scraper install             # downloads the browser (~100MB), once
```

Two subcommands do everything:

| Command | Purpose |
|---|---|
| `shot-scraper shot URL -o out.png` | capture a still |
| `shot-scraper javascript URL "expr"` | run JS in the page, get JSON back |

You need `javascript` as much as `shot`: use it to learn what is on the page before you try
to photograph part of it. Check `--help` before inventing flags — `shot-scraper javascript`
has **no `--wait` flag** (wait inside the expression), and `--reporter` does not exist.

## Getting a URL

- Plain web app / dashboard: you already have one (`wipnote serve` → `http://127.0.0.1:8080`).
- Desktop shell (Tauri/Electron) around a web frontend: point at the dev server and skip the
  shell. Expect the shell to have injected things the browser will not have (API base URL,
  auth token, capability detection). Do **not** weaken production auth to make this work —
  check whether the backend has a no-auth dev mode instead.
- **Say which layers you are proving.** Browser-mode stills verify rendering, not the shell's
  transport or native APIs.

## Step 1 — Probe before photographing

Do not guess selectors. Ask the page what it has:

```bash
shot-scraper javascript http://localhost:5173 "
new Promise(r => setTimeout(() => r(
  Array.from(document.querySelectorAll('h2,h3,[class*=panel]'))
    .map(e => (e.textContent||'').trim().slice(0,60))
    .filter(Boolean)
), 8000))
"
```

This returns real JSON you can act on — the headings tell you exactly what text to anchor a
selector to. To find where something sits, get real offsets instead of guessing:

```bash
shot-scraper javascript URL "
new Promise(r => setTimeout(() => {
  const find = re => {
    const els = Array.from(document.querySelectorAll('*'))
      .filter(e => re.test((e.textContent||'').trim()) && e.children.length < 6);
    const e = els[els.length-1];
    return e ? Math.round(e.getBoundingClientRect().top + window.scrollY) : null;
  };
  r({ forecast: find(/^Projected cash flow/), digest: find(/^What changed/) });
}, 8000))
"
```

**When a probe says "absent", suspect the probe first.** A regex with three literal dots
(`/Thinking\.\.\./`) will never match a Unicode ellipsis (`…`); one such false "absent" built a
whole wrong theory. Assert on a substring you have **actually seen** in captured text.

## Step 2 — Capture ONE component, not the whole page

A full-page shot of a long dashboard is nearly useless for judging a single panel. Tag the
element with `--javascript` (runs **before** the capture), then crop with `--selector`:

```bash
shot-scraper shot http://localhost:5173 -o panel.png --width 1440 --wait 8000 \
  --selector '#target' --javascript "
  const hits = Array.from(document.querySelectorAll('*'))
    .filter(e => /^Projected cash flow/.test((e.textContent||'').trim()));
  let card = hits[hits.length - 1];              // innermost match
  for (let i = 0; i < 8 && card.parentElement; i++) {
    card = card.parentElement;
    if (card.className && /card/i.test(card.className.toString())) break;
  }
  card.id = 'target';
"
```

- Take the **last** text match — outer containers match the same text; the innermost is what
  you mean. Then **walk up** to the real card boundary, or you crop a text node.
- **`--wait <ms>`** matters: pages that fetch on mount capture loading skeletons without it.
  6000–9000 ms is a realistic start for a dashboard hitting several endpoints.
- **`--height`** matters: omit it and you get a full-page capture (1440×6909 is unreadable).
  Use `--selector` for a component or `--height` for a viewport shot. Check dimensions cheaply:
  `python3 -c "import struct;d=open('panel.png','rb').read(33);print(struct.unpack('>II',d[16:24]))"`.

### Driving state first

`--javascript` can navigate and interact before the capture; chain with awaited sleeps:

```bash
shot-scraper shot URL -o chat.png --width 1440 --height 900 --wait 7000 --javascript "
  (async () => {
    const sleep = ms => new Promise(s => setTimeout(s, ms));
    const nav = Array.from(document.querySelectorAll('button,a,[role=button]'))
      .find(e => /^Chat\$/.test((e.textContent||'').trim()));
    nav && nav.click(); await sleep(3500);
  })()
"
```

**Typing into React needs the native value setter** — assigning `.value` does not notify React:

```javascript
const input = document.querySelector('textarea') || document.querySelector('input');
const proto = input.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
Object.getOwnPropertyDescriptor(proto, 'value').set.call(input, 'my question');
input.dispatchEvent(new Event('input', { bubbles: true }));
```

Prefer in-page DOM interaction over OS-level menus: native dropdowns may not appear in a
headless capture at all, so "click the menu then screenshot" can silently produce nothing.

## Step 3 — READ THE PNG BACK (mandatory)

A PNG is not verification; reading it back is. `shot-scraper` only writes a file.

```
Read /abs/path/panel.png
```

`ls -l panel.png` or the file existing does not count. A report that lists screenshot paths
without a read-back is unverified. Open the image with your image-reading tool and look at it. Only then write the judgement:
what is rendered, what is wrong (layout, readability, data correctness, hierarchy), severity.

If you cannot read images in this harness, say so explicitly in the report and hand the PNG
paths to an agent that can — do not describe an image you have not seen.

## Step 4 — Judge, adjust, repeat

- **A single frame cannot tell "hasn't arrived yet" from "never renders".** For streaming,
  polling or progressive loading, sample the DOM on a timer with `shot-scraper javascript`
  and use stills only to confirm what the samples already showed.
- **Verify with a second look when a still surprises you** before building a theory on it.
- Report: screenshot paths **plus** a per-image judgement written after the read-back, with
  severity (**CRITICAL** broken/data missing · **MAJOR** layout/usability · **MINOR** polish · **OK**).

## Safety — never capture real user data

Stills outlive the session and get shared. Never capture real balances, names, merchant or
counterparty strings, tokens, or personal data into a saved artifact: use a sample/demo
profile or seeded fixture data for every screenshot. If real data is needed to verify
correctness, inspect it **read-only** via the probe and keep it out of saved images.

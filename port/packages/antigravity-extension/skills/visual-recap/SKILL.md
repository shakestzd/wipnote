---
name: visual-recap
description: "Generates a diff-grounded visual recap of a work item, git range, or session and summarizes its outcome. Use when asked to recap, summarize, or visualize completed work."
disable-model-invocation: true
---

# wipnote Visual Recap

Use this skill to generate a grounded, diff-backed recap for a work item, git range,
or session — then surface the key outcome narrative and a direct link to the HTML artifact.

Copy this checklist and tick it off:

```
Recap (<scope>):
- [ ] 1. Scope identified (asked if ambiguous)
- [ ] 2. `wipnote recap` succeeded; recap-id captured
- [ ] 3. Artifact read; changes, reason and outcome extracted
- [ ] 4. Secrets redacted; every claim traced to the artifact
- [ ] 5. Narrative presented with the artifact path and dashboard link
```

---

## Step 1: Identify the scope

Determine what to recap from the user's request:

| User says | Scope | Command |
|-----------|-------|---------|
| `recap feat-<id>` / feature ID | Work item | `wipnote recap feat-<id>` |
| `recap bug-<id>` / bug ID | Work item | `wipnote recap bug-<id>` |
| `recap spk-<id>` / spike ID | Work item | `wipnote recap spk-<id>` |
| `recap main..HEAD` / git range | Git range | `wipnote recap --range main..HEAD` |
| `recap this session` / session ID | Session | `wipnote recap --session <session-id>` |

If the scope is ambiguous, ask for clarification before running anything.

---

## Step 2: Run wipnote recap

Run the command determined in Step 1:

```bash
wipnote recap <scope>
```

The command writes `.wipnote/recaps/<recap-id>.html` and prints:

```
Recap written: .wipnote/recaps/<recap-id>.html
```

Capture the `<recap-id>` from the output line. If the command fails, report the error
verbatim — do not attempt to construct the artifact manually.

---

## Step 3: Read the artifact and extract the narrative

Read the produced HTML artifact to gather the ground-truth diff data:

```bash
wipnote sh "cat .wipnote/recaps/<recap-id>.html" --max-lines 300
```

From the artifact extract:
- **What changed** — files touched, key additions / deletions (from the embedded diff)
- **Why** — work item title and description (from the embedded lineage metadata)
- **Outcome** — tests passing, artifacts committed, quality gates

If the output is truncated, `wipnote recap show <recap-id>` prints the title, outcome, and git range as JSON. For the rest of the diff, read the next chunk with an offset (e.g. the Read tool's offset/limit, or `wipnote sh "sed -n '301,600p' .wipnote/recaps/<recap-id>.html"`).

**Redact before displaying:** API keys, tokens, passwords, `.env` values, private paths,
and any content that looks like a secret. Replace with `[REDACTED]`.

---

## Step 4: Write the outcome narrative

Present a concise, grounded summary to the user:

```markdown
## Recap: <title or scope>

**Artifact:** `.wipnote/recaps/<recap-id>.html`
**Dashboard:** `wipnote serve` then open http://127.0.0.1:8080 (or http://127.0.0.1:8088 in devcontainer)

### What changed
<bullet list of key file changes and additions, grounded in the real diff>

### Outcome
<1-3 sentences: tests passed, gates clean, artifacts committed — based on the artifact content>

### Lineage
<work item ID + title if available, git range if range mode, session ID if session mode>
```

Before presenting, check each claim against the artifact and drop any you cannot point to. Keep the narrative tight — 5-10 lines. The full artifact is in the dashboard for deep review.
Write in plain English:
- One idea per sentence.
- Keep sentences under 20 words.
- Use the active voice.

---

## Notes

- **Local-first.** No data leaves `.wipnote/`. The recap artifact is a self-contained HTML
  file committed to the repository — no hosted service is involved.
- **Idempotent.** Re-running `wipnote recap <same-id>` overwrites the artifact in place.
- **Dashboard URL:** default port is `8080`; devcontainer port is `8088` (see AGENTS.md).
  Tell the user which applies based on their environment.
- **Secrets:** always redact before displaying any diff content in the narrative.
- **Auto-triggers.** `wipnote plan finalize` writes a rollup recap (`recap-pln-<plan-id>`).
  `wipnote feature complete` / `bug complete` only print a nudge to run `wipnote recap <id>`.

# wipnote-questions (mod)

A Claude Code mod that lists the open wipnote questions across work items in a pane and answers them. State lives in the CLI; the mod keeps none beyond what it last read.

## What it does

- `/questions-pane` opens the pane. It reads `wipnote question list --open --format json` and polls every 5 s while placed. Each row shows the question, item, age, `blocking` and the recommended key (★), then its options.
- `/answer-question <question-id> <option-key>` runs `wipnote question answer <qid> <key>`, toasts the command's first output line (for example "overturned, <item> reopened"), shows it in the pane, and the row leaves the list.
- The orchestra band shows "N open questions · M blocking", toasts a new blocking question, and wipnote-ref cards list the item's open questions (see `mods/wipnote-orchestra`).

The mod API used here has no clickable element, so choosing an option is the command above.

## Files

```
.claude-plugin/plugin.json   manifest
hooks/hooks.json             { "modules": ["./register.tsx"] }
hooks/register.tsx           commands, 5 s poll, Pane render
hooks/model.ts               pure helpers: argv, list JSON, row and count lines
types/index.d.ts             row and listing types, plugin state
tests/model.test.ts          claude plugin test suite
```

## Tests

```
claude plugin test mods/wipnote-questions
```

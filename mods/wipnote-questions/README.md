# wipnote-questions (mod)

A Claude Code mod that lists the open wipnote questions across work items in a pane and answers them. State lives in the CLI; the mod keeps none beyond what it last read.

## What it does

- `/questions-pane` opens the pane. It reads `wipnote question list --open --format json` and polls every 5 s while placed. Each row shows the question, item, age, `blocking` and the recommended key (★), then its options.
- `/answer-question <question-id> <option-key>` runs `wipnote question answer <qid> <key>`; `/answer-question <question-id> --text <reply>` answers a free-text question (one with no options, marked "free text" in the pane). The toast is the overturn or reopen warning when the answer made one, else the command's first output line; the pane shows the whole output. The row leaves the list.
- The orchestra band shows "N open questions · M blocking", toasts a new blocking question, and wipnote-ref cards list the item's open questions (see `mods/wipnote-orchestra`).

Each option is a button: click it, or give the pane focus (ctrl+x tab or a click) and press its digit (1-4) to answer the top question. A second press while the answer is being recorded is dropped. `/answer-question` stays for free-text questions and for answering from the prompt.

## Files

```
.claude-plugin/plugin.json   manifest
hooks/hooks.json             { "modules": ["./register.tsx"] }
hooks/register.tsx           commands, 5 s poll, Pane render
hooks/model.ts               pure helpers: argv, list JSON, row and count lines
types/index.d.ts             row and listing types, plugin state
tests/model.test.ts          claude plugin test suite
tests/pane.test.tsx          pane: option buttons answer the question
```

## Tests

```
claude plugin test mods/wipnote-questions
```

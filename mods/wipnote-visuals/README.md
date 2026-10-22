# wipnote-visuals (dev mod)

Three things: a **receipt line** under each turn that says what the tools did and how the reply reads (costs the model nothing), a **writing check** that sends one line with the next prompt only when a reply broke a rule, and **drawn fenced blocks** in replies, where the model sends only short inputs and the mod draws them. Without the mod each block still reads as plain text.

Commands: `/visuals on|off` turns the blocks and the model rule on or off; `/visuals receipt on|off` turns the receipt line on or off; `/visuals` alone reports both states, and any other argument prints usage and changes nothing. `/plainspeak strict|80|off` sets the writing check (default `off`: no score, no note).

## Receipt

After a main-loop turn, one line under the answer, built from the turn's own tool calls (`tool.call`) and shown at `turn.complete`. With `/plainspeak strict` or `80` the writing score follows:

```
✎ 2 files · ✔ go test · ⎇ a5f39e1 · 2m10s
✎ 2 files · ✔ go test · ⎇ a5f39e1 · 2m10s · tells 0 · long 1 · em— 0
```

| Part | Source |
|---|---|
| `✎ n files` | Edit, Write, MultiEdit, NotebookEdit paths, counted once each |
| `✔ go test`, `✘ go test (exit 1)`, `? npm test (exit unknown)` | Bash commands whose shape is a test (`go test`, `npm test`, `bun test`, `pytest`, `vitest`, `jest`, `claude plugin test`, `wipnote check`, …) or a build/check (`go build`, `go vet`, `tsc`, `claude plugin validate`, …). Leading `NAME=value` assignments and wrappers (`time`, `sudo`, `env`, `nohup`, `nice`, `timeout`, `npx`, `bunx`) are stripped first, so `CGO_ENABLED=0 go test` is the test run. |
| chained commands | One Bash call has one exit status, and the receipt never claims more than it tells. `a && b && c` that succeeds: every piece `✔`. One that fails: which piece failed is unknown, so the chain is one item, `✘ go build && go vet && go test (exit 1)`, and it counts as a failed test run when a test is in it. `a ; b` and `a \| b` report `b` only; `a` is `?`. `a \|\| b` that succeeds hides which ran (`?`); one that fails means both failed (`✘`). An interrupted call is `?` throughout. So `go test ./... \| tail -5` reads `? go test (exit unknown)`: drop the pipe if you want the exit known. |
| `⎇ a5f39e1` | The sha core attaches to a committing Bash call (`gitOperation.commit.sha`); `⎇ commit` when a `git commit` ran but no sha came back |
| `2m10s` | `e.durationMs` |
| `⚠ reply says tests pass; no test command ran this turn` | The answer claims passing tests (`tests pass`, `all 39 tests pass`, `all checks passed`, `suite is green`, …) and no test command ended ok. Also `… go test failed` and `… ended with an unknown exit`. A negated sentence (`tests did not pass`, `no checks passed`, `don't pass`) and another sense of the verb (`checks pass through the proxy`) are not claims. |

Nothing is drawn for a turn with no edits and no test/build/commit commands (a flagged claim still shows). Subagent calls and turns are left out; the receipt is the main loop's. An interrupted turn gets no line, and its collected calls are dropped, so nothing leaks into the next receipt.

**Exit codes.** The Bash tool's record carries `stdout`, `stderr`, `interrupted` and `gitOperation`, but no exit code. A non-zero exit makes core answer the call with `isError: true`, and the model-facing text usually says `Exit code N`, which the receipt reads when present. So `✔` means the call answered without error, `✘` means it errored or a guard denied it, and `?` means interrupted or an unreadable result.

Helpers are pure in `hooks/receipt.ts`; `tests/receipt-fixture.ts` holds 16 fake turns the tests must reproduce exactly. The hooks live in `hooks/turnhooks.ts`.

## Writing check

`hooks/lint.ts` (pure, ported from luladoc's Zinsser rules) checks a reply for AI tells (banned words, openers, filler, em dashes, same opener three times, a short negating closer, curly quotes or en dashes) and sentences over 25 words (20 in a list item). Code, inline code, URLs and tables are ignored.

- **Modes**: `/plainspeak off` (default) shows no score and sends no note. `80` checks banned words, filler, em dashes and long sentences. `strict` adds the repeated opener, the negating closer and curly quotes or en dashes.
- **Score**: in `80` or `strict`, `tells n · long n · em— n` is appended to the receipt line of every main-loop turn. A tool-free turn shows the score alone only when it breaks a rule, so a clean short answer adds no line.
- **Feedback note**: in `80` or `strict`, a reply that breaks a rule earns one line (about 20 tokens) such as `Last reply: "crucial", 2 em dashes in ¶1, 1 sentence >25 words.` It goes with the next prompt the user types as `prompt.submit` context (text only the model reads), once; a clean reply or an interrupted turn sends nothing and clears any pending note.
- **Word list**: ordinary technical words (`robust`, `enhance`, `landscape of the X`) are not in the lint list; the Plain English style still bans them for the model.
- **Ranking**: the note names the worst three items by severity (banned words, then openers, filler, em dashes, long sentences, then the softer rules), not the first three hits in the text.

The note travels as prompt context rather than a system-prompt section on purpose: a section that changes between turns would break the prompt cache on every request.

`eval/lintcheck.ts` runs the rules offline over saved replies and a session transcript; results and verdicts are in `eval/README.md` under "Lint false-positive check".

## Blocks

The rule (about 60 tokens, `prompt.md`) asks for one block only. `wipnote claude` appends it to the system prompt when it loads the mods, so `/visuals off` stops the drawing but not the rule. It is not a `prompt.compose` hook, because a managed policy plugin that sends that event past the user tier skips every mod's hook.

**wip:compare** — `# Option` starts an option (a trailing `*` marks the recommended one), then `+ pro` and `- con` lines. Drawn side by side.

````
```wip:compare
# SQLite*
+ no server
- single writer
# Postgres
+ concurrent writes
- extra service
```
````

Two older blocks still parse and draw, so earlier transcripts keep their look, but the rule no longer asks for them: a numbered list and a bold `**Warning:**` line are Markdown already (feat-ca6bd746, design section 3).

**wip:steps** — one step per line; `x ` = done, `> ` = current, no prefix = todo.

**wip:callout** — first line `decision:`, `warning:` or `note:` then text; further lines continue it.

Open or malformed fences stay plain text. Parsers are in `hooks/blocks.ts` (pure), drawing in `hooks/components.tsx`.

## Check it

```bash
claude plugin validate mods/wipnote-visuals
claude plugin test mods/wipnote-visuals
```

`eval/` holds the live-model eval for the rule and the offline lint check (see `eval/README.md`). The receipt's real-session measurement (20 turns: line shown when and only when tools changed something, classifier misses, mismatch flags that were right) is still to do.

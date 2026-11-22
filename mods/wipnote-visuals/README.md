# wipnote-visuals (dev mod)

Two things: a **receipt line** under each turn that says what the tools did and how the reply reads (costs the model nothing), and a **writing check** that sends one line with the next prompt only when a reply broke a rule. The mod draws nothing inside a reply (see Blocks).

Commands: `/visuals receipt on|off` turns the receipt line on or off; `/visuals` alone reports its state, and any other argument prints usage and changes nothing. `/plainspeak strict|80|off` sets the writing check (default `off`: no score, no note).

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

The mod no longer draws blocks or sends a rule for them. Steps, options and callouts in a reply are ```` ```lulawrite ```` blocks, which the lulawrite plugin checks and draws (feat-fd3637f9). Two plugins that each draw a whole reply fight over it: the one loaded first won and the other's blocks showed raw.

Install lulawrite beside wipnote: `/plugin install lulawrite@luladoc` (marketplace `shakestzd/luladoc`), then `/lulawrite-install`. Its Precise output style carries the block shapes.

| Old wipnote block (`wip:` prefix) | lulawrite block |
|---|---|
| compare | `{"options": {"items": [{"name", "summary", "pros", "cons", "pick": true}]}}` |
| steps | `{"steps": {"items": [{"title", "text"}]}}` (no done or current state) |
| callout | `{"callout": {"tone": "decision\|warning\|note", "text"}}` |

Old transcripts still hold `wip:*` fences. They read as plain text, so they need no converter.

## Check it

```bash
claude plugin validate mods/wipnote-visuals
claude plugin test mods/wipnote-visuals
```

`eval/` holds the offline lint check (see `eval/README.md`). The receipt's real-session measurement (20 turns: line shown when and only when tools changed something, classifier misses, mismatch flags that were right) is still to do.

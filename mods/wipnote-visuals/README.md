# wipnote-visuals (dev mod)

Two things: a **receipt line** under each turn that says what the tools did (costs the model nothing), and **drawn fenced blocks** in replies, where the model sends only short inputs and the mod draws them. Without the mod each block still reads as plain text. `/visuals on|off` toggles the blocks and the model rule; `/visuals receipt on|off` toggles the receipt.

## Receipt

After a main-loop turn, one line under the answer, built from the turn's own tool calls (`tool.call`) and shown at `turn.complete`:

```
✎ 2 files · ✔ go test · ⎇ a5f39e1 · 2m10s
```

| Part | Source |
|---|---|
| `✎ n files` | Edit, Write, MultiEdit, NotebookEdit paths, counted once each |
| `✔ go test`, `✘ go test (exit 1)`, `? npm test (exit unknown)` | Bash commands whose shape is a test (`go test`, `npm test`, `bun test`, `pytest`, `claude plugin test`, `wipnote check`, …) or a build/check (`go build`, `go vet`, `tsc`, `claude plugin validate`, …). Chained commands (`a && b`) count per segment. |
| `⎇ a5f39e1` | The sha core attaches to a committing Bash call (`gitOperation.commit.sha`); `⎇ commit` when a `git commit` ran but no sha came back |
| `2m10s` | `e.durationMs` |
| `⚠ reply says tests pass; no test command ran this turn` | The answer claims passing tests (`tests pass`, `all checks passed`, `suite is green`, …) and no test command ended ok. Also `… go test failed` and `… ended with an unknown exit`. |

Nothing is drawn for a turn with no edits and no test/build/commit commands (a flagged claim still shows). Subagent calls and turns are left out; the receipt is the main loop's. Interrupted turns get no line.

**Exit codes.** The Bash tool's record carries `stdout`, `stderr`, `interrupted` and `gitOperation`, but no exit code. A non-zero exit makes core answer the call with `isError: true`, and the model-facing text usually says `Exit code N`, which the receipt reads when present. So `✔` means the call answered without error, `✘` means it errored or a guard denied it, and `?` means interrupted or an unreadable result.

Helpers are pure in `hooks/receipt.ts`; `tests/receipt-fixture.ts` holds 16 fake turns the tests must reproduce exactly.

## Blocks

The rule (about 60 tokens, `prompt.compose`) asks for one block only:

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

`eval/` holds the live-model eval for the rule (see `eval/README.md`). The receipt's real-session measurement (20 turns: line shown when and only when tools changed something, classifier misses, mismatch flags that were right) is still to do.

# Delegate token benchmark: lean workers against native subagents

Measured 2026-11-01 on Claude Code 2.1.316, herdr-app 0.1.11, hc 0.1.16, macOS.

## Result

On three read-only tasks and one small edit, a lean `wipnote delegate` worker cost less than a native
`wipnote:feature-coder` subagent on the same model (Sonnet 5.5), by list-price weighting.

| Task | Worker cost units | Native cost units | Worker saving |
|---|---|---|---|
| 1. Read 2 files, plus one follow-up | 28,577 | 46,395 / 48,531 | about 40% |
| 2. Read 2 files | 22,085 | 34,187 / 34,010 | about 35% |
| 3. Trace across 8 files | 40,335 | 42,355 / 48,765 | about 11% |
| 4. Add a function and its table test, run the test | 25,013 / 24,307 | 35,189 / 38,129 | about 33% |

**Treat this as a small sample, not a general claim.** It covers four small tasks and few runs. On raw
tokens the worker is not always lower (see Limits). A long task that edits many files is untested.

## Metrics

**Cost units.** One unit is one uncached input token. Each token type is weighted by its list price
on the [pricing page](https://platform.claude.com/docs/en/about-claude/pricing):

| Model | Input | 5m cache write | 1h cache write | Cache read | Output |
|---|---|---|---|---|---|
| Sonnet 5.5, $ per MTok | 2 | 2.50 | 4 | 0.10 | 10 |
| Sonnet 5.5, weight | 1 | 1.25 | 2 | 0.05 | 5 |
| Haiku 5.5 up to 100k tokens, $ per MTok | 0.10 | 0.125 | 0.20 | 0.01 | 0.50 |

Token counts come from the `usage` block of each request in the session transcript, counting each
message ID once.

**Usage limits.** On a subscription, the number that matters is how much of the plan limit a run uses.
This benchmark could not measure that. The only figure available from the CLI, `utilization` in
`rate_limit_event`, is rounded to whole percents of the 7-day window, and it read 0.03 before and after
these runs. The docs do not say how the limit weights cache reads and writes, so cost units may not
track it. To measure it, run a large batch of one kind at a time with no other sessions using the
limit, and compare the change. `/usage` also attributes recent usage to subagents, but it is
approximate and interactive only.

## Tasks

All are read-only over `internal/delegate/session/` on `feat/delegate-herdr-core`. Each side received
the same question. The native brief also named the work item, and the worker's brief came with the
standard preamble.

1. Read `boot.go` and `lean.go`. In at most 5 lines: which flags does a lean Claude worker get, and in
   what order relative to the brief? Follow-up: name one test that covers those flags, in one line.
2. Read `profile.go` and `../herdr/core_name.go`. In at most 5 lines: how is a profile resolved, and
   what happens if hc refuses a terminal name?
3. Read `spawn.go`, `profile.go`, `lean.go`, `lean_mcp.go`, `boot.go`, `flags.go`, `info.go` and
   `resume.go`. In at most 10 lines, trace how `--mcp "claude.ai Notion"` travels from the spawn options
   to the final argv, and how `--resume` keeps that setup. Cite file:line.
4. Edit, each run in its own clean worktree: add `TrimLabel(label, width)` to `internal/delegate/herdr/label.go`,
   add a table test in a new file with six named cases, and run it. All four runs' tests pass when rerun.

## Raw results

`Worker` is `wipnote delegate spawn` with `--profile standard` (Sonnet, medium effort).
`Native` is `Agent(subagent_type="wipnote:feature-coder", model="sonnet")`; its effort was not set or recorded.

| Run | Config | Requests | Input | 5m write | 1h write | Cache read | Output | Raw total | Cost units |
|---|---|---|---|---|---|---|---|---|---|
| T1 worker | final | 8 | 16 | 13,240 | 0 | 122,610 | 1,176 | 137,042 | 28,577 |
| T1 native a | | 4 | 10 | 30,114 | 0 | 80,951 | 939 | 112,014 | 46,395 |
| T1 native b | | 4 | 10 | 31,034 | 0 | 82,376 | 1,122 | 114,542 | 48,531 |
| T2 worker | final | 3 | 6 | 12,923 | 0 | 38,498 | 800 | 52,227 | 22,085 |
| T2 native a | | 3 | 6 | 20,081 | 0 | 61,505 | 1,201 | 82,793 | 34,187 |
| T2 native b | | 3 | 6 | 20,143 | 0 | 61,716 | 1,148 | 83,013 | 34,010 |
| T3 worker | final | 6 | 12 | 19,351 | 0 | 98,089 | 2,246 | 119,698 | 40,335 |
| T3 worker | search-first preamble | 7 | 14 | 22,354 | 0 | 127,851 | 2,297 | 152,516 | 45,834 |
| T3 worker | search-first preamble | 5 | 10 | 17,544 | 0 | 74,101 | 2,126 | 93,781 | 36,275 |
| T3 native a | | 4 | 8 | 24,522 | 0 | 88,690 | 1,452 | 114,672 | 42,355 |
| T3 native b | | 5 | 10 | 26,135 | 0 | 123,234 | 1,985 | 151,364 | 48,765 |
| T4 worker a | final | 5 | 10 | 10,269 | 0 | 66,242 | 1,771 | 78,292 | 25,013 |
| T4 worker b | final | 5 | 10 | 10,157 | 0 | 66,116 | 1,659 | 77,942 | 24,307 |
| T4 native a | | 5 | 10 | 17,217 | 0 | 109,968 | 1,632 | 128,827 | 35,189 |
| T4 native b | | 5 | 10 | 17,546 | 0 | 110,032 | 2,137 | 129,725 | 38,129 |

### Earlier configurations, kept because they changed the design

| Run | Config | Requests | 5m write | 1h write | Cache read | Output | Cost units |
|---|---|---|---|---|---|---|---|
| T1 worker | 1h cache, answer repeated | 6 | 0 | 14,921 | 98,779 | 1,796 | 43,773 |
| T2 worker | 1h cache, answer repeated | 3 | 0 | 12,660 | 38,241 | 1,100 | 32,738 |
| T3 worker | 1h cache, answer repeated | 7 | 0 | 19,157 | 112,114 | 2,831 | 58,089 |
| T2 worker | 5m, no repeat, "read several files in one step" | 3 | 12,913 | 0 | 38,474 | 800 | 22,070 |
| T3 worker | 5m, no repeat, "read several files in one step" | 5 | 37,296 | 0 | 90,970 | 2,910 | 65,728 |

The two task 3 rows lost to native and the others won narrowly or later improved, which is how the fixes below were found. The last row
read all 8 files in full, and swapping that preamble line for "search first" brought task 3 back down.

### Model choice

Task 2 on `--profile simple` (Haiku 5.5, low effort): 6 requests, 11,536 writes, 77,229 reads, 1,541
output, about $0.0030. The Sonnet worker cost about $0.044 and native about $0.068. The answers were not
scored for quality, so this shows the price gap and does not show that Haiku suits every task.

## Startup load

First request of a real worker, as cache writes plus reads, on an interactive session:

| Setup | First request |
|---|---|
| Worker before this work (default flags) | 75,100 |
| + `--strict-mcp-config`, `--tools` (4 tools), `autoMemoryEnabled`, `outputStyle`, excluding user CLAUDE.md, disabling user plugins | 35,807 |
| + `syncClaudeAiPlugins: false`, `remoteControlAtStartup: false` | 28,560 |
| + `--no-chrome`, `includeGitInstructions: false` | 14,419 (Haiku) |
| Final lean worker on Sonnet | about 13,900 |
| Native `wipnote:feature-coder` | about 22,900 |

Headless measurements of MCP choices (`claude -p`, Haiku, not the same load as interactive):

| Setup | Context |
|---|---|
| No MCP (`--strict-mcp-config`) | 11.9k |
| One local server through `--mcp-config` | 12.6k |
| One connector, others denied by name | 15.3k |
| All connectors with `ToolSearch` | 18.5k |
| All connectors **without** `ToolSearch` | 159.7k |

The last row is why spawn always adds `ToolSearch` when any MCP server is on.

## What made the difference

- **Startup flags.** Most of the first-request saving. The Chrome tool definitions alone were about 40 KB.
- **Cache lifetime.** A worker is a main conversation, so on a subscription it writes at the 1-hour rate
  (2x input) by default, while a subagent writes at 5 minutes (1.25x). Lean workers set
  `promptCacheTtl: "5m"`. Use `--cache-ttl 1h` for a worker that will sit idle between tasks: after five
  idle minutes, a 5m cache has to be rewritten.
- **No repeated answer.** Workers wrote the answer in `delegate reply` and again in their final text, and
  output is the most expensive token. The preamble now says to end with one short line.
- **Search first.** The preamble tells workers to read only what they need.

## Limits

- **Sample size.** One to three runs per configuration. Native task 3 ranged from 42k to 49k, and worker
  task 3 from 36k to 46k, so a single run can flip that task: the 45,834 run costs more than native's
  best run.
- **Raw tokens.** Workers often make more requests, partly for the claim and reply commands. On task 1
  the worker's raw total is higher (137k against about 113k) and it wins only because cache reads are
  cheap. A billing model that weights reads more heavily would narrow or reverse that.
- **Shared cache.** Workers in the same folder read about 6k of cache from each other's prefix. Native runs
  got cache hits between runs as well (task 2 about 9.7k). The first worker of a session pays more.
- **Effort.** Workers ran at medium. Native effort was not controlled or recorded, and output differences
  may partly come from that.
- **Parent cost.** What each side adds to the parent session was not measured precisely. The replies are
  similar in size, but a parent's later turns re-read them.
- **Quality.** Read-only answers were checked by reading them, and file:line citations in task 3 were not
  all verified. The edit task was judged only by its test passing, not by reviewing the code. It is one
  small edit. Three larger real changes were made by lean workers during this work, but not against a
  native twin.
- **Plan limit.** Not measured, as above.
- **Fewer capabilities.** Lean workers lack skills, subagents, MCP, auto memory and `~/.claude/CLAUDE.md`
  unless the delegator turns them on, so the saving partly comes from doing without them.

## How to repeat

Read a run's usage from its transcript, counting each message ID once:

```sh
jq -c 'select(.type=="assistant" and .message.usage) | {id:.message.id, u:.message.usage}' SESSION.jsonl |
  jq -s 'group_by(.id) | map(.[-1].u) | {
    reqs: length,
    in:  (map(.input_tokens) | add),
    w5:  (map(.cache_creation.ephemeral_5m_input_tokens // 0) | add),
    w1h: (map(.cache_creation.ephemeral_1h_input_tokens // 0) | add),
    cr:  (map(.cache_read_input_tokens) | add),
    out: (map(.output_tokens) | add)
  } | .units = (.in + .w5*1.25 + .w1h*2 + .cr*0.05 + .out*5)'
```

A worker's transcript is `~/.claude/projects/<project>/<session>.jsonl`, with the session ID from
`wipnote delegate list --json`. A native subagent's is under `<parent session>/subagents/`. Check the
weights against the pricing page first, because they differ by model and change over time.

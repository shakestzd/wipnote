# ADR — Harness-agnostic per-session OTel collector

| Field | Value |
|-------|-------|
| Status | Accepted |
| Date | 2026-04-29 |
| Track | trk-cb36e595 (Multi-Project Hardening) |
| Implementation | feat-ac7532be (split into 11 child features) |
| Motivating incident | bug-28a9d7a7 |
| Prior design | plan-1cd284e0 (per-session OTel collector with NDJSON ingest) |

This document captures the cross-cutting decisions made while hardening the
per-session OTel collector and extending it to Codex CLI and Gemini CLI. It is
the discoverable narrative for new contributors who want to know *why* the
code looks the way it does — the *what* lives in the commit messages, the
*how* lives in `internal/otel/collector/` and the launchers.

## Context

`plan-1cd284e0` introduced a per-session OTel collector that the `htmlgraph
claude` launcher spawns as a child of the user's interactive session. The
collector listens on a dynamic 127.0.0.1 port, accepts OTLP/HTTP from the
harness, and writes NDJSON to `.htmlgraph/sessions/<sid>/events.ndjson`.
SQLite is treated as a derived index — the NDJSON file is canonical.

`bug-28a9d7a7` showed two failure modes the v1 design did not handle:

1. The collector handshake at startup could time out and the launcher
   silently continued in degraded mode. The operator had no signal — the
   first sign of trouble was an empty `events.ndjson` after the session.
2. The collector could die mid-session (OOM, signal, internal panic). The
   launcher had no detection or respawn path.

Concurrent with this hardening, Codex CLI and Gemini CLI both shipped
first-class OTel emission (Codex via `~/.codex/config.toml [otel]` and
service.name=`codex-cli`; Gemini via `GEMINI_TELEMETRY_*` env vars and
service.name=`gemini-cli`). The collector code was Claude-shaped, and
extending it to two more harnesses by copy-paste would have produced three
divergent spawn paths within weeks.

The work in `feat-ac7532be` therefore had two intertwined goals: harden the
spawn path against silent failure, and extract a harness-agnostic interface
so Codex and Gemini can ride the same lifecycle.

## ADR-001: `CollectorLifecycle` as a Go interface

### Decision

The spawn / retry / watchdog / cleanup machinery lives in a single new
package, `internal/otel/collector`, behind a single Go interface:

```go
type Lifecycle interface {
    Spawn(binPath, sessionID, projectDir string) (port int, cleanup func(), err error)
}
```

There is one concrete implementation, `ProcessCollector`, configured via
`ProcessCollectorOpts` (stderr writer, strict mode, optional spawn-fn for
tests, watchdog-interval env var name). All harness launchers — Claude,
Codex, Gemini — call `collector.NewProcessCollector(...).Spawn(...)`.

### Alternatives considered

**(a) Per-harness duplicated spawn code.** Each launcher imports
`os/exec` directly and re-implements retry, handshake parsing, watchdog,
and cleanup. Rejected because: three near-identical 200-line files diverge
within a release or two; the next bug (the equivalent of bug-28a9d7a7 for
Codex or Gemini) would have to be fixed in three places; testing surface
triples.

**(b) Function-based API (`collector.Spawn(opts)` package function with no
interface).** Simpler than an interface for a single implementation. Rejected
because the test seam matters: making `spawnFn` injectable through a struct
field is dramatically cleaner than mutating a package-level var, and the
struct can grow more knobs without breaking the call signature. The
interface adds zero runtime cost and one line of declaration.

**(c) Plugin-based (Go `plugin` package, dlopen).** Rejected on sight —
adds dynamic linking complexity, breaks cross-compilation, and solves a
problem we don't have (third-party lifecycles).

**(d) Subprocess-as-service (one always-on collector daemon, harnesses
reattach via session ID).** This is genuinely tempting and we may revisit
it. Rejected for v1 because: per-session collectors give us crash
isolation by construction (one bad session can't poison telemetry for
others); a daemon needs its own lifecycle/upgrade story; and the existing
NDJSON-canonical model assumes one writer per session file.

### Consequences

- `cmd/htmlgraph/claude_otel_collect_spawn.go` is now a thin shim
  (~110 LOC) that calls `internal/otel/collector`. Future Claude-specific
  hooks can live in the shim without contaminating the lifecycle.
- `cmd/htmlgraph/codex_launch.go` and `cmd/htmlgraph/gemini_launch.go`
  each contribute ~60-80 LOC of harness-specific env-injection logic on
  top of the shared lifecycle.
- The shared `appendOrReplaceEnv` helper (in `codex_launch.go`,
  package-visible to `gemini_launch.go`) keeps env-mutation logic in one
  place across launchers.
- Tests at the lifecycle boundary (`internal/otel/collector/lifecycle_test.go`)
  inject a fake `SpawnFn` and exercise retry/watchdog without real
  subprocesses. Per-launcher tests (`codex_launch_test.go`,
  `gemini_launch_test.go`) only verify env-string construction —
  cheap and deterministic.

### Forward considerations

- If a fourth harness arrives (Cursor, Aider, etc.) the cost is one
  `<harness>_launch.go` file plus one adapter in `internal/otel/adapter/`,
  not a redesign.
- If we eventually want per-harness retry policy or backoff schedules, the
  interface can grow to `Spawn(ctx, opts) (Result, error)` without breaking
  current callers (since there is exactly one external caller per harness).
- The watchdog's "respawn but lose the gap" semantics live in
  `ProcessCollector`. If a future harness needs zero-gap respawn (port
  forwarding via a localhost redirect), it would belong as a second
  implementation of `Lifecycle`, not a flag on the existing one.

## ADR-002: Launcher-based spawn vs hook-based spawn

### Decision

Each harness gets a wrapper command (`htmlgraph claude`, `htmlgraph codex`,
`htmlgraph gemini`) that spawns the collector *before* `exec`ing the
harness child, and injects the collector's port into the child's
environment. The hook-based alternative (have a `SessionStart` hook spawn
the collector and write the port to a file the harness later reads) was
considered and rejected for the spawn responsibility, though the existing
`SessionStart` hook continues to handle attribution and other side concerns.

### Alternatives considered

**(a) Launcher-based (chosen).** `htmlgraph codex` resolves the user's
`codex` binary, spawns the collector, builds the child env with the OTel
exporter pointed at the collector port, then `exec`s codex. Pre-fork env
injection is deterministic — by the time the harness reads its OTel
config, the port is real and listening.

**(b) Hook-based.** A `SessionStart` hook (`htmlgraph hook session-start`)
detects the harness, spawns the collector, and writes the port to
`.htmlgraph/sessions/<sid>/.collector-port`. The harness reads its OTel
config from the same file (or a wrapper script polls the file and exports
the env var before launching the harness binary). Considered because it
avoids requiring users to type `htmlgraph codex` instead of `codex`. But:

- Hooks cannot mutate child env post-fork in any of the three harnesses we
  support today. Whatever the hook writes has to be picked up by *some*
  pre-fork mechanism — which is exactly the wrapper command we already
  need.
- The hook path adds a write/poll race: the hook writes
  `.collector-port`, the harness needs to know the file is final before
  reading. Filesystem syncs across container boundaries (devcontainer +
  host mount) can produce stale reads.
- Diagnosing "my OTel didn't work" requires inspecting hook execution
  *and* the port file, vs. one `htmlgraph codex --debug` invocation.

**(c) Daemon mode.** A `htmlgraph daemon` runs in the background; the
harness's OTel config is permanently pointed at `127.0.0.1:<fixed port>`;
the daemon multiplexes multiple sessions onto one collector. Rejected for
the same reasons as ADR-001(d): per-session isolation, simpler upgrade
story.

### Consequences

- The user-visible command surface gains three commands:
  `htmlgraph claude`, `htmlgraph codex`, `htmlgraph gemini`. Each is a
  thin wrapper — invocations not using these wrappers continue to work
  but get no telemetry capture.
- For Codex and Gemini, the `htmlgraph plugin install` step (or
  equivalent harness-specific install) must document the wrapper
  invocation. The daemon-style "set it and forget it" UX is not
  available.
- The wrappers compose cleanly with non-OTel concerns (work-item
  attribution, worktree creation) that already live in `htmlgraph claude`.

### What remains for hooks

The launcher owns OTel collector spawning. The existing `SessionStart`
hook continues to own work-item attribution, session-table writes, and
project-dir resolution. There is no overlap or duplication; each handles
its own dimension.

## Canonical attribute mapping

All three harnesses now produce a `UnifiedSignal` with a small canonical
attribute set, populated by their respective adapter
(`internal/otel/adapter/{claude,codex,gemini}.go`). This is the same table
that lives in the doc comment on `internal/otel/signal.go:UnifiedSignal`,
reproduced here for narrative accuracy:

| Harness | `service.name` | Session attribute (signal-level) | Resource fallback | PromptID source |
|---------|----------------|-----------------------------------|-------------------|-----------------|
| Claude  | `claude-code`  | `session.id`                      | `session.id`      | `prompt.id` |
| Codex   | `codex-cli`    | `conversation.id`                 | `conversation.id` | `gen_ai.prompt_id` (when present; else empty) |
| Gemini  | `gemini-cli`   | `session.id`                      | `session.id`      | `gen_ai.prompt_id` |

### Notes on resource fallback

For metrics with cardinality control, the OTel SDK can omit per-data-point
session attributes; the adapter falls back to the resource-level
attribute. This mirrors the pattern established in the original Claude
adapter (`internal/otel/adapter/claude.go:baseSignal`) — Codex and Gemini
adapters reuse the same shape.

### Notes on PromptID

The Codex adapter does *not* synthesize PromptID from
`conversation.id + sequence` even though earlier design notes mentioned
this as a possibility. Synthesis would require sequence-number tracking
across batches, which adds state to a stateless adapter and produces
non-stable IDs (a re-ingestion of the same NDJSON file would produce
different sequences). Callers that need prompt-level correlation for
Codex must group by `(SessionID, Timestamp window)`.

## Resilience model

The hardened spawn path now provides three layers of protection, gated by
opt-in env vars where the new behavior is more aggressive than the
existing default:

1. **Retry with backoff** (`retrySpawnCollector` in
   `internal/otel/collector/lifecycle.go`). Always on. Three attempts at
   100ms / 300ms / 700ms backoff. Worst-case wall time ~10s including
   handshake timeouts. Targets transient port-bind contention and
   process-scheduling jitter.

2. **Fail-loud on permanent failure** (`HTMLGRAPH_OTEL_STRICT=1`). Off by
   default. When set, all-attempts-failed produces a non-zero exit
   instead of degraded silent mode. Operators running CI or production
   sessions where missing telemetry is itself a failure should set this.

3. **Watchdog respawn with port preservation**
   (`StartWatchdog` / `HTMLGRAPH_OTEL_WATCHDOG_INTERVAL`). On by
   default, 15s polling interval. Detects mid-session collector death
   and respawns the collector binding the **same port** as the original
   spawn, so the harness's OTLP exporter endpoint remains valid across
   restarts. OTel traffic emitted between the death and the respawn is
   still lost (the harness keeps retrying against a temporarily dead
   port), but traffic resumes once the new collector binds. The race
   window where another process could grab the port between collector
   exit and respawn is small on a 127.0.0.1 ephemeral range; if the
   rebind fails, the watchdog logs a FATAL line and stops. The
   `ProcessCollector` tracks the current process via
   `atomic.Pointer[*os.Process]` so launcher cleanup terminates the
   *current* collector (not the original), and per-process reaper
   goroutines remove `.collector-pid` only when the file's contents
   still match the reaped PID — making the cleanup race-free across
   respawns and idle exits.

The status surface (`/api/otel/status` and the `Collector health:` block
in `htmlgraph status`) lets operators verify all three are working
without grepping logs. Path traversal in the `?session=` query parameter
is rejected via `isSafeSessionID` before any filesystem access.

The cleanup function returned by `ProcessCollector.Spawn` is wrapped in
`sync.Once` so it can be safely invoked multiple times. This matters for
the launcher pattern — `htmlgraph claude/codex/gemini` register cleanup
via `defer` for normal/panic returns, but also call cleanup explicitly
before `os.Exit(exitCode)` on non-zero harness exit (Ctrl-C or harness
crash), since `os.Exit` bypasses deferred functions. The double-call
under successful normal returns is harmless under `sync.Once`.

## Future work

- **Zero-gap respawn.** The watchdog already preserves the port across
  respawns, so harness retries land on the new collector once it binds.
  Telemetry emitted *during* the gap (between old-collector death and
  new-collector ready) is still lost. A localhost redirect shim on the
  original port (a small TCP proxy that buffers in-flight requests
  during the rebind window) would close that residual gap. Cost is one
  more goroutine plus buffered-write semantics. Worth doing if
  real-world incidents show the residual gap matters.
- **Daemon-mode option.** A `htmlgraph daemon --multi-session` mode for
  power users who run many parallel sessions and want a single
  long-lived collector. Would require extending `Lifecycle` with a
  "join existing" path and a session-id-to-port multiplex table.
  Probably belongs in a separate plan.
- **Adapter for Cursor / Aider / others.** Bias toward shipping these as
  separate adapters rather than special-casing the existing ones.
- **Process-identity check on `.collector-pid` — implemented.** The
  reaper removes the PID file only when its contents still match the
  reaped PID. To close the PID-reuse window — where the kernel
  recycles a dead collector's PID for an unrelated process before
  `/api/otel/status` probes — the lifecycle now records the process
  start time (clock ticks from `/proc/<pid>/stat` field 22) on a
  second line of `.collector-pid` at write-time, and the
  `IsCollectorAlive` check verifies both PID liveness and start-time
  match. On non-Linux platforms (`/proc` unavailable) or when reading
  legacy single-line PID files, the check falls back to the PID-only
  `Signal(0)` probe — degraded but no worse than before. Future work:
  port this start-time mechanism to macOS using `kproc` /
  `proc_pidinfo` if observed PID-reuse incidents on darwin warrant
  the platform code.
- **TEST-1 escalation path.** The plan originally included a real-
  subprocess respawn integration test gated on `testing.Short()`. That
  test was deferred during this work — see `feat-e195d658` for the
  rationale. If a future incident traces back to a wiring bug between
  the watchdog and the side-effect helpers (`writeCollectorPID`,
  `registerCollectorCleanup`), revive the test.

## Cross-references

| Artifact | Where |
|----------|-------|
| Lifecycle implementation | `internal/otel/collector/lifecycle.go` |
| Lifecycle tests | `internal/otel/collector/lifecycle_test.go` |
| Claude shim | `cmd/htmlgraph/claude_otel_collect_spawn.go` |
| Codex launcher | `cmd/htmlgraph/codex_launch.go`, `cmd/htmlgraph/codex.go:execCodex` |
| Gemini launcher | `cmd/htmlgraph/gemini_launch.go`, `cmd/htmlgraph/gemini.go:execGemini` |
| Adapters | `internal/otel/adapter/{claude,codex,gemini}.go` |
| Adapter conformance | `internal/otel/adapter/conformance_test.go` |
| Multi-harness HTTP test | `cmd/htmlgraph/multiharness_test.go` |
| Status surface | `cmd/htmlgraph/api_collector_status.go`, `cmd/htmlgraph/status.go:printCollectorHealth` |
| Canonical attribute table | `internal/otel/signal.go` (`UnifiedSignal` doc comment) |

## Implementation history

The implementation was split into 11 child features under `feat-ac7532be`,
dispatched in waves with explicit `blocked_by` edges:

| Wave | Feature | Slice | Commit |
|------|---------|-------|--------|
| 1 | feat-75426991 | SCHEMA-1 (canonical attrs + Codex/Gemini adapters) | `ad7b335a` |
| 1 | feat-dcf3d618 | RESILIENCE-1 (fail-loud spawn) | `e6815d7e` |
| 1 | feat-1c3ebad9 | STATUS-1 (collector health surface) | `4f09f824` |
| — | feat-ac7532be | Roborev fixes (HIGH+3MED on Wave 1) | `54b34896` |
| 2 | feat-6286f19f | RESILIENCE-2 (retry with backoff) | `49d6d12a` |
| 3 | feat-7b195a56 | RESILIENCE-3 (watchdog respawn) | `08878da2` |
| 4a | feat-35519ac2 | REFACTOR-1 (`CollectorLifecycle`) | `1cd3a153`, `65641657` |
| 4b | feat-e195d658 | TEST-1 (closed without dispatch — covered) | — |
| 5a | feat-9104fb56 | CODEX-1 (Codex launcher) | `a7d5e91d` |
| 5b | feat-7b094d00 | GEMINI-1 (Gemini launcher) | `c5a174a7` |
| 6 | feat-c6b3d956 | TEST-2 (multi-harness ingest) | `600918ac` |
| 7 | feat-bc3c0067 | DOC-1 (this document) | (pending commit) |

Quality gates (`go build ./... && go vet ./... && go test ./...`) ran
green on every commit, enforced by the project pre-commit hook.

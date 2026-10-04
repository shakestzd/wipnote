# Artifact-friendly activity report via a read-only MCP server

Status: spike. `wipnote mcp` with the `wipnote_overview` tool is prototyped (`cmd/wipnote/mcp*.go`). The other tools, the skill and the plugin registration are proposals.

## Goal

The dashboard UI is no longer being invested in. Instead ONE long-lived Claude Artifact renders wipnote activity and refreshes live through a connector, rather than a new artifact per request. The artifact calls tools on a local `wipnote mcp` stdio server; the server returns small, bounded, stable JSON. Rendering stays in the artifact HTML.

## What data exists (survey)

| Source | Reliable? | Notes |
|---|---|---|
| `.wipnote/{features,bugs,spikes,tracks,plans}/*.html` | Yes, canonical | `graph.LoadAll` parses ~1,150 items in ~0.2s. Counts, status, `updated`, blocked state. |
| `.wipnote/claims/sess-*.html` (claim episodes) | Yes | One file per root session. Open episode with `EndedAt` zero is a live claim. Collision = 2+ roots on one item. |
| `.wipnote/sessions/<id>/events.ndjson` (OTel shards) | Yes, canonical | Per wipnote session. Gitignored, absent in a fresh clone. This is the only complete cost/tool/permission source. |
| `otel_signals`, `otel_session_rollup` (SQLite) | Stale or empty | The index is now an in-memory projection (`OpenEphemeralProjection`) and never holds OTel unless the dashboard replays every shard into RAM (`startDashboardTelemetryIndexer`). That replay is the cause of the large-project hang. `/api/otel/cost` is only as good as that replay. |
| `statsHandler` cost (`api.go` ~560) | Wrong | Hard-coded per-model prices applied to `messages` tokens. Ignore; OTel `cost_usd` is the single source. |
| `agent_events`, `tool_calls`, session adherence (`DeriveSessionAdherence`, `ListSessionAdherenceTrend`, `commitFeatureSet`) | Expensive | Per-row queries over a full projection hydrate. Not used. |
| `wipnote snapshot`, `analytics`, `recap` | Fine, human-oriented | Counts only, or a heavy diff-grounded narrative. Not machine-shaped. |

Cost caveat discovered while prototyping: Claude emits an `api_request` as both a `log` and a `span` with the same cost. Summing both double counts; the server counts `kind=log` only (same rule as `/api/otel/cost`). A session in a shard is a wipnote session; the `session_id` inside rows is the harness id, and one harness session can span shards.

## Library choice

`go.mod` had no MCP library. I added `github.com/modelcontextprotocol/go-sdk` v1.8.0, the official SDK. It handles initialize, capability negotiation, schema generation from Go structs (input and output schemas, validation) and stdio framing, so the server is ~100 lines instead of a hand-rolled JSON-RPC loop. Cost: seven new modules in `go.mod`/`go.sum` (jsonschema-go, oauth2, segmentio/encoding, uritemplate, x/time, x/oauth2 chain). Alternative if that is unacceptable: a hand-rolled stdio server is feasible (read-only, three methods) but loses schema generation and protocol-version handling.

## Tools (all read-only, `readOnlyHint: true`, structured output schema)

Common rules: ids, counts, durations, USD only. No prompt text, no tool input/output, no `error_msg`. Titles are the only free text, are control-stripped, truncated to 80 runes, and flagged untrusted in a `notice` field and in the server instructions. Every list has a server-side cap and reports `*_total` and `*_truncated`.

1. `wipnote_overview` (built). Input `{since_hours?: int=24 (max 720), max_attention?: int=25 (max 100), max_sessions?: int=10 (max 50)}`. Output: `schema_version`, `generated_at`, `elapsed_ms`, `window{since,hours}`, `work_items{total,by_status,by_type}`, `needs_attention[{kind,severity,ref_type,ref,title?,count?,duration_ms?,detail}]` plus `attention_total`/`attention_truncated`, `cost{total_usd,source,unpriced_requests,top_sessions[],sessions_total,sessions_returned}`, `telemetry{available,shards_in_window,shards_scanned,note?}`, `notice`. Attention kinds: `blocked_work_item`, `stale_in_progress` (>72h), `claim_collision`, `failed_tool_calls`, `permission_waits`, `api_errors`. Each session row: cost, tokens, api_requests, tool_calls, failed_tool_calls, permission_waits and wait ms, api_errors, compactions, subagent_invocations, started/ended.
2. `wipnote_sessions` (proposed). Input `{since_hours?, limit?<=100, sort?: "cost"|"recent"|"failures"}`. Output: the session rows above plus `branch`, `harness`, `parent_session_id` from `sessions-ledger.html`. One shard scan, same code path as overview.
3. `wipnote_session_trace` (proposed). Input `{session_id, top_n?<=20}`. Output: slowest tools (name, calls, p50/max ms, failures), compaction timestamps, subagent tree summary (agent_type, calls, cost; ids only, from `agent_id`/`parent_span`), cost by model. Single shard, so bounded by one file.
4. `wipnote_work_items` (proposed). Input `{status?, type?, stale_days?, limit?<=100}`. Output: id, type, status, priority, updated, claim holders count, cost_usd. Cost per work item uses `claim_episodes` intervals joined in memory to signal `ts` by session (the existing claim-episode design attributes a signal by agent and time), never a query per item.
5. `wipnote_cost` (proposed). Input `{group_by: "session"|"model"|"day"|"work_item", since_hours?, limit?<=100}`. Output buckets with `total_usd`, tokens, `unpriced_requests`. Same scan, different fold.

All five share one scanner, so a refresh costs one pass over in-window shards, never a loop of queries.

## Performance

Budget: under 2s for ~1,000 work items plus a realistic telemetry window, no per-row query loops. Measured on a scratch copy of this repo's real `.wipnote` (1,151 items, 618 features and 784 bugs on disk) plus a synthetic 150-session / 323 MB telemetry corpus (this checkout has no shards; see the report for the caveat):

- First design (signalvtab SQL, one query per shard, serial): about 5.3s. Over budget; JSON decode dominates.
- Shipped (gjson field extraction that skips `attrs`, shards scanned in parallel, one pass each): 0.85-1.0s for a 24h window, up to about 1.3s for 720h, on 4 cores.
- Work-item load alone: about 0.2s. Result size 17 KB.
- Scaling lever if needed: `maxTelemetryShards` (300, newest first) bounds work; a per-shard summary sidecar written at SessionEnd would make scans O(sessions) instead of O(bytes).

## Artifact usage

Declare the capability once and poll with `watchTool`:

```js
// page capabilities: { mcp: { servers: [{ server: "host:wipnote", tools: ["wipnote_overview","wipnote_sessions"] }] } }
window.claude.mcp.watchTool("host:wipnote", "wipnote_overview", { since_hours: 24 },
  (result) => render(result.structuredContent),
  { refetchInterval: 30000 });
```

Because the shape is stable (`schema_version`), the HTML is written once and only the data changes. The page must treat every string field as text (use `textContent`, never `innerHTML`).

Limitation: `host:` servers only answer when the viewer opens the artifact in the Claude desktop app with the local server connected. They do not work on claude.ai web, mobile or in Codespaces/remote sessions (no local process to reach).

Fallback for web and remote viewers: a Claude session (or `/wipnote:report` below) calls the MCP tools or `wipnote mcp`-equivalent code, then writes the JSON into the artifact's `db` capability (a `snapshot` document, replaced each time). The page reads `db` instead of calling a tool and shows `generated_at` so staleness is visible. Same render code, different data source; the page can try `watchTool` first and fall back to `db` on failure.

## Plugin registration (proposal, nothing edited)

`packages/plugin-core/manifest.json` gives Codex and Antigravity an `mcpPath` (`.mcp.json`, `mcp_config.json`); the Claude target has none, so no MCP server is currently shipped to Claude Code. Proposal:

1. Add `"mcpPath": ".mcp.json"` to `targets.claude` and teach `port/pluginbuild` to emit `{"mcpServers":{"wipnote":{"command":"wipnote","args":["mcp"]}}}` (the Claude plugin loader reads `.mcp.json` at the plugin root). Manifest owners to land this; other worktrees own the file.
2. Add a skill `plugin/skills/artifact-report/SKILL.md` (existing skills are `SKILL.md` with `name`/`description` front matter, e.g. `visual-recap`). It would: call `wipnote_overview`, create or update ONE artifact (record its URL in a work-item or `.wipnote` note so later runs republish to the same URL), and for web viewers push the snapshot to `db`. A thin `/wipnote:report` command in `plugin/commands/` invokes the skill.
3. `--project-dir` / `WIPNOTE_PROJECT_DIR` selects the project, since a desktop-launched server's CWD is arbitrary.

## Security notes

Read-only by construction: no write tools, handlers open nothing writable, and `wipnote mcp` is excluded from `persistentPreRunE` (otherwise every launch would register a session and upsert the registry). A test asserts `.wipnote/` is byte-identical after calls and that a secret planted in `attrs`/`error_msg` never appears in a result. Anything derived from transcripts (titles, future tool names, error messages) must stay untrusted data; the doc's rule is to return ids and aggregates by default and add content-bearing tools only behind an explicit opt-in flag.

## Open questions

- Accept the new SDK dependency (seven modules) or hand-roll?
- Should `mcp` (with `--project-dir`) also appear in the Codex/Antigravity MCP scaffolds?
- Is a per-session summary sidecar at SessionEnd wanted, to make refresh O(sessions)?
- Cost per work item: attribute by claim episode intervals (proposed) or by `active_feature_id` on signals?

# wipnote Overview artifact

`wipnote-overview.html` is the source of the Claude Artifact that shows what needs attention in your
wipnote projects: a needs-attention list, cost per day and per model, work items by cost, and (when
live) a per-session trace. It is published once and keeps its data in the artifact's own database, so
nobody regenerates the page.

## Two ways data reaches the page

| Path | Works where | Freshness | Needs |
|---|---|---|---|
| **Snapshots** in the artifact database (`snapshots/<view>-<hours>h`) | Any browser or phone | As fresh as the last upload; the page updates live when documents change | A Claude session that has the artifact tools and your wipnote project |
| **Live** through the local `wipnote mcp` server | Claude desktop app only, owner only | Every 30 s | `claude mcp add wipnote -- wipnote mcp` on the computer that runs the app |

The page prefers live data when it is connected and falls back to the snapshot, always showing the
snapshot's age. Session traces are live only.

## Refresh the snapshots

In a Claude session that has the artifact tools and your wipnote project:

```bash
wipnote report snapshot --dir /tmp/wipnote-snapshot          # windows 24h, 7d, 30d by default
```

This writes one `{generated_at, payload}` document per view and window plus `batch.json`. Upload them
with a single `ArtifactData` batch call using the `writes` array from `batch.json` (documents that
already exist need `if_version`). Views: `overview`, `cost_day`, `cost_model`, `work`, `work_stale`.
Files are written 0600 because they contain work-item titles. Cost and session data come from OTel
files under `.wipnote/sessions/`, which are not in git, so a session running on a clone without them
gets work-item data only.

The page's "Copy refresh prompt" button copies a ready-made instruction for this.

## Update the page

Publish from a **local** Claude session, passing the artifact URL so the link is kept. A cloud session
cannot add local tools to an artifact that already declares a local server, so new MCP tools need a
local publish. Capabilities the artifact declares:

```json
{
  "mcp": {"servers": [
    {"server": "host:wipnote", "tools": ["wipnote_overview", "wipnote_cost", "wipnote_work_items", "wipnote_session_trace"]},
    {"server": "host:plugin_wipnote_wipnote", "tools": ["wipnote_overview", "wipnote_cost", "wipnote_work_items", "wipnote_session_trace"]}
  ]},
  "db": {"rules": [{"path": "snapshots", "read": "view", "write": "admin"}]}
}
```

`host:wipnote` is the name used by `claude mcp add wipnote`. `host:plugin_wipnote_wipnote` is how a
plugin-registered server (`plugin/.mcp.json`) would be named; whether artifacts can reach a
plugin-registered server has not been verified.

Tool schemas live in `docs/design/artifact-report.md`. The page shows only ids, counts, durations and
USD, and renders work-item titles as plain text (they are untrusted).

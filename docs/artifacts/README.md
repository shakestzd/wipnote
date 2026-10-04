# wipnote Overview artifact

`wipnote-overview.html` is the source of the Claude Artifact that shows what needs attention in your
wipnote projects: needs-attention list, cost per day and per model, work items by cost, and a
per-session trace. It reads your local wipnote through the read-only `wipnote mcp` server and refreshes
every 30 seconds, so the artifact is published once and never regenerated.

## Use it

1. Build wipnote from a revision that includes `wipnote mcp` (`wipnote build`).
2. Register the server once: `claude mcp add wipnote -- wipnote mcp`.
3. Open the artifact in the **Claude desktop app**. A local server is reachable only from the app, and
   only by the artifact's owner. The first call asks for consent.

Outside the app the page shows a banner with the setup command, and the Overview tab falls back to a
saved snapshot if one exists in the artifact's `snapshots/overview-<hours>h` documents
(`{generated_at, payload}`, where `payload` is the output of `wipnote report overview`).

## Update it

Publish from a **local** Claude session, passing the artifact URL so the same link is kept. A cloud
session cannot add local tools to an artifact that already declares a local server, so new tools in
`wipnote mcp` need a local publish. The artifact declares these capabilities:

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

Tool schemas live in `docs/design/artifact-report.md`. The page renders only ids, counts, durations and
USD, and shows work-item titles as plain text (they are untrusted).

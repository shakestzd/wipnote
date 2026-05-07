# Security Review (2026-04-15)

This document captures **glaring** security issues identified from a quick code audit.

## 1) Cross-origin access + no auth on local HTTP API (High)

### Why this matters
The dashboard server enables permissive CORS (`*`) for all routes via a shared middleware, and multiple routes include state-changing operations (POST).

In practice, any website opened in the same browser can issue requests to `http://localhost:<port>` and read responses. Because there is no auth boundary, this can expose local project/session data and trigger mutations.

### Evidence
- CORS is globally permissive: `Access-Control-Allow-Origin: *` for all wrapped handlers.
- Mutating APIs exist (e.g. session ingest POST) and are wrapped with that middleware.

### Recommendations
- Default-bind API to loopback (already done) **and** require same-origin checks for mutating endpoints.
- Replace wildcard CORS with explicit allowlist (or disable CORS entirely for non-embed mode).
- Add CSRF protections for mutating endpoints.
- Consider a per-process random bearer token printed at startup for dashboard use.

## 2) Entire `.wipnote` directory is web-accessible (High)

### Why this matters
The server exposes `.wipnote/` through `/wipnote/` using `http.FileServer`. This likely includes:
- `wipnote.db` (full indexed session/event history)
- logs
- plans and internal artifacts

Combined with permissive CORS, a malicious webpage can exfiltrate local project metadata/transcripts from the running dashboard server.

### Evidence
- `/wipnote/` is directly mapped to the on-disk `.wipnote` directory with no file allowlist.

### Recommendations
- Remove raw directory serving in production mode.
- If needed for legacy UI, replace with explicit allowlist endpoints for specific safe files only.
- Block access to `*.db`, `logs/`, and other sensitive/internal files.

## 3) Project metadata leakage in global mode API (Medium)

### Why this matters
`/api/projects` returns each project's absolute local path (`Dir`) and `GitRemoteURL`. If CORS remains permissive, a third-party web page can read this and fingerprint repositories, organization names, or private infrastructure naming.

### Evidence
- The JSON response includes local `Dir` and `GitRemoteURL` directly from registry entries.

### Recommendations
- Do not return absolute paths by default; provide only opaque IDs + display names.
- Gate remote URL exposure behind an explicit flag.
- Restrict CORS / require auth before returning registry metadata.

---

## Risk summary
- **High:** cross-origin unauthenticated API access and raw `.wipnote` file serving.
- **Medium:** metadata overexposure from `/api/projects`.

Together, these issues create a realistic local-data exfiltration path from a normal browser session.

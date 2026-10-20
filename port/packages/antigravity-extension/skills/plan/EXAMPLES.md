# Plan slice examples

Complete slice cards for `/wipnote:plan`. Both use `schema_version: v4`.

## Trivial slice

```yaml
meta:
  id: plan-<generated>
  title: "Fix typo in serve.go log message"
  status: draft
  schema_version: v4
slices:
  - id: slice-1
    num: 1
    title: "Fix typo in serve.go log message"
    why: "User-facing log message reads 'lisening' instead of 'listening'."
    files:
      - cmd/wipnote/serve.go
    effort: S
    risk: Low
    complexity: trivial
    deps: []
```

That's it. No `what`, `done_when`, `tests`, or `decisions_notes` required.

## Complex slice (abbreviated)

```yaml
meta:
  id: plan-<generated>
  title: "Rate-limit /api/ingest"
  status: draft
  schema_version: v4
slices:
  - id: slice-1
    num: 1
    title: "Rate-limit /api/ingest per client IP"
    what: |
      Add token-bucket limiter in internal/ratelimit/limiter.go keyed by
      client IP. Middleware in cmd/wipnote/serve.go returns HTTP 429 with
      JSON body when bucket empty.
    why: |
      Protects the canonical event writer from burst overload (Goal 1 — throughput cap).
    files:
      - internal/ratelimit/limiter.go
      - cmd/wipnote/serve.go
    deps: []
    blocks:
      # Authored FIRST during the Scope stage — `files` was read off this tree.
      - type: file-tree
        title: Files touched
        entries:
          - internal/ratelimit/limiter.go
          - cmd/wipnote/serve.go
      # Authored FIRST during the API/contract stage — `done_when` derived from it.
      - type: data-model
        fields:
          name: "POST /api/ingest — 429 response"
        rows:
          - name: error
            type: 'string ("rate_limited")'
          - name: retry_after
            type: int (seconds)
    done_when:
      - "Requests above the configured limit receive HTTP 429"
      - "Requests within the limit pass through unchanged"
      - "Counter `wipnote_throttled_total` increments on each 429"
    effort: M
    risk: Med
    tests: |
      Unit: TestLimiter_Allow — 10 req/s burst, first 5 allow, next reject
      Integration: TestServeRateLimit — spin up server, assert 429s
    complexity: complex
    decisions_notes: |
      **Trigger:** middleware on /api/ingest, keyed by RemoteAddr.
      **State:** in-memory map[ip]bucket, reset on process restart.
      **Payload:** 429 + {"error":"rate_limited","retry_after":1}.
      **Rejected:** file-backed counters (write amplification under load);
        Redis (new runtime dep — violates constraint).
    questions:
      - id: q-key
        text: "Key by RemoteAddr or X-Forwarded-For?"
        answer: "RemoteAddr — wipnote runs behind localhost only; XFF would need explicit trust list."
```

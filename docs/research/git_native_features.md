Now I have the exact code. Here are the five concrete improvements:

---

## 1. Derive Timestamps from Git Instead of Storing in HTML

**What:** Stop writing `data-created` and `data-updated` into HTML files. Compute them from git history at index time.

**Why:** These attributes drift. If someone edits an HTML file manually, `data-updated` doesn't change. If a rebase rewrites history, the attribute is wrong. Git's commit timestamps are the ground truth.

**What changes:**

| File | Change |
|------|--------|
| `packages/go/internal/workitem/templates/node.gohtml:15-16` | Remove `data-created` and `data-updated` attributes |
| `packages/go/internal/workitem/htmlwriter.go:110-111,165-166` | Remove `CreatedAt`/`UpdatedAt` from template data |
| `packages/go/internal/htmlparse/parser.go` | Stop parsing these attributes (they won't exist) |
| `packages/go/cmd/wipnote/reindex.go` | At index time, shell out to `git log --diff-filter=A --format=%aI -- <file>` for created, `git log -1 --format=%aI -- <file>` for updated |
| `packages/go/internal/db/schema.go` | `features` table keeps `created_at`/`updated_at` columns — they're populated from git during reindex |

**Risk:** Slightly slower reindex (one `git log` call per file). Mitigated by batching: `git log --format='%aI %H' --name-only -- .wipnote/features/` gets all timestamps in one call.

**What you lose:** Nothing real. The HTML files become simpler and the timestamps become trustworthy.

---

## 2. Incremental Reindex via `git diff`

**What:** Track the last-indexed commit. On reindex, only reparse HTML files that git reports as changed.

**Why:** `reindex.go:25-62` currently globs and parses every HTML file on every run. As `.wipnote/` grows, this gets slower linearly. Git knows exactly what changed.

**What changes:**

| File | Change |
|------|--------|
| `packages/go/internal/db/schema.go` | Add `metadata` table: `CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT)` |
| `packages/go/cmd/wipnote/reindex.go` | Before reindex: read `last_indexed_commit` from metadata. Run `git diff --name-only <last_commit> HEAD -- .wipnote/`. Only parse returned files. After reindex: write current HEAD as `last_indexed_commit`. |
| `packages/go/cmd/wipnote/reindex.go` | Keep `--full` flag to force full reparse when needed |

**Implementation sketch:**
```go
// Get changed files since last index
lastCommit := db.GetMetadata("last_indexed_commit")
if lastCommit != "" && !fullReindex {
    cmd := exec.Command("git", "diff", "--name-only", lastCommit, "HEAD", "--", ".wipnote/")
    // parse only these files
} else {
    // existing full glob behavior
}
// after successful reindex:
db.SetMetadata("last_indexed_commit", currentHEAD)
```

**Impact:** Reindex goes from O(all files) to O(changed files). For a repo with 500 work items where 3 changed, that's ~150x faster.

---

## 3. Derive `feature_files` from Git History

**What:** Replace the hook-populated `feature_files` table with data derived from `git_commits` + `git diff-tree`.

**Why:** The current approach (`feature_files_repo.go:14-30`) only captures files touched via Claude Code hooks. It misses:
- Manual edits committed outside a session
- Files touched by other agents without hooks
- Historical work before wipnote was installed

Git knows every file every commit touched. Since `git_commits` already links commits to features, the mapping is derivable.

**What changes:**

| File | Change |
|------|--------|
| `packages/go/internal/hooks/pretooluse.go` | Stop calling `UpsertFeatureFile` on every tool use (remove hot-path overhead) |
| `packages/go/cmd/wipnote/reindex.go` | Add a `reindexFeatureFiles()` pass: for each feature, get linked commits from `git_commits`, run `git diff-tree --no-commit-id -r <commit>` to get files, upsert into `feature_files` |
| `packages/go/internal/db/feature_files_repo.go` | Keep the table and query functions. Change population from "hook-driven append" to "reindex-driven rebuild" |
| `packages/go/cmd/wipnote/backfill.go` | Can be simplified — backfill IS the reindex now |

**Implementation sketch:**
```go
func reindexFeatureFiles(db *sql.DB) error {
    rows, _ := db.Query("SELECT DISTINCT feature_id, commit_hash FROM git_commits WHERE feature_id IS NOT NULL")
    for rows.Next() {
        // git diff-tree --no-commit-id -r <hash>
        // upsert each file path into feature_files
    }
}
```

**Impact:** More accurate data (catches manual commits), removes per-tool-call write overhead, self-healing on reindex.

---

## 4. Adopt Agent Trace Format for Attribution Data

**What:** Align the traceparent format in `attribution.go:15-20` with the [Agent Trace RFC](https://github.com/cursor/agent-trace) that Cursor, Cloudflare, Vercel, Google Jules, and Git AI have adopted.

**Why:** wipnote currently uses a custom `traceparentEntry` struct with custom JSON written to temp files. The Agent Trace standard defines a common format for "which agent contributed which code." Adopting it means:
- Git AI can read wipnote's attribution data
- Agent Blame (Mesa) can read it
- Cursor's tooling can read it
- wipnote can read theirs

**What changes:**

| File | Change |
|------|--------|
| `packages/go/internal/hooks/attribution.go:15-20` | Align `traceparentEntry` fields with Agent Trace schema (contributor ID, tool, session, timestamp, code ranges) |
| `packages/go/internal/hooks/attribution.go:24-45` | Write trace records in Agent Trace JSON format |
| `packages/go/internal/hooks/subagent_start.go` | Include Agent Trace contributor records in delegation events |

**Risk:** The Agent Trace RFC is still evolving. Pin to a specific version and version the format in the output.

**Impact:** Interoperability with the emerging ecosystem. wipnote stops being an island and becomes a node in a network of tools that share attribution data.

---

## 5. Add GitHub Actions Workflow for Quality Gates

**What:** Move quality gate enforcement from local hooks (`quality_gate.go`) to GitHub Actions, running the same checks server-side on PRs.

**Why:** Local hooks can be skipped (`--no-verify`), bypassed by agents without hooks installed, or simply not present on a fresh clone. Server-side enforcement via required status checks is unforgeable.

**What changes:**

| File | Change |
|------|--------|
| `.github/workflows/ci.yml` (new) | Go build, vet, test + wipnote quality checks on every PR |
| `.github/BRANCH_PROTECTION.md` | Update to reference the actual CI workflow (currently references checks that don't exist) |
| `packages/go/internal/hooks/quality_gate.go` | **Keep as-is** — fast local feedback is still valuable. The Action is the enforcement backstop. |

**Workflow:**
```yaml
name: CI
on: [pull_request]
jobs:
  quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: cd packages/go && go build ./...
      - run: cd packages/go && go vet ./...
      - run: cd packages/go && go test ./...
```

Then enable "Require status checks to pass before merging" on the `main` branch.

**Impact:** Quality gates become enforceable, not advisory. Local hooks give fast feedback; GitHub Actions prevent bad merges. Defense in depth.

---

## Summary

| # | Change | Effort | Impact | Risk |
|---|--------|--------|--------|------|
| 1 | Derive timestamps from git | Medium | Simpler HTML, trustworthy data | Slightly slower reindex (mitigable) |
| 2 | Incremental reindex via `git diff` | Small | ~100x faster reindex at scale | Need `--full` escape hatch |
| 3 | Derive `feature_files` from git history | Medium | More accurate, removes hot-path writes | Depends on `git_commits` completeness |
| 4 | Adopt Agent Trace format | Medium | Ecosystem interoperability | RFC still evolving |
| 5 | GitHub Actions CI | Small | Unforgeable quality enforcement | None — additive |

I'd do them in order: **5 → 2 → 1 → 3 → 4**. Start with the zero-risk additive change (CI), then the easy performance win (incremental reindex), then the data model simplifications, then the ecosystem play.
# htmlgraph blame

Reverse-lookup which features and tracks touched a given file path.

## Synopsis

```
htmlgraph blame <path> [--format text|json|markdown] [--since YYYY-MM-DD] [--top N]
```

## Description

`htmlgraph blame` queries the `feature_files` attribution index and returns every
feature that has touched the specified file path, rolled up by track with touch counts
and last-seen timestamps. Touch count is the number of times a feature's work was
attributed to that file (proxy for code churn).

This command is the foundational primitive for file-to-track lineage. The `htmlgraph
code-areas` command (feat-7f1ac9a4) builds on top of the same query helper.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: `text`, `json`, or `markdown` |
| `--since` | — | Filter to touches after this date (`YYYY-MM-DD` or RFC3339) |
| `--top N` | `0` (unlimited) | Return only top N features by touch count |

## Recipes

### Who touched this file?

Basic invocation — lists all features and tracks that touched a file:

```bash
htmlgraph blame internal/db/schema.go
```

Output (text):
```
────────────────────────────────────────────────────────────
  Blame: internal/db/schema.go
────────────────────────────────────────────────────────────

  Total touches: 14  Features: 7  Tracks: 3

  Tracks:
    ID            Title                 Features  Touches
    trk-08dcbb33  Workitem Attribution  4         8
    trk-abc123    Core DB Layer         2         4
    trk-xyz789    Schema Migrations     1         2

  Features:
    ID          Title                        Track         Touches  Last Seen
    feat-abc    Add feature_files table      trk-abc123    3        2025-02-14
    feat-def    Schema migration helpers     trk-xyz789    2        2025-01-30
    ...
```

### Which track owns this directory?

Combine `find` + `xargs` + `jq` to aggregate blame data across all files in a directory:

```bash
find internal/db -type f -name '*.go' | \
  xargs -n1 htmlgraph blame --format json | \
  jq -s '[.[] | .tracks[]] | group_by(.id) | map({id: .[0].id, title: .[0].title, total_touches: (map(.touch_count) | add)}) | sort_by(-.total_touches)'
```

This produces a ranked list of tracks by how much work they contributed to the directory.

### Audit untracked files

Find source files that have no attribution in the blame index. Requires `comm` and `git`:

```bash
# All tracked source files (from git)
git ls-files '*.go' | sort > /tmp/git-files.txt

# Files that appear in blame results (have at least one feature touch)
# Build a list by scanning all go files through blame --format json
git ls-files '*.go' | \
  xargs -I{} sh -c 'htmlgraph blame --format json {} 2>/dev/null | jq -r "if .features | length > 0 then .path else empty end"' | \
  sort > /tmp/blamed-files.txt

# Files in git but NOT in blame — these have no attribution
comm -23 /tmp/git-files.txt /tmp/blamed-files.txt
```

Run `htmlgraph reindex` to rebuild the attribution index if the list is unexpectedly large.

# `wipnote migrate-tracks`

Backfill feature track attribution by classifying each feature's `feature_files`
paths against a YAML rule catalog. Confident moves are applied; ambiguous
features are flagged for manual review rather than silently re-attributed.

## When to use

After a track reorganization (rename, split, merge), existing features keep
the track they were created with. `migrate-tracks` walks every feature, looks
at the file paths it has touched, and proposes re-attribution to the track
whose code surface dominates.

## Usage

```
wipnote migrate-tracks --rules <yaml> [--dry-run|--write]
                          [--types features,bugs]
                          [--ambiguity-threshold 0.6]
                          [--format text|json] [--force]
```

| Flag | Default | Notes |
|------|---------|-------|
| `--rules` | (required) | Path to the rule YAML catalog. |
| `--dry-run` | `true`  | Preview without writing. Default mode. |
| `--write`   | `false` | Apply moves and write a manifest. |
| `--types`   | `features` | Comma-separated: `features`, `bugs`. |
| `--ambiguity-threshold` | `0.6` | Minimum dominant-track share (0..1). |
| `--format`  | `text` | `text` or `json`. |
| `--force`   | `false` | Required to overwrite an existing manifest. |

## Rules file format

```yaml
rules:
  - { glob: "cmd/wipnote/yolo.go",     track_id: "trk-be293476", priority: 110 }
  - { glob: "internal/blame/**",         track_id: "trk-08dcbb33", priority: 100 }
  - { glob: "plugin/agents/*.md",        track_id: "trk-2c83a1e2", priority: 100 }
```

- `glob`: path pattern. `*` matches a single segment; `**` matches across `/` boundaries.
- `track_id`: target track for files matching the glob.
- `priority`: higher wins on overlap. Use `>=110` for exact-file rules and
  `100` for directory-level rules.

Paths captured in `feature_files` may be absolute (when tool hooks ran inside
a worktree). The classifier normalizes those by stripping the worktree prefix
(`.../.claude/worktrees/<name>/`) or the longest path-prefix that ends just
before a recognized top-level repo directory (`cmd/`, `internal/`, `plugin/`,
etc.) before glob matching.

## Decision categories

| Reason | Action in `--write` mode |
|--------|--------------------------|
| `confident`      | Move applied. Dominant share ≥ threshold and current track ≠ proposed. |
| `ambiguous`      | NOT moved. No track exceeded threshold; flag for human review. |
| `no-attribution` | NOT moved. Feature has zero `feature_files` rows (orphan in the read index). |
| `no-match`       | NOT moved. Feature touched files but none matched any rule. |
| `no-change`      | NOT moved. Current track is already dominant. |

## Manifest

Every `--write` invocation records a manifest at
`.wipnote/migrations/track-backfill-<unix>.json` containing the rules path
and the full Decision array (every feature considered, including the ones
left untouched). To roll back a move, edit the feature's track via
`wipnote feature update <id> --track <old-track>`.

To prevent stomping on a prior run, `--write` refuses to proceed if any
`track-backfill-*.json` file already exists in `.wipnote/migrations/`.
Pass `--force` to override.

## Refresh the read index after `--write`

The migration writes through the canonical HTML store. The SQLite read
index that powers `wipnote blame` and `wipnote code-areas` only
catches up after a full reindex:

```
wipnote reindex --full
```

The incremental reindex skips files whose `data-track-id` changed but
whose `data-updated` timestamp didn't, so a `--full` pass is required.

## Smoke tests

Verify the migration landed by running blame on canonical files:

```
wipnote blame cmd/wipnote/yolo.go         # expect dominant: trk-be293476 (Yolo Mode)
wipnote blame internal/hooks/yolo_guard.go  # expect dominant: trk-be293476
wipnote blame plugin/agents/sonnet-coder.md # expect dominant: trk-2c83a1e2 (Subagents)
```

`wipnote code-areas --format markdown > docs/code-areas.md` regenerates
the per-track inventory snapshot that quantifies the rebalance.

## Scope notes

- Bugs are opt-in via `--types bugs` (deferred to a follow-up — features
  are the dominant work-item type and the highest-impact target for
  re-attribution).
- The classifier only operates on work items present in the canonical
  HTML store. Features that exist in `feature_files` but not in
  `.wipnote/features/*.html` are silently ignored — repairing those
  orphans is a separate upstream concern.

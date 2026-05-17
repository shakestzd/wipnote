# Launcher Isolation Runbook

This runbook documents the staged rollout of wipnote launcher isolation — the
transition from warn-only (host-production default) to worktree-enforced
isolation — and the gate conditions that must pass before advancing.

---

## Background

wipnote launchers (claude, codex, gemini, yolo) can run either in-place (in
the main worktree) or inside a managed git worktree isolated per work item.
Slice-8 (plan-1670cacd) defined four deployment profiles:

| Profile | Default Rollout |
|---------|----------------|
| `host-production` | warn-only — never enforced by default |
| `devcontainer-dev` | config-gated (opt-in) |
| `ci-test` | config-gated (opt-in) |
| `local-plugin-dev` | warn-only |

**The host-production profile NEVER defaults to enforcement.** This is a
deliberate safety constraint: flipping a host to mandatory isolation has
user-visible consequences (every launch without a work-item ID would be
refused or degraded). The gate exists to prevent accidental breakage.

---

## Rollout Gate (slice-9)

Before the host profile can advance from warn-only to config-gated or
worktree-by-default, ALL of the following must hold:

1. **Doctor checks pass** — `wipnote launcher doctor` reports no stale
   worktrees, no canonical-root mismatch, and clean git state.

2. **Orphan sessions cleaned** — `wipnote cleanup orphan-sessions` reports
   zero orphans (or the user has acknowledged and dismissed them).

3. **Session reconciliation clean** — `wipnote reconcile` reports no
   ambiguous generator drift (runs clean or with `--strict` exit 0).

4. **Legacy sessions are visible** — Old sessions without `session_family_id`
   must remain visible in the dashboard labeled "legacy" (not hidden or
   broken). Verify with `wipnote session list`.

5. **Operator opt-in** — Set `WIPNOTE_ENFORCE_ISOLATION=true` in the
   environment before running any launcher. This is NOT set automatically.

---

## Step-by-Step: Advance Host to Enforced Mode

```bash
# Step 1: Run the doctor and read its output carefully.
wipnote launcher doctor

# Step 2: Clean up orphan sessions (non-destructive list first).
wipnote cleanup orphan-sessions --dry-run
# When satisfied, remove them:
wipnote cleanup orphan-sessions

# Step 3: Reconcile session artifacts.
wipnote reconcile --strict

# Step 4: Prune stale worktree admin entries if doctor flagged any.
# The doctor prints the exact command — e.g.:
git -C /path/to/repo worktree prune

# Step 5: Opt in on the host.
export WIPNOTE_ENFORCE_ISOLATION=true

# Step 6: Launch as normal — managed worktree will be required.
wipnote claude --work-item feat-xxxx
```

---

## Reverting Enforcement

Simply unset the environment variable:

```bash
unset WIPNOTE_ENFORCE_ISOLATION
```

The launcher falls back to warn-only immediately. No state is persisted.

---

## Legacy Session Compatibility

Sessions created before slice-4 (session_family_id introduced) have no
family metadata. These sessions:

- Remain **fully visible** in the dashboard (not hidden or broken).
- Are labeled **"legacy"** in `wipnote launcher doctor` output.
- Do NOT need migration; they age out naturally as new sessions replace them.

No action is required for legacy sessions. The doctor reports their count
for informational purposes only.

---

## What the Doctor Checks

`wipnote launcher doctor` reports (non-destructively):

| Check | What it detects |
|-------|----------------|
| Git state | Dirty main branch, ahead/behind origin |
| Managed worktrees | Stale entries (directory removed but admin record exists) |
| Session family | Legacy sessions count, canonical-root divergence |
| Rollout gate | Whether `WIPNOTE_ENFORCE_ISOLATION` is active |

The doctor **never auto-mutates** anything. It prints `git worktree prune`
and `wipnote cleanup orphan-sessions` as guidance but does not run them.

---

## Delegated Commands

The doctor explicitly delegates to two external commands:

- **`wipnote cleanup orphan-sessions`** — plan-ae0c37b2 slice-11 (feat-2c631aa9).
  Handles orphan NDJSON session directories with no DB row.
- **`wipnote reconcile`** — plan-83f909bc slice-5 (feat-f93fe770).
  Auto-commits done-but-uncommitted work-item artifacts and reports generator drift.

Do NOT reimplement their logic inside the doctor. Always delegate.

---

## Release Guidance

Recommended release order for flipping host default:

1. Ship this runbook + `wipnote launcher doctor` (slice-9, done).
2. Communicate the gate to users in release notes.
3. Allow a soak period (≥1 release cycle) in warn-only mode.
4. Re-run doctor against real user installations.
5. When all gate conditions hold: update `profile.go` to set
   `EnforceIsolation: true` for the `host-production` profile, gated on
   a new config key (e.g. `WIPNOTE_ISOLATION_PROFILE=enforced`).
6. Do NOT flip the default unconditionally — always require explicit opt-in.

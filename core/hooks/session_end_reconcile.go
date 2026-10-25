package hooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// --- Session-exit reconciliation (slice-5, feat-f93fe770) ---

// ReconcileReport is the structured result of a reconcile pass. It is consumed
// by both `wipnote reconcile` (CLI) and the Stop/SessionEnd hook handlers.
//
//   - AutoCommitted: done-but-uncommitted artifacts that were auto-committed
//     during this pass (deterministic bookkeeping — never blocks).
//   - PortDrift: generator-touched-without-build-ports paths reported by
//     the injected PortDriftPathsFn (see lifecycle_injection.go).
//   - Orphaned: in-progress work items with no live owning session (reported
//     only — never auto-resolved).
//
// HasAmbiguousDrift() is the single signal the harness discriminator keys on:
// when true and harness==claude the Stop handler returns BlockExit2Error; for
// Gemini/Codex a durable warning is persisted instead.
type ReconcileReport struct {
	AutoCommitted []string `json:"auto_committed,omitempty"`
	PortDrift     []string `json:"port_drift,omitempty"`
	Orphaned      []string `json:"orphaned,omitempty"`

	// ReapedSessions and ReapedCollectors are the forced-reap audit trail
	// produced by ReapStaleSessionsAndCollectors (feat-88f92f44). They are
	// deliberately report-only JSON fields with NO backing schema/DB column —
	// this struct IS the audit trail; a migration would break migrations_test.go
	// and there is no query that needs them indexed. ReapedSessions holds the
	// session ids transitioned active→completed; ReapedCollectors holds
	// "<sid>:<pid>" entries for each identity-verified orphan collector killed.
	ReapedSessions   []string `json:"reaped_sessions,omitempty"`
	ReapedCollectors []string `json:"reaped_collectors,omitempty"`
}

// HasAmbiguousDrift reports whether the pass found unresolved source-ambiguous
// drift that a human must reconcile (generator-touched-without-build-ports).
// done-but-uncommitted items are NOT ambiguous — reconcile fixed them
// deterministically by auto-committing the artifact, so they never gate exit.
// Orphaned items are reported but are also not exit-gating (a session ending
// is the expected time to surface them, not to block on them).
func (r *ReconcileReport) HasAmbiguousDrift() bool {
	return r != nil && len(r.PortDrift) > 0
}

// Empty reports whether the pass found nothing actionable at all.
func (r *ReconcileReport) Empty() bool {
	return r == nil ||
		(len(r.AutoCommitted) == 0 && len(r.PortDrift) == 0 && len(r.Orphaned) == 0 &&
			len(r.ReapedSessions) == 0 && len(r.ReapedCollectors) == 0)
}

// reconcileArtifactCommitFn is the injection seam for the deterministic
// artifact auto-commit. Production wires it to a git-add+commit of the single
// work-item HTML path. Tests override it to assert the call without mutating a
// real repo. It returns true when a new commit was actually created.
var reconcileArtifactCommitFn = defaultReconcileArtifactCommit

// Reconcile runs a full session-exit reconciliation pass against projectDir.
//
// strict only affects the CLI surface (exit code); the detection itself is
// identical. The three classes are:
//
//  1. done-but-uncommitted → auto-commit the artifact (deterministic
//     bookkeeping) and record it under AutoCommitted. This CANNOT strand a
//     later `wipnote feature complete`: slice-4 gate records are session-local
//     and re-checked at complete, and the complete path's own strict-commit is
//     idempotent on an already-committed unchanged artifact (returns no-op,
//     HEAD must-not-advance branch) — so a reconcile pre-commit is forward
//     compatible.
//  2. generator-touched-without-build-ports → delegated to the injected
//     PortDriftPathsFn (see lifecycle_injection.go; feat-29195f33).
//     Skipped when skipPortDrift is true (per-turn Stop path).
//  3. started-but-orphaned → reported only.
//
// database is accepted for call-shape compatibility and IGNORED: both
// item-shaped classes now read canonical state (reconcile_canonical.go), so a
// nil handle no longer disables them — which it silently did for every caller,
// since every caller passed nil once the project read index went away.
func Reconcile(database *sql.DB, projectDir string, strict bool, skipPortDrift ...bool) (*ReconcileReport, error) {
	_ = strict   // detection is identical; strict only changes CLI exit semantics
	_ = database // canonical-only; see doc comment
	rep := &ReconcileReport{}

	rep.AutoCommitted = reconcileDoneButUncommittedCanonical(projectDir)
	rep.Orphaned = reconcileStartedButOrphanedCanonical(projectDir)
	if len(skipPortDrift) == 0 || !skipPortDrift[0] {
		rep.PortDrift = reconcilePortDrift(projectDir)
	}

	sort.Strings(rep.AutoCommitted)
	sort.Strings(rep.PortDrift)
	sort.Strings(rep.Orphaned)
	return rep, nil
}

// reconcileCheapClasses runs ONLY the latency-cheap reconcile classes — the
// orphan scan and (unless skipped) the delegated port-drift check — and
// deliberately OMITS the done-but-uncommitted class, whose per-item git fork
// loop (git status/add/diff/commit over every dirty terminal artifact) is the
// ~5.45s synchronous cost the Stop hot path used to pay on EVERY model response
// (feat-c08d1ba1 slice-6, profiled at ~7.2s for 200 dirty done-features).
//
// This is sound for the Stop discriminator contract: per runSessionExitReconcile,
// only ambiguous PortDrift can block (Claude) or warn (Gemini/Codex), and the
// done-but-uncommitted auto-commit "never blocks any harness" — it is purely a
// deterministic side effect. Deferring it loses nothing as long as it still runs
// eventually, which ReconcileDoneButUncommittedForProject guarantees via the
// serve-side drain loop (mirrors the bug-504095f2 orphan-drain split). On the
// per-turn Stop path skipPortDrift is also true, so this reduces to the claim-
// ledger orphan scan alone: a few small shard reads and a kill(pid,0) each,
// which is the canonical equivalent of the single query it replaces.
//
// database is accepted for call-shape compatibility and ignored (see Reconcile).
func reconcileCheapClasses(database *sql.DB, projectDir string, skipPortDrift bool) (*ReconcileReport, error) {
	_ = database
	rep := &ReconcileReport{}
	rep.Orphaned = reconcileStartedButOrphanedCanonical(projectDir)
	if !skipPortDrift {
		rep.PortDrift = reconcilePortDrift(projectDir)
	}
	sort.Strings(rep.PortDrift)
	sort.Strings(rep.Orphaned)
	return rep, nil
}

// ReconcileDoneButUncommittedForProject runs ONLY the done-but-uncommitted
// auto-commit class across the project, with NO cap. It is the deferred,
// off-hot-path counterpart to the class reconcileCheapClasses omits: the
// writer daemon calls it on a low-frequency loop (startWriterMaintenance's reconcile drain)
// so the deterministic artifact auto-commit the Stop hook no longer performs
// synchronously still completes — exactly the split bug-504095f2 used for the
// orphan sweep. Returns the list of work-item IDs whose artifacts were committed
// this pass.
//
// The "no cap" claim is now structural rather than a paging loop: the canonical
// scan enumerates DIRTY artifacts and reads only their status, so there is no
// newest-N window an older dirty artifact can fall below (the defect roborev-478
// finding 2 had to page around). The hot Stop path is unchanged — it never calls
// this, it runs reconcileCheapClasses.
//
// database is accepted for call-shape compatibility and ignored (see Reconcile).
func ReconcileDoneButUncommittedForProject(database *sql.DB, projectDir string) []string {
	_ = database
	return reconcileDoneButUncommittedCanonical(projectDir)
}

// reconcilePortDrift delegates to the injected PortDriftPathsFn
// (feat-29195f33). The checker owns the manifest-presence gate and the
// regenerate-and-compare; core never reimplements port diffing. Returns
// the drifted paths, or nil when in sync / not a plugin-core repo.
func reconcilePortDrift(projectDir string) []string {
	// Delegated to the injected PortDriftPathsFn so this core reconcile path
	// does not import port-generation tooling. A nil fn (tooling not wired)
	// means no drift to reconcile.
	if PortDriftPathsFn == nil {
		return nil
	}
	return PortDriftPathsFn(reconcileRepoRoot(projectDir))
}

// reconcileRepoRoot walks up from projectDir to the directory containing a
// .wipnote/ store, treating that as the repo root the artifacts live under.
func reconcileRepoRoot(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	for d := abs; ; {
		if fi, err := os.Stat(filepath.Join(d, ".wipnote")); err == nil && fi.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// defaultReconcileArtifactCommit stages and commits exactly the one artifact
// path. Mirrors cmd/wipnote/workitem_commit.go's non-fatal contract: a failure
// to commit is logged and treated as "not committed" — reconcile never makes
// session exit depend on git succeeding. Returns true iff a new commit landed.
func defaultReconcileArtifactCommit(repoRoot, absPath, relPath, id string) bool {
	if out, err := exec.Command("git", "-C", repoRoot, "add", "--", absPath).CombinedOutput(); err != nil {
		debugLog(repoRoot, "[reconcile] git add %s failed: %s", relPath, strings.TrimSpace(string(out)))
		return false
	}
	// Nothing staged → already committed and unchanged: idempotent no-op.
	if err := exec.Command("git", "-C", repoRoot, "diff", "--cached", "--quiet", "--", absPath).Run(); err == nil {
		return false
	}
	msg := "wipnote: reconcile " + id
	if out, err := exec.Command(
		"git", "-C", repoRoot, "commit", "-m", msg, "--", absPath,
	).CombinedOutput(); err != nil {
		o := string(out)
		if strings.Contains(o, "nothing to commit") || strings.Contains(o, "no changes added") {
			return false
		}
		debugLog(repoRoot, "[reconcile] git commit %s failed: %s", id, strings.TrimSpace(o))
		return false
	}
	return true
}

// --- Durable warnings (Gemini/Codex non-blocking surface) ---

// reconcileWarning is one persisted warning record. Persisted to
// .wipnote/.reconcile-warnings.jsonl so the user-never-returns case is still
// recorded, then rendered (and consumed) at the next SessionStart.
type reconcileWarning struct {
	Timestamp string   `json:"timestamp"`
	Harness   string   `json:"harness"`
	SessionID string   `json:"session_id,omitempty"`
	PortDrift []string `json:"port_drift,omitempty"`
	Orphaned  []string `json:"orphaned,omitempty"`
}

// reconcileWarningsPath is the durable warnings log under .wipnote/.
func reconcileWarningsPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", ".reconcile-warnings.jsonl")
}

// persistReconcileWarning appends a durable warning for the Gemini/Codex path.
// It is append-only JSONL so concurrent sessions never corrupt each other and
// a warning survives even if the user never returns to this session.
func persistReconcileWarning(projectDir, harness, sessionID string, rep *ReconcileReport) error {
	if rep == nil || (!rep.HasAmbiguousDrift() && len(rep.Orphaned) == 0) {
		return nil
	}
	w := reconcileWarning{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Harness:   harness,
		SessionID: sessionID,
		PortDrift: rep.PortDrift,
		Orphaned:  rep.Orphaned,
	}
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(reconcileWarningsPath(projectDir),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// DrainReconcileWarnings reads and removes the durable warnings log, returning
// a human-readable block to surface at SessionStart, or "" when there are
// none. Consuming (deleting) the log makes the surface idempotent: the next
// session does not re-show stale warnings the user has already seen.
func DrainReconcileWarnings(projectDir string) string {
	path := reconcileWarningsPath(projectDir)
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return ""
	}
	_ = os.Remove(path)

	var lines []string
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var w reconcileWarning
		if err := json.Unmarshal([]byte(ln), &w); err != nil {
			continue
		}
		if len(w.PortDrift) > 0 {
			lines = append(lines, fmt.Sprintf(
				"  - [%s] generator drift not reconciled: %s — run `wipnote plugin build-ports` and commit",
				w.Harness, strings.Join(w.PortDrift, ", ")))
		}
		if len(w.Orphaned) > 0 {
			lines = append(lines, fmt.Sprintf(
				"  - [%s] orphaned in-progress items from a prior session: %s",
				w.Harness, strings.Join(w.Orphaned, ", ")))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Unreconciled work from a previous session\n\n" +
		"A prior session ended with reconciliation drift that was recorded but not blocked:\n\n" +
		strings.Join(lines, "\n")
}

// runSessionExitReconcile is the shared Stop/SessionEnd entry point. It runs a
// reconcile pass and applies the harness discriminator:
//
//   - harness=="claude" AND ambiguous drift → return BlockExit2Error (exit-2).
//     This deliberately AMENDS missing_events.go's historical no-block contract
//     for the Stop handler; the amendment is intended and scoped to ambiguous
//     generator drift only.
//   - Gemini/Codex → never block. Persist a DURABLE warning (so the
//     user-never-returns case is still recorded) for SessionStart to surface.
//
// done-but-uncommitted auto-commits and orphan reports never block any harness.
//
// When skipPortDrift is true, the expensive port-drift check is skipped
// entirely — used by the per-turn Stop path so it does not pay 8-22s of full
// regeneration on every model response. The commit-time
// checkPortDriftCommitGuard (commit_portdrift_guard.go) enforces port-drift
// correctness instead, firing only when generator-input files are staged.
//
// HOT-PATH SPLIT (feat-c08d1ba1 slice-6): this entry point runs only
// reconcileCheapClasses — the pure-SQL orphan scan plus (unless skipped) the
// delegated port-drift check. It NO LONGER runs the done-but-uncommitted
// auto-commit, whose per-item git fork loop was the ~5.45s synchronous cost the
// Stop hook paid on every model response. That class is now drained off the hot
// path by the writer daemon (startWriterMaintenance's reconcile drain →
// ReconcileDoneButUncommittedForProject), so the deterministic artifact
// auto-commit still completes — just not on the interactive path. This is sound
// because only ambiguous PortDrift gates the discriminator below; the
// auto-commit class never blocked any harness.
func runSessionExitReconcile(database *sql.DB, projectDir, harness, sessionID string, skipPortDrift bool) error {
	rep, err := reconcileCheapClasses(database, projectDir, skipPortDrift)
	if err != nil || rep.Empty() {
		return nil
	}
	return discriminateReconcile(rep, harness, projectDir, sessionID)
}

// discriminateReconcile applies the harness discriminator to an already-
// computed report. Extracted from runSessionExitReconcile so it is unit-
// testable with a synthetic report (no git/DB needed).
//
//   - harness=="claude" + ambiguous drift → BlockExit2Error (exit-2). This is
//     the intentional, narrowly-scoped amendment to the Stop handler's
//     historical no-block contract.
//   - Gemini/Codex → never block; persist a DURABLE warning so the
//     user-never-returns case is still recorded and SessionStart can surface it.
func discriminateReconcile(rep *ReconcileReport, harness, projectDir, sessionID string) error {
	if harness == "claude" {
		if rep.HasAmbiguousDrift() {
			return &BlockExit2Error{Message: fmt.Sprintf(
				"session-exit reconcile: generator-touched files committed without "+
					"regenerating ports (%s). Run `wipnote plugin build-ports` and "+
					"commit the regenerated trees before exiting.",
				strings.Join(rep.PortDrift, ", "))}
		}
		return nil
	}

	// Gemini / Codex: durable, non-blocking.
	if err := persistReconcileWarning(projectDir, harness, sessionID, rep); err != nil {
		debugLog(projectDir, "[reconcile] persist warning failed: %v", err)
	}
	return nil
}

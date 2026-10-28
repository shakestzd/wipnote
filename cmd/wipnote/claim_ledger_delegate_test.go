package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
)

// Slice 16 (plan-28b4a7f6): the claim gate treats a caller's live workers as
// its family, and a worker as family of its caller. The spawn-side claims
// (one worker per item, the lost-pane expired close) are tested with the
// herdr fake in internal/delegate/session/claims_test.go.

const (
	dgItem   = "feat-0000dlg1"
	dgCaller = "019ee378-abcd-7000-8000-00000000d0c1"
	dgWorker = "019ee378-abcd-7000-8000-00000000d0a1"
	dgClear  = "019ee378-abcd-7000-8000-00000000d0a2" // the worker after /clear
	dgOther  = "019ee378-abcd-7000-8000-00000000d0f1" // unrelated live session
)

// newDelegateClaimDir is a temp .wipnote with no inherited project or
// delegate env, so nothing touches the real project.
func newDelegateClaimDir(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", delegateRunEnv} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	dir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dgOpenEpisode(t *testing.T, dir, sid string) {
	t.Helper()
	recordClaimEpisodeOpen(nil, dir, sid, "", dgItem, time.Now().UTC().Add(-time.Minute))
	if !dgEpisodeOpen(t, dir, sid) {
		t.Fatalf("no open episode for %s", sid)
	}
}

func dgEpisodeOpen(t *testing.T, dir, sid string) bool {
	t.Helper()
	eps, err := claimledger.NewStore(dir).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.SessionID == sid && e.WorkItemID == dgItem && e.IsOpen() {
			return true
		}
	}
	return false
}

// dgOpenRow opens a delegation row as spawn does; workers are its bound
// worker_sessions (moved there by list).
func dgOpenRow(t *testing.T, dir, mode, caller string, workers ...string) string {
	t.Helper()
	s := delegationledger.NewStore(dir)
	r, err := s.Open(delegationledger.Record{Mode: mode, Harness: "claude", WorkItem: dgItem,
		CallerSession: caller, Started: time.Now().UTC().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) > 0 {
		if _, err := s.Enrich(r.ID, delegationledger.Enrichment{WorkerSessions: workers}); err != nil {
			t.Fatal(err)
		}
	}
	return r.ID
}

func dgForeign(t *testing.T, dir, caller string) map[string]bool {
	t.Helper()
	live, ok := liveForeignClaims(dir, dgItem, caller)
	if !ok {
		t.Fatal("liveForeignClaims could not read the ledgers")
	}
	got := map[string]bool{}
	for _, e := range live {
		got[e.SessionID] = true
	}
	return got
}

func TestLiveForeignClaims_WorkerIsFamilyOfCaller(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	dgOpenEpisode(t, dir, dgWorker)
	if got := dgForeign(t, dir, dgCaller); !got[dgWorker] {
		t.Fatalf("no delegation row yet, the worker must be foreign: %v", got)
	}
	dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker)
	if got := dgForeign(t, dir, dgCaller); len(got) != 0 {
		t.Fatalf("the caller's live worker is family, got foreign %v", got)
	}
	if err := checkLiveCollisionGate(dir, dgItem, dgCaller); err != nil {
		t.Fatalf("the orchestrator re-start was refused: %v", err)
	}
	// Other side: a row whose caller is someone else makes no family.
	if got := dgForeign(t, dir, dgOther); !got[dgWorker] || !got[dgCaller] {
		t.Fatalf("another caller sees %v, want worker and caller foreign", got)
	}
}

func TestLiveForeignClaims_CallerIsFamilyOfWorker(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	run := dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller)
	if got := dgForeign(t, dir, dgWorker); !got[dgCaller] {
		t.Fatalf("without %s the orchestrator must be foreign: %v", delegateRunEnv, got)
	}
	t.Setenv(delegateRunEnv, run)
	if got := dgForeign(t, dir, dgWorker); len(got) != 0 {
		t.Fatalf("the worker's caller is family, got foreign %v", got)
	}
	// Other side: the env var is trusted only when it names an open
	// interactive row.
	t.Setenv(delegateRunEnv, "dlg-000000000000")
	if got := dgForeign(t, dir, dgWorker); !got[dgCaller] {
		t.Fatalf("an unknown run made family: %v", got)
	}
	t.Setenv(delegateRunEnv, dgOpenRow(t, dir, delegationledger.ModeOneshot, dgCaller))
	if got := dgForeign(t, dir, dgWorker); !got[dgCaller] {
		t.Fatalf("a one-shot run made family: %v", got)
	}
}

func TestLiveForeignClaims_UnrelatedSessionStillRefused(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	dgOpenEpisode(t, dir, dgOther)
	t.Setenv(delegateRunEnv, dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker))
	got := dgForeign(t, dir, dgWorker)
	if !got[dgOther] || got[dgCaller] || len(got) != 1 {
		t.Fatalf("foreign %v, want only the unrelated session", got)
	}
	err := checkLiveCollisionGate(dir, dgItem, dgWorker)
	if err == nil || !strings.Contains(err.Error(), dgOther) || strings.Contains(err.Error(), dgCaller) {
		t.Fatalf("gate = %v, want a refusal naming only %s", err, dgOther)
	}
}

func TestLiveForeignClaims_EndedRowIsNotFamily(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	dgOpenEpisode(t, dir, dgWorker)
	run := dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker)
	if got := dgForeign(t, dir, dgCaller); len(got) != 0 {
		t.Fatalf("open row: foreign %v", got)
	}
	if err := delegationledger.NewStore(dir).Close(run, delegationledger.CloseInfo{Ended: time.Now().UTC(), Reason: delegationledger.EndStop}); err != nil {
		t.Fatal(err)
	}
	if got := dgForeign(t, dir, dgCaller); !got[dgWorker] {
		t.Fatalf("an ended row still made family: %v", got)
	}
	t.Setenv(delegateRunEnv, run)
	if got := dgForeign(t, dir, dgWorker); !got[dgCaller] {
		t.Fatalf("an ended row trusted the worker's env: %v", got)
	}
}

func TestLiveForeignClaims_UnreadableDelegationLedgerFailsOpen(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgOther)
	// A missing delegations ledger is readable and empty: the gate refuses.
	if err := checkLiveCollisionGate(dir, dgItem, dgWorker); err == nil {
		t.Fatal("a missing delegations ledger must not open the gate")
	}
	// A ledger that exists but cannot be read fails open.
	if err := os.Mkdir(filepath.Join(dir, delegationledger.FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := liveForeignClaims(dir, dgItem, dgWorker); ok {
		t.Fatal("an unreadable delegations ledger must report ok=false")
	}
	if err := checkLiveCollisionGate(dir, dgItem, dgWorker); err != nil {
		t.Fatalf("an unreadable delegations ledger must fail open, got %v", err)
	}
}

// TestSpawn_WorkerClaimsItem_CoClaim is the claim half of the spawned
// worker's first brief line (`wipnote feature start <id>`): the gate lets it
// in beside the orchestrator and both episodes are open.
func TestSpawn_WorkerClaimsItem_CoClaim(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	run := dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller)
	if err := checkLiveCollisionGate(dir, dgItem, dgWorker); err == nil {
		t.Fatal("without the run env the worker must be refused")
	}
	t.Setenv(delegateRunEnv, run)
	if err := checkLiveCollisionGate(dir, dgItem, dgWorker); err != nil {
		t.Fatalf("the worker was refused beside its orchestrator: %v", err)
	}
	dgOpenEpisode(t, dir, dgWorker)
	if !dgEpisodeOpen(t, dir, dgCaller) || !dgEpisodeOpen(t, dir, dgWorker) {
		t.Fatal(".wipnote/claims/ must hold open episodes for the worker and the orchestrator")
	}
}

func TestOrchestratorEnd_WorkerKeepsClaim(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	dgOpenEpisode(t, dir, dgWorker)
	dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker)
	if _, err := hooks.CloseClaimEpisodesForSession(dir, dgCaller, claimledger.OutcomeAbandoned); err != nil {
		t.Fatal(err)
	}
	if dgEpisodeOpen(t, dir, dgCaller) || !dgEpisodeOpen(t, dir, dgWorker) {
		t.Fatal("the orchestrator's end must close only its own episode")
	}
}

func TestClear_ReopensEpisodeOnNextBrief(t *testing.T) {
	dir := newDelegateClaimDir(t)
	dgOpenEpisode(t, dir, dgCaller)
	run := dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker)
	dgOpenEpisode(t, dir, dgWorker)
	// /clear: the old session's SessionEnd closes its rows; the new session
	// is not yet in worker_sessions.
	if _, err := hooks.CloseClaimEpisodesForSession(dir, dgWorker, claimledger.OutcomeAbandoned); err != nil {
		t.Fatal(err)
	}
	if err := checkLiveCollisionGate(dir, dgItem, dgClear); err == nil {
		t.Fatal("without the run env the new session must be refused")
	}
	t.Setenv(delegateRunEnv, run)
	if err := checkLiveCollisionGate(dir, dgItem, dgClear); err != nil {
		t.Fatalf("the cleared worker could not reclaim: %v", err)
	}
	dgOpenEpisode(t, dir, dgClear)
	// Other side: an old worker session whose episode never closed is a
	// sibling of the new one, not a collision.
	dgOpenEpisode(t, dir, dgWorker)
	if got := dgForeign(t, dir, dgClear); len(got) != 0 {
		t.Fatalf("foreign %v", got)
	}
}

func TestComplete_WarnsOnLiveDelegation(t *testing.T) {
	dir := newDelegateClaimDir(t)
	var buf bytes.Buffer
	warnLiveDelegation(&buf, dir, dgItem)
	dgOpenRow(t, dir, delegationledger.ModeOneshot, dgCaller)
	warnLiveDelegation(&buf, dir, dgItem)
	if buf.Len() != 0 {
		t.Fatalf("no interactive row, yet warned: %q", buf.String())
	}
	run := dgOpenRow(t, dir, delegationledger.ModeInteractive, dgCaller, dgWorker)
	warnLiveDelegation(&buf, dir, "feat-0000dlg2")
	if buf.Len() != 0 {
		t.Fatalf("a row on another item warned: %q", buf.String())
	}
	warnLiveDelegation(&buf, dir, dgItem)
	if out := buf.String(); !strings.Contains(out, run) || !strings.Contains(out, "delegate stop") {
		t.Fatalf("warning %q must name the run and how to stop it", out)
	}
	buf.Reset()
	if err := delegationledger.NewStore(dir).Close(run, delegationledger.CloseInfo{Ended: time.Now().UTC(), Reason: delegationledger.EndStop}); err != nil {
		t.Fatal(err)
	}
	warnLiveDelegation(&buf, dir, dgItem)
	if buf.Len() != 0 {
		t.Fatalf("an ended row warned: %q", buf.String())
	}
}

// TestComplete_WarnsOnLiveDelegation_FeatureComplete drives the real
// completion path: `feature complete` prints the warning and still completes.
func TestComplete_WarnsOnLiveDelegation_FeatureComplete(t *testing.T) {
	dir := newDelegateClaimDir(t)
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	projectDirFlag = filepath.Dir(dir)
	t.Cleanup(func() { projectDirFlag = "" })
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "none")
	var errBuf bytes.Buffer
	origStderr := stderr
	stderr = &errBuf
	t.Cleanup(func() { stderr = origStderr })
	wiAcceptedAdvisory, wiAllowDirtyComplete = "unit test", true
	t.Cleanup(func() { wiAcceptedAdvisory, wiAllowDirtyComplete = "", false })

	complete := func(title string, live bool) string {
		t.Helper()
		if err := testCreateStandalone("feature", title); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join(dir, "features", "feat-*.html"))
		node, err := parseNodeFile(newestFile(t, files))
		if err != nil {
			t.Fatal(err)
		}
		if err := wiSetStatusWithAgent("feature", node.ID, "in-progress", dgCaller, ""); err != nil {
			t.Fatal(err)
		}
		if live {
			if _, err := delegationledger.NewStore(dir).Open(delegationledger.Record{Mode: delegationledger.ModeInteractive,
				Harness: "claude", Name: "w1", WorkItem: node.ID, CallerSession: dgCaller, Started: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
		}
		errBuf.Reset()
		if err := wiSetStatusWithAgent("feature", node.ID, "done", dgCaller, ""); err != nil {
			t.Fatalf("complete: %v", err)
		}
		return errBuf.String()
	}
	if out := complete("No delegation", false); strings.Contains(out, "delegate worker") {
		t.Fatalf("warned without a live delegation: %q", out)
	}
	if out := complete("Live delegation", true); !strings.Contains(out, "live delegate worker") || !strings.Contains(out, "delegate stop w1") {
		t.Fatalf("no warning on complete: %q", out)
	}
}

func newestFile(t *testing.T, files []string) string {
	t.Helper()
	var best string
	var bestMod time.Time
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && !st.ModTime().Before(bestMod) {
			best, bestMod = f, st.ModTime()
		}
	}
	if best == "" {
		t.Fatal("no feature file")
	}
	return best
}

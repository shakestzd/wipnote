package claimledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

const claimRun = "dlg-0123456789ab"

// claimProject is a temp .wipnote with a run folder and one host shard
// holding the host orchestrator's open episode on feat-x.
func claimProject(t *testing.T) (wipnoteDir, runDir string, hostShard []byte) {
	t.Helper()
	wipnoteDir = filepath.Join(t.TempDir(), ".wipnote")
	runDir = filepath.Join(wipnoteDir, "delegate", "runs", claimRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	t.Setenv(containerwrite.RunEnv, claimRun)
	host := NewStore(wipnoteDir)
	mustOpen(t, host, "host-root", "host-root", "claude", "feat-x", time.Now().UTC().Add(-time.Hour))
	data, err := os.ReadFile(host.ShardPath("host-root"))
	if err != nil {
		t.Fatal(err)
	}
	return wipnoteDir, runDir, data
}

// TestContainerWorker_ClaimsWriteOnlyOwnFiles: inside a container worker
// every claim write (open, close, close-all, close-work-item) goes to the
// worker's own files in its run folder; no shared shard is written, not even
// the host's shard that a completion would close. The worker's own reads see
// its episodes and the host's. A completion that would close a host shard
// leaves an op for the host fold.
func TestContainerWorker_ClaimsWriteOnlyOwnFiles(t *testing.T) {
	wipnoteDir, runDir, hostBefore := claimProject(t)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	w := NewStore(wipnoteDir)
	now := time.Now().UTC()
	mustOpen(t, w, "w-root", "w-root", "claude", "feat-x", now)
	mustOpen(t, w, "w-root", "w-root", "claude", "feat-y", now)
	if _, err := w.Close("w-root", "w-root", "claude", "feat-y", OutcomeReleased, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CloseAllForSession("w-root", "w-root", OutcomeAbandoned, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	mustOpen(t, w, "w-root", "w-root", "claude", "feat-x", now.Add(3*time.Minute))
	n, err := w.CloseWorkItem("feat-x", OutcomeCompleted, now.Add(4*time.Minute), now.Add(4*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("CloseWorkItem = %d, %v; want the worker's own one closed here", n, err)
	}
	shared := filepath.Join(wipnoteDir, DirName)
	if after, _ := os.ReadFile(filepath.Join(shared, "sess-host-root.html")); string(after) != string(hostBefore) {
		t.Error("the container worker rewrote the host's shard")
	}
	if _, err := os.Stat(filepath.Join(shared, "sess-w-root.html")); !os.IsNotExist(err) {
		t.Errorf("the container worker wrote a shared shard (err %v)", err)
	}
	own, _ := ReadFile(filepath.Join(runDir, DirName, "sess-w-root.html"))
	if len(own) != 3 {
		t.Errorf("own shard holds %d episodes, want 3", len(own))
	}
	ops, _ := ReadPendingCloses(runDir)
	if len(ops) != 1 || ops[0].WorkItemID != "feat-x" || ops[0].Outcome != OutcomeCompleted {
		t.Errorf("pending closes %+v, want one for feat-x", ops)
	}
	all, _ := w.ReadAll()
	var mine, hosts int
	for _, e := range all {
		switch e.RootSessionID {
		case "w-root":
			mine++
		case "host-root":
			hosts++
		}
	}
	if mine != 3 || hosts != 1 {
		t.Errorf("worker's ReadAll: %d own, %d host episodes; want 3 and 1", mine, hosts)
	}
	if _, err := w.Reconcile(func(string) bool { return false }, now); !errors.Is(err, ErrContainerWorker) {
		t.Errorf("Reconcile inside a worker: %v, want ErrContainerWorker", err)
	}
}

// TestHost_ClaimsStillWriteSharedShards is the other side: on the host the
// same writes go to the shared shards and leave no own files.
func TestHost_ClaimsStillWriteSharedShards(t *testing.T) {
	wipnoteDir, runDir, _ := claimProject(t)
	s := NewStore(wipnoteDir)
	mustOpen(t, s, "h2", "h2", "claude", "feat-x", time.Now().UTC())
	if n, err := s.CloseWorkItem("feat-x", OutcomeCompleted, time.Now().UTC(), time.Time{}); err != nil || n != 2 {
		t.Fatalf("CloseWorkItem = %d, %v; want both shared episodes", n, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, DirName)); !os.IsNotExist(err) {
		t.Errorf("the host wrote own claim files (err %v)", err)
	}
}

// TestMergeOwnShard_AppliesOnceUnderTheShardLock: the host merge adds the
// worker's new episodes and ends to the shared shard, keeps an episode the
// host appended in between, and is a no-op the second time.
func TestMergeOwnShard_AppliesOnceUnderTheShardLock(t *testing.T) {
	wipnoteDir, runDir, _ := claimProject(t)
	now := time.Now().UTC()
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	w := NewStore(wipnoteDir)
	mustOpen(t, w, "w-root", "w-root", "claude", "feat-x", now)
	if _, err := w.Close("w-root", "w-root", "claude", "feat-x", OutcomeReleased, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	host := NewStore(wipnoteDir)
	mustOpen(t, host, "w-root", "w-root", "codex", "feat-z", now) // the host wrote this root's shard too
	own, _ := ReadFile(filepath.Join(runDir, DirName, "sess-w-root.html"))
	if n, err := host.MergeOwnShard("sess-w-root.html", own); err != nil || n != 1 {
		t.Fatalf("merge = %d, %v; want one episode added", n, err)
	}
	got, _ := host.ReadShard("w-root")
	if len(got) != 2 {
		t.Fatalf("shared shard %+v: want the host's and the worker's episode", got)
	}
	before, _ := os.ReadFile(host.ShardPath("w-root"))
	if n, _ := host.MergeOwnShard("sess-w-root.html", own); n != 0 {
		t.Errorf("second merge changed %d", n)
	}
	if after, _ := os.ReadFile(host.ShardPath("w-root")); string(after) != string(before) {
		t.Error("second merge rewrote the shard")
	}
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if _, err := host.MergeOwnShard("sess-w-root.html", own); !errors.Is(err, ErrContainerWorker) {
		t.Errorf("merge inside a worker: %v, want ErrContainerWorker", err)
	}
}

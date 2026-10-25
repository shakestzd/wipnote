package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/fsprobe"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const endTestSession = "aaaabbbb-cccc-dddd-eeee-ffff00002222"

// endFixture is a temp project with an open ledger row and open claim episodes
// for endTestSession, plus filler claim shards owned by other sessions.
type endFixture struct {
	projectDir string
	wipnoteDir string
	sessionID  string
}

// unsetenv removes key for the test and restores it afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}

func newEndFixture(t *testing.T, fillerShards int) *endFixture {
	t.Helper()
	unsetenv(t, "WIPNOTE_PROJECT_DIR")
	unsetenv(t, "CLAUDE_PROJECT_DIR")
	unsetenv(t, "WIPNOTE_PARENT_SESSION")
	unsetenv(t, "WIPNOTE_OTEL_SESSION_ID")
	t.Setenv("WIPNOTE_SESSION_ID", endTestSession)
	t.Setenv("WIPNOTE_HARNESS", "claude")
	prev := CollectorAliveFn
	CollectorAliveFn = nil
	t.Cleanup(func() { CollectorAliveFn = prev })

	f := &endFixture{projectDir: t.TempDir(), sessionID: endTestSession}
	f.wipnoteDir = filepath.Join(f.projectDir, ".wipnote")
	if err := os.MkdirAll(f.wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionledger.NewStore(f.wipnoteDir).Open(sessionledger.Record{
		SessionID: f.sessionID, Harness: "claude_code", ProjectDir: ".",
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	f.openEpisode(t, f.sessionID, f.sessionID, "feat-aaaa0001")
	f.openEpisode(t, f.sessionID, f.sessionID, "feat-aaaa0002")
	for i := 0; i < fillerShards; i++ {
		other := fmt.Sprintf("bbbb%04d-cccc-dddd-eeee-ffff00003333", i)
		f.openEpisode(t, other, other, fmt.Sprintf("feat-bbbb%04d", i))
	}
	return f
}

func (f *endFixture) openEpisode(t *testing.T, root, session, item string) {
	t.Helper()
	if _, _, err := claimledger.NewStore(f.wipnoteDir).Open(root, claimledger.Episode{
		WorkItemID: item, SessionID: session, RootSessionID: root, AgentID: "claude-code",
		StartedAt: time.Now().UTC().Add(-50 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *endFixture) notePath() string {
	return filepath.Join(f.wipnoteDir, "sessions", f.sessionID, SessionEndNoteFile)
}

// writeNote plants an end note as a cancelled hook would have left it.
func (f *endFixture) writeNote(t *testing.T, endedAt time.Time, outcome string) {
	t.Helper()
	dir := filepath.Dir(f.notePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"session_id":%q,"ended_at":%q,"reason":"prompt_input_exit","outcome":%q}`+"\n",
		f.sessionID, endedAt.UTC().Format(time.RFC3339Nano), outcome)
	if err := os.WriteFile(f.notePath(), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *endFixture) episodes(t *testing.T, root string) []claimledger.Episode {
	t.Helper()
	eps, err := claimledger.NewStore(f.wipnoteDir).ReadShard(root)
	if err != nil {
		t.Fatalf("read shard %s: %v", root, err)
	}
	return eps
}

func (f *endFixture) ledgerRow(t *testing.T) sessionledger.Record {
	t.Helper()
	rec, found, err := sessionledger.NewStore(f.wipnoteDir).Get(f.sessionID)
	if err != nil || !found {
		t.Fatalf("ledger row: found=%v err=%v", found, err)
	}
	return rec
}

// stubGit puts a fake git first on PATH. Every call is logged with whether the
// end note already existed, then sleeps for delay.
func stubGit(t *testing.T, f *endFixture, delay time.Duration) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(bin, "git.log")
	script := fmt.Sprintf(`#!/bin/sh
if [ -s %q ]; then s=present; else s=absent; fi
echo "$s $*" >> %q
sleep %.3f
echo abc1234
`, f.notePath(), logPath, delay.Seconds())
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func gitCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// probeLog models a slow filesystem and records what ran before the note.
type probeLog struct {
	mu             sync.Mutex
	scanBeforeNote []string
	claimScans     int
	ops            int
}

// installSlowFS adds delay to every probed operation and records any scan of the
// claims directory (a ReadAll) and whether it happened before the note existed.
func installSlowFS(t *testing.T, f *endFixture, delay time.Duration) *probeLog {
	t.Helper()
	p := &probeLog{}
	claimsDir := claimledger.NewStore(f.wipnoteDir).Dir()
	fsprobe.Set(func(op fsprobe.Op, path string) {
		time.Sleep(delay)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.ops++
		if op == fsprobe.OpRead && path == claimsDir {
			p.claimScans++
			if fi, err := os.Stat(f.notePath()); err != nil || fi.Size() == 0 {
				p.scanBeforeNote = append(p.scanBeforeNote, path)
			}
		}
	})
	t.Cleanup(func() { fsprobe.Set(nil) })
	return p
}

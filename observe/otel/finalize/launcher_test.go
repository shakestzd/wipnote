package finalize

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// seedLauncherShard writes a launcher-keyed shard whose signals carry the
// harness (Claude) session ids, plus one HTML shell per harness id.
func seedLauncherShard(t *testing.T, projectDir, shard string, harnessIDs ...string) string {
	t.Helper()
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(filepath.Join(sessionsDir, shard), 0o755); err != nil {
		t.Fatal(err)
	}
	var body string
	for _, id := range harnessIDs {
		html := `<html><body><article id="` + id + `"><h1>s</h1></article></body></html>`
		if err := os.WriteFile(filepath.Join(sessionsDir, id+".html"), []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
		body += apiRequestLine(id, id+"-a", "0.25") + "\n" + apiRequestLine(id, id+"-b", "0.5") + "\n"
	}
	path := filepath.Join(sessionsDir, shard, "events.ndjson")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The shard directory is named for the launcher id, the signals for the
// harness ids. Finalize must roll up each harness id, not the directory name.
func TestSession_LauncherShardRollsUpHarnessSessions(t *testing.T) {
	projectDir := t.TempDir()
	path := seedLauncherShard(t, projectDir, "01alauncher", "claude-one", "claude-two")

	res, err := Session(context.Background(), projectDir, "01alauncher")
	if err != nil || !res.Complete {
		t.Fatalf("Session = %+v, %v; want complete", res, err)
	}
	for _, id := range []string{"claude-one", "claude-two"} {
		if html := readHTML(t, projectDir, id); !strings.Contains(html, `data-total-cost-usd="0.750000"`) {
			t.Fatalf("%s: rollup missing collector cost:\n%s", id, html)
		}
	}
	info, _ := os.Stat(path)
	if got, ok := readMarker(t, path); !ok || got != info.Size() {
		t.Fatalf("marker = %d (present=%v), want %d", got, ok, info.Size())
	}
}

// Catch-up reaches a launcher shard and rolls up its harness sessions too.
func TestCatchUp_LauncherShardRollsUpHarnessSession(t *testing.T) {
	projectDir := t.TempDir()
	path := seedLauncherShard(t, projectDir, "01alauncher", "claude-one")
	ageFile(t, path, time.Hour)

	if res := CatchUp(context.Background(), projectDir, "claude-one"); res.Finalized != 1 {
		t.Fatalf("CatchUp = %+v, want 1 finalized", res)
	}
	if html := readHTML(t, projectDir, "claude-one"); !strings.Contains(html, `data-total-cost-usd="0.750000"`) {
		t.Fatalf("rollup missing collector cost:\n%s", html)
	}
}

func TestShardsFor(t *testing.T) {
	projectDir := t.TempDir()
	seedLauncherShard(t, projectDir, "01alauncher", "claude-one")
	seedSession(t, projectDir, "plain")

	cases := []struct {
		name, id, launcher string
		want               []string
	}{
		{"launcher env wins", "claude-one", "01alauncher", []string{"01alauncher"}},
		{"launcher plus own dir", "plain", "01alauncher", []string{"01alauncher", "plain"}},
		{"no env, own dir", "plain", "", []string{"plain"}},
		{"launcher missing on disk", "claude-one", "01agone", nil},
		{"unsafe launcher id", "claude-one", "../x", nil},
		{"same id once", "plain", "plain", []string{"plain"}},
	}
	for _, c := range cases {
		if got := ShardsFor(projectDir, c.id, c.launcher); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ShardsFor = %v, want %v", c.name, got, c.want)
		}
	}
}

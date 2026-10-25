package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// TestSessionHTML_ConcurrentAppendAndSetActiveFeature: appends must survive
// concurrent SetSessionHTMLActiveFeature and same-session CreateSessionHTML
// replays. Without a shared flock those read-modify-write cycles lose events.
func TestSessionHTML_ConcurrentAppendAndSetActiveFeature(t *testing.T) {
	projectDir := t.TempDir()
	const sid = "sess-concurrent-html"
	sess := &models.Session{SessionID: sid, AgentAssigned: "claude-code", Status: "active", CreatedAt: time.Now().UTC()}
	CreateSessionHTML(projectDir, sess)

	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			AppendEventToSessionHTML(projectDir, sid, SessionEvent{Timestamp: time.Now(), ToolName: "Bash", Success: true, EventID: "e"})
		}()
		go func() {
			defer wg.Done()
			_ = SetSessionHTMLActiveFeature(projectDir, sid, "feat-12345678")
		}()
		go func() {
			defer wg.Done()
			CreateSessionHTML(projectDir, sess)
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sid+".html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), `data-tool="Bash"`); got != n {
		t.Fatalf("events in session HTML = %d, want %d (lost updates)", got, n)
	}
}

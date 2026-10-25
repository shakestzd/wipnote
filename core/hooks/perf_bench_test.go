package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// Benchmarks for the per-event hook paths that grow with session length
// (feat-c30d0792). Run with:
//
//	go test ./hooks/ -run '^$' -bench 'SessionHTML|StopBackfill' -benchtime 200x

// writeSessionHTMLFixture creates a session HTML shell for sessionID and pads
// its activity log with <li> entries until the file is at least size bytes.
func writeSessionHTMLFixture(tb testing.TB, projectDir, sessionID string, size int) string {
	tb.Helper()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		tb.Fatal(err)
	}
	CreateSessionHTML(projectDir, &models.Session{
		SessionID: sessionID, AgentAssigned: "claude-code", Status: "active",
		CreatedAt: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	})
	path := filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html")
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	content := string(data)
	idx := strings.LastIndex(content, "</ol>")
	if idx < 0 {
		tb.Fatal("fixture has no </ol>")
	}
	var pad strings.Builder
	for i := 0; len(content)+pad.Len() < size; i++ {
		fmt.Fprintf(&pad, "                <li data-ts=\"2026-10-25T12:00:00Z\" data-tool=\"Bash\" "+
			"data-success=\"true\" data-event-id=\"evt-%06d\">Bash: go test ./... (padding entry)</li>\n", i)
	}
	content = content[:idx] + pad.String() + "            " + content[idx:]
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
	return path
}

func benchSessionEvent(i int) SessionEvent {
	return SessionEvent{
		Timestamp: time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC),
		ToolName:  "Edit", Success: true, EventID: fmt.Sprintf("evt-bench-%d", i),
		FeatureID: "feat-c30d0792", Summary: "Edit: core/hooks/session_html.go",
	}
}

func benchAppendSessionHTML(b *testing.B, size int) {
	projectDir := b.TempDir()
	path := writeSessionHTMLFixture(b, projectDir, "bench-sess", size)
	orig, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%100 == 0 {
			b.StopTimer()
			if err := os.WriteFile(path, orig, 0o644); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
		}
		AppendEventToSessionHTML(projectDir, "bench-sess", benchSessionEvent(i))
	}
}

func BenchmarkAppendEventToSessionHTML_255KB(b *testing.B) { benchAppendSessionHTML(b, 255*1024) }
func BenchmarkAppendEventToSessionHTML_2MB(b *testing.B)   { benchAppendSessionHTML(b, 2*1024*1024) }

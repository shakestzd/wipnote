package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// largeTranscript writes a transcript of about sizeMB: ordinary 2KB lines with
// a task notification every notifyEvery lines, all naming one 256KB output file.
func largeTranscript(tb testing.TB, dir string, sizeMB, notifyEvery int) string {
	tb.Helper()
	path := filepath.Join(dir, "large.jsonl")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	filler := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 2000) + `"}}` + "\n"
	out := filepath.Join(dir, "task.output")
	os.WriteFile(out, []byte(strings.Repeat("ok  pkg 0.1s\n", 20000)), 0o644)
	for i, written := 0, 0; written < sizeMB<<20; i++ {
		line := filler
		if i%notifyEvery == 0 {
			id := fmt.Sprintf("b%06d", i)
			line = notificationLine(id, "toolu_"+id, out, "completed", `Background command "gate" completed (exit code 0)`)
		}
		n, _ := f.WriteString(line)
		written += n
	}
	return path
}

// BenchmarkSyncTranscriptRuns_FirstRead measures one first pass (no saved
// offset) over a 10MB transcript, at the Stop and prompt limits; the dense
// case has a notification on every line, the prompt path's worst case.
func BenchmarkSyncTranscriptRuns_FirstRead(b *testing.B) {
	for _, c := range []struct {
		name  string
		pass  runPass
		every int
	}{{"stop-pass", runStopPass, 100}, {"prompt-pass", runPromptPass, 100}, {"prompt-pass-dense", runPromptPass, 1}} {
		b.Run(c.name, func(b *testing.B) {
			dir := b.TempDir()
			projectDir := filepath.Join(dir, "proj")
			os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755)
			transcript := largeTranscript(b, dir, 10, c.every)
			offset := filepath.Join(projectDir, ".wipnote", "sessions", runsSession, runOffsetFile)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				os.Remove(offset)
				syncTranscriptRuns(projectDir, runsSession, transcript, c.pass)
			}
		})
	}
}

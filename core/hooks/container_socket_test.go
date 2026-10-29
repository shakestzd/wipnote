package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// TestContainerWorker_EventsDoNotNeedWriterSock (bug-6715affb): through
// Docker Desktop's bind mount .wipnote/writer.sock is not a usable socket.
// A container worker's prompt hook must still write its events file, must
// not wait on the socket, and must leave the socket path as it found it.
func TestContainerWorker_EventsDoNotNeedWriterSock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, sock string)
	}{
		{"absent", func(*testing.T, string) {}},
		{"regular file", func(t *testing.T, sock string) {
			if err := os.WriteFile(sock, []byte("not a socket"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project, runDir := workerProject(t)
			t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
			t.Setenv(DaemonSocketEnv, "") // a worker is not launcher-guaranteed
			sock := filepath.Join(project, ".wipnote", "writer.sock")
			tc.setup(t, sock)
			before, statErr := os.Stat(sock)

			sid := "bbbbbbbb-1111-2222-3333-444444444444"
			start := time.Now()
			ev := &CloudEvent{SessionID: sid, CWD: project, HookEventName: "UserPromptSubmit", Prompt: "hi"}
			if _, err := UserPrompt(ev); err != nil {
				t.Fatalf("UserPrompt: %v", err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("hook took %v, want it never to wait on a socket or daemon", d)
			}
			fi, err := os.Stat(filepath.Join(runDir, "events-"+sid+".ndjson"))
			if err != nil || fi.Size() == 0 {
				t.Fatalf("events file missing or empty: %v", err)
			}
			after, afterErr := os.Stat(sock)
			if (statErr == nil) != (afterErr == nil) || (statErr == nil && before.Size() != after.Size()) {
				t.Errorf("hook changed writer.sock: before %v/%v after %v/%v", before, statErr, after, afterErr)
			}
		})
	}
}

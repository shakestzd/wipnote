package gitwatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture returns the contents of testdata/<name>.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestExecRunner_NotFoundIsNotAvailable(t *testing.T) {
	r := ExecRunner{}
	_, _, _, err := r.Run(context.Background(), "wipnote-gitwatch-no-such-binary")
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("missing executable: want ErrNotAvailable, got %v", err)
	}
}

func TestExecRunner_NonZeroExitKeepsStdout(t *testing.T) {
	r := ExecRunner{}
	out, errOut, code, err := r.Run(context.Background(), "sh", "-c", "echo out; echo bad >&2; exit 8")
	if err != nil {
		t.Fatalf("non-zero exit must not be an error, got %v", err)
	}
	if code != 8 {
		t.Errorf("exit code = %d, want 8", code)
	}
	if string(out) != "out\n" || string(errOut) != "bad\n" {
		t.Errorf("stdout=%q stderr=%q", out, errOut)
	}
	_, _, code, err = r.Run(context.Background(), "sh", "-c", "exit 0")
	if err != nil || code != 0 {
		t.Errorf("clean exit: code=%d err=%v", code, err)
	}
}

func TestExecRunner_Timeout(t *testing.T) {
	r := ExecRunner{Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, _, _, err := r.Run(context.Background(), "sleep", "5")
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if errors.Is(err, ErrNotAvailable) {
		t.Errorf("timeout must not read as not-available: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout not honoured: took %s", time.Since(start))
	}
}

func TestFakeRunner_RecordsAndRejectsUnknown(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr view --json number": {Stdout: `{"number":1}`, Exit: 0},
	}}
	out, _, _, err := f.Run(context.Background(), "gh", "pr", "view", "--json", "number")
	if err != nil || string(out) != `{"number":1}` {
		t.Fatalf("known argv: out=%q err=%v", out, err)
	}
	_, _, _, err = f.Run(context.Background(), "gh", "api", "user")
	if err == nil || !strings.Contains(err.Error(), "gh api user") {
		t.Fatalf("unknown argv must error naming it, got %v", err)
	}
	if len(f.Calls) != 2 || f.Calls[1] != "gh api user" {
		t.Errorf("calls = %q", f.Calls)
	}
}

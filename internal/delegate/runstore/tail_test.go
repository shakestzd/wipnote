package runstore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func tailFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// orch F-4: a bounded tail read whose window begins exactly on a line
// boundary dropped that whole first line. The window "bbb\nccc\n" starts
// right after a newline, so "bbb" is a whole line and is kept.
func TestTail_KeepsLineAtWindowStart(t *testing.T) {
	path := tailFile(t, "aaa\nbbb\nccc\n")
	got, err := runstore.ReadTail(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bbb\nccc\n" {
		t.Errorf("ReadTail = %q, want %q", got, "bbb\nccc\n")
	}
}

// The other side: a window that begins inside a line drops only that
// partial first line, never the whole line after it.
func TestTail_DropsOnlyPartialFirstLine(t *testing.T) {
	path := tailFile(t, "aaa\nbbb\nccc\n")
	for max, want := range map[int64]string{
		9:   "bbb\nccc\n", // window "\nbbb\nccc\n": starts on the newline itself
		7:   "ccc\n",      // window "bb\nccc\n": "bb" is cut
		4:   "ccc\n",      // exactly the last line
		3:   "",           // "cc\n" is all partial
		100: "aaa\nbbb\nccc\n",
	} {
		got, err := runstore.ReadTail(path, max)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("runstore.ReadTail(max %d) = %q, want %q", max, got, want)
		}
	}
	if _, err := runstore.ReadTail(filepath.Join(t.TempDir(), "absent"), 10); !os.IsNotExist(err) {
		t.Errorf("absent file: err = %v", err)
	}
}

// A run folder's files read from an offset (the viewer's follow) and by tail.
func TestRunFolder_ReadFromAndTail(t *testing.T) {
	s, err := runstore.New(newProject(t))
	if err != nil {
		t.Fatal(err)
	}
	rf, err := s.CreateRun(testRun)
	if err != nil {
		t.Fatal(err)
	}
	if err := rf.WriteFile(runstore.FileCleanLog, []byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	data, size, err := rf.ReadFrom(runstore.FileCleanLog, 4)
	if err != nil || string(data) != "two\n" || size != 8 {
		t.Errorf("ReadFrom = %q, %d, %v", data, size, err)
	}
	if data, size, err := rf.ReadFrom(runstore.FileCleanLog, 20); err != nil || len(data) != 0 || size != 8 {
		t.Errorf("ReadFrom past the end = %q, %d, %v", data, size, err)
	}
	if tail, err := rf.Tail(runstore.FileCleanLog, 4); err != nil || string(tail) != "two\n" {
		t.Errorf("Tail = %q, %v", tail, err)
	}
	if _, _, err := rf.ReadFrom("other", 0); err == nil {
		t.Error("a name outside the contract should be refused")
	}
	link := filepath.Join(rf.Dir(), runstore.FileLastMD)
	if err := os.Symlink(filepath.Join(rf.Dir(), runstore.FileCleanLog), link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rf.ReadFrom(runstore.FileLastMD, 0); err == nil {
		t.Error("a symlinked run-folder file should be refused")
	}
}

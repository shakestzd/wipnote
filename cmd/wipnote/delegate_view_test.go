package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// delegateTestProject is a temp project with no environment pointing a
// store elsewhere.
func delegateTestProject(t *testing.T) string {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "HERDR_") || name == "WIPNOTE_PROJECT_DIR" || name == "CLAUDE_PROJECT_DIR" {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// wipnote delegate view <run-id> prints a finished run from the project's
// run store and returns: its log, status and answer.
func TestDelegateView_PrintsFinishedRun(t *testing.T) {
	dir := delegateTestProject(t)
	store, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rf, err := store.CreateRun("dlg-00112233445566")
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		runstore.FileLabel:   "cdx: list",
		runstore.FileCleanMD: "### Assistant\n\nhello\n",
		runstore.FileLastMD:  "the answer\n",
	} {
		if err := rf.WriteFile(name, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := rf.Finish(0, time.Now()); err != nil {
		t.Fatal(err)
	}
	projectDirFlag = dir
	defer func() { projectDirFlag = "" }()
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"view", "dlg-00112233445566"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cdx: list", "hello", "finished in", "--- answer ---\nthe answer"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	cmd = delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"view", "dlg-ffffffffffffff"})
	if err := cmd.Execute(); err == nil {
		t.Error("an unknown run should fail")
	}
}

func TestDelegateView_Registered(t *testing.T) {
	for _, sub := range buildRoot().Commands() {
		if sub.Name() == "delegate" {
			for _, c := range sub.Commands() {
				if c.Name() == "view" {
					return
				}
			}
		}
	}
	t.Error("wipnote delegate view is not registered")
}

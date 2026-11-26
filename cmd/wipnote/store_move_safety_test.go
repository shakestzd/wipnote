package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

func featureRecord(id, extra string) string {
	return "<article id=\"" + id + "\"" + extra + "></article>\n"
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// rescuedFiles maps each file under the store's rescue folder, by its path
// below the per-checkout folder's store, to its content.
func rescuedFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	_ = filepath.WalkDir(filepath.Join(root, "rescued"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		got[filepath.Join(filepath.Base(filepath.Dir(path)), d.Name())] = mustRead(t, path)
		return nil
	})
	return got
}

// A submodule is another repository. A launch inside it moves the project's
// store and leaves the submodule's own store alone (PR 390 finding 1).
func TestAutoMoveStore_SubmoduleIsLeftAlone(t *testing.T) {
	p := inRepoStoreProject(t)
	extGit(t, p.code, "stash", "-q")
	lib := filepath.Join(p.base, "lib")
	if err := os.MkdirAll(filepath.Join(lib, ".wipnote", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	extGit(t, lib, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(lib, ".wipnote", "features", "feat-dddddddd.html"), []byte(featureRecord("feat-dddddddd", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	extGit(t, lib, "add", "-f", ".")
	extGit(t, lib, "commit", "-q", "-m", "lib store")
	extGit(t, p.code, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	extGit(t, p.code, "commit", "-q", "-m", "add submodule")
	sub := filepath.Join(p.code, "vendor", "lib")
	head := extGit(t, sub, "rev-parse", "HEAD")
	t.Chdir(sub)

	var out bytes.Buffer
	autoMoveStore(&out, true)
	if !paths.IsMovedStore(paths.ShadowRootFor(p.code)) {
		t.Fatalf("the project's store was not moved:\n%s", out.String())
	}
	if now := extGit(t, sub, "rev-parse", "HEAD"); now != head {
		t.Errorf("the launch committed in the submodule: %s -> %s\n%s", head, now, out.String())
	}
	if status := extGit(t, sub, "status", "--porcelain"); status != "" {
		t.Errorf("the launch changed the submodule's tree or index:\n%s", status)
	}
	if _, err := os.Stat(filepath.Join(sub, ".wipnote", "features", "feat-dddddddd.html")); err != nil {
		t.Errorf("the submodule's store is gone: %v", err)
	}
	if got := out.String(); !strings.Contains(got, filepath.Join(sub, ".wipnote")) || !strings.Contains(got, "did not move it") {
		t.Errorf("no notice names the submodule's store:\n%s", got)
	}
}

// A record tracked only on another branch and a record written after the move
// both reach the app store before a later launch deletes the folder (finding 2).
func TestAutoMoveStore_LaterLaunchCopiesBeforeDelete(t *testing.T) {
	p := inRepoStoreProject(t)
	extGit(t, p.code, "stash", "-q")
	extGit(t, p.code, "checkout", "-q", "-b", "side")
	p.write(t, ".wipnote/features/feat-bbbbbbbb.html", featureRecord("feat-bbbbbbbb", ""))
	extGit(t, p.code, "add", "-f", ".wipnote")
	extGit(t, p.code, "commit", "-q", "-m", "side item")
	extGit(t, p.code, "checkout", "-q", "main")
	var out bytes.Buffer
	autoMoveStore(&out, true)
	root := paths.ShadowRootFor(p.code)

	extGit(t, p.code, "checkout", "-q", "side")
	p.write(t, ".wipnote/features/feat-cccccccc.html", featureRecord("feat-cccccccc", ""))
	out.Reset()
	autoMoveStore(&out, true)
	for _, id := range []string{"feat-bbbbbbbb", "feat-cccccccc"} {
		if got, err := os.ReadFile(filepath.Join(root, ".wipnote", "features", id+".html")); err != nil || string(got) != featureRecord(id, "") {
			t.Errorf("%s is not in the app store: %v %q", id, err, got)
		}
	}
	if !strings.Contains(out.String(), "Added 2 file(s)") {
		t.Errorf("the launch did not say how many files it added:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote")); err == nil {
		t.Errorf("the old folder is still there:\n%s", out.String())
	}
	if status := extGit(t, root, "status", "--porcelain", "--", ".wipnote/features"); status != "" {
		t.Errorf("the added records are not committed in the app store:\n%s", status)
	}
}

// A record that differs from the app store's copy and that git does not keep
// goes to the rescue folder. The app store's copy always wins.
func TestAutoMoveStore_ConflictingRecordsAreRescued(t *testing.T) {
	p := inRepoStoreProject(t)
	var out bytes.Buffer
	autoMoveStore(&out, false) // copy only: the folder and the index stay
	root := paths.ShadowRootFor(p.code)
	features := filepath.Join(root, ".wipnote", "features")
	moved := mustRead(t, filepath.Join(features, "feat-11111111.html"))

	// Modified against HEAD, and no longer what the app store holds.
	p.write(t, ".wipnote/features/feat-11111111.html", featureRecord("feat-11111111", " data-status=\"late\""))
	// Untracked, with an id the app store already has under other content.
	appCopy := featureRecord("feat-99999999", " data-from=\"app\"")
	if err := os.WriteFile(filepath.Join(features, "feat-99999999.html"), []byte(appCopy), 0o644); err != nil {
		t.Fatal(err)
	}
	p.write(t, ".wipnote/features/feat-99999999.html", featureRecord("feat-99999999", " data-from=\"project\""))
	// Tracked and unmodified, but changed in the app store: git history keeps it.
	if err := os.WriteFile(filepath.Join(root, ".wipnote", "config.json"), []byte("{\"app\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	autoMoveStore(&out, true)
	want := map[string]string{
		filepath.Join("features", "feat-11111111.html"): featureRecord("feat-11111111", " data-status=\"late\""),
		filepath.Join("features", "feat-99999999.html"): featureRecord("feat-99999999", " data-from=\"project\""),
	}
	got := rescuedFiles(t, root)
	if len(got) != len(want) {
		t.Fatalf("rescued %v, want exactly %d file(s)\n%s", got, len(want), out.String())
	}
	for rel, body := range want {
		if got[rel] != body {
			t.Errorf("rescued %s = %q, want %q", rel, got[rel], body)
		}
	}
	kept := map[string]string{"features/feat-11111111.html": moved, "features/feat-99999999.html": appCopy, "config.json": "{\"app\":true}\n"}
	for rel, body := range kept {
		if now := mustRead(t, filepath.Join(root, ".wipnote", rel)); now != body {
			t.Errorf("the app store's %s was overwritten: %q", rel, now)
		}
	}
	if !strings.Contains(out.String(), filepath.Join(root, "rescued")) || !strings.Contains(out.String(), "2 file(s)") {
		t.Errorf("the launch did not name the rescue folder and the count:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote")); err == nil {
		t.Errorf("the old folder is still there:\n%s", out.String())
	}
}

// A linked worktree's folder is not the one a move copies, so its extra
// record is copied before the folder goes.
func TestAutoMoveStore_LinkedWorktreeRecordIsCopied(t *testing.T) {
	p := inRepoStoreProject(t)
	var out bytes.Buffer
	autoMoveStore(&out, true)
	root := paths.ShadowRootFor(p.code)
	wt := filepath.Join(p.base, "wt")
	extGit(t, p.code, "worktree", "add", "-q", "-b", "old", wt, "HEAD~1")
	extra := filepath.Join(wt, ".wipnote", "features", "feat-44444444.html")
	if err := os.WriteFile(extra, []byte(featureRecord("feat-44444444", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(wt)
	out.Reset()
	autoMoveStore(&out, true)
	if got, err := os.ReadFile(filepath.Join(root, ".wipnote", "features", "feat-44444444.html")); err != nil || string(got) != featureRecord("feat-44444444", "") {
		t.Errorf("the worktree's record is not in the app store: %v %q\n%s", err, got, out.String())
	}
	if _, err := os.Stat(filepath.Join(wt, ".wipnote")); err == nil {
		t.Errorf("the worktree's folder is still there:\n%s", out.String())
	}
}

// When a record cannot be copied, nothing in the project changes.
func TestAutoMoveStore_FailedCopyKeepsTheOldFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	p := inRepoStoreProject(t)
	var out bytes.Buffer
	autoMoveStore(&out, false)
	root := paths.ShadowRootFor(p.code)
	late := ".wipnote/features/feat-55555555.html"
	p.write(t, late, featureRecord("feat-55555555", ""))
	features := filepath.Join(root, ".wipnote", "features")
	if err := os.Chmod(features, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(features, 0o755) })
	head := extGit(t, p.code, "rev-parse", "HEAD")
	status := extGit(t, p.code, "status", "--porcelain", "--ignored")
	tracked := extGit(t, p.code, "ls-files", "--", ".wipnote")

	out.Reset()
	autoMoveStore(&out, true)
	if !strings.Contains(out.String(), "feat-55555555.html") {
		t.Errorf("the launch did not report the failed copy:\n%s", out.String())
	}
	if got := mustRead(t, filepath.Join(p.code, late)); got != featureRecord("feat-55555555", "") {
		t.Errorf("the record that could not be copied changed: %q", got)
	}
	if now := extGit(t, p.code, "rev-parse", "HEAD"); now != head {
		t.Errorf("the launch committed although a copy failed: %s -> %s", head, now)
	}
	if now := extGit(t, p.code, "ls-files", "--", ".wipnote"); now != tracked || tracked == "" {
		t.Errorf("the index changed although a copy failed:\n%s\nwas:\n%s", now, tracked)
	}
	if now := extGit(t, p.code, "status", "--porcelain", "--ignored"); now != status {
		t.Errorf("the project's tree changed although a copy failed:\n%s\nwas:\n%s", now, status)
	}
}

// An ordinary command never commits a queued store edit to the project: the
// move copies the file and drops the intent (finding 3).
func TestStoreMove_QueuedIntentIsNotCommitted(t *testing.T) {
	p := inRepoStoreProject(t)
	extGit(t, p.code, "stash", "-q")
	extGit(t, p.code, "checkout", "-q", "--detach")
	rel := ".wipnote/features/feat-11111111.html"
	p.write(t, rel, featureRecord("feat-11111111", " data-status=\"queued\""))
	if err := appendCommitIntent(p.code, []string{rel}, "wipnote: queued edit", "feat-11111111", "update"); err != nil {
		t.Fatal(err)
	}
	// An intent for a path outside the store is not the move's to drop.
	if err := appendCommitIntent(p.code, []string{"docs/note.md"}, "docs: note", "", "update"); err != nil {
		t.Fatal(err)
	}
	head := extGit(t, p.code, "rev-parse", "HEAD")
	status := extGit(t, p.code, "status", "--porcelain")
	if out, err := runCLI(t, "status"); err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	root := paths.ShadowRootFor(p.code)
	if !paths.IsMovedStore(root) {
		t.Fatal("`wipnote status` did not move the store")
	}
	if now := extGit(t, p.code, "rev-parse", "HEAD"); now != head {
		t.Errorf("`wipnote status` committed to the project: %s -> %s\n%s", head, now, extGit(t, p.code, "log", "--oneline", "-2"))
	}
	if now := extGit(t, p.code, "status", "--porcelain"); now != status {
		t.Errorf("`wipnote status` changed the project's tree:\n%s\nwas:\n%s", now, status)
	}
	if got := mustRead(t, filepath.Join(root, rel)); !strings.Contains(got, "data-status=\"queued\"") {
		t.Errorf("the queued file is not in the app store: %q", got)
	}
	ob, err := openCommitOutbox(p.code)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := ob.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RelPaths[0] != "docs/note.md" {
		t.Errorf("the queue holds %+v, want only the intent outside the store", pending)
	}
}

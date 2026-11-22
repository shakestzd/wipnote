package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

// inRepoStoreProject is a repo with a tracked in-repo store, one runtime file
// and one unrelated staged change.
func inRepoStoreProject(t *testing.T) externalProject {
	t.Helper()
	p := newExternalProject(t)
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@example.invalid")
	}
	outbox := filepath.Join(p.base, "outbox.ndjson")
	saved := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return outbox, nil }
	t.Cleanup(func() { commitOutboxPath = saved })

	if err := os.MkdirAll(filepath.Join(p.code, ".wipnote", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	p.write(t, ".wipnote/features/feat-11111111.html", "<article id=\"feat-11111111\"></article>\n")
	p.write(t, ".wipnote/config.json", "{}\n")
	extGit(t, p.code, "add", "-f", ".wipnote")
	extGit(t, p.code, "commit", "-q", "-m", "wipnote: create feat-11111111")
	p.write(t, ".wipnote/writer.lease.pid", "1\n")
	p.write(t, ".wipnote/features/feat-11111111.html", "<article id=\"feat-11111111\" data-status=\"done\"></article>\n")
	p.write(t, "other.go", "package main\n")
	extGit(t, p.code, "add", "other.go")
	return p
}

func TestStoreMove_CopiesAndLeavesTheProjectAlone(t *testing.T) {
	p := inRepoStoreProject(t)
	head := extGit(t, p.code, "rev-parse", "HEAD")
	status := extGit(t, p.code, "status", "--porcelain")

	out, err := runCLI(t, "store", "move", "--dry-run")
	if err != nil || !strings.Contains(out, "would copy 2 file(s)") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if owner := paths.StoreOwnerFor(p.code); owner != p.code {
		t.Fatalf("dry run changed the store owner to %s", owner)
	}

	if out, err = runCLI(t, "store", "move"); err != nil {
		t.Fatalf("store move: %v\n%s", err, out)
	}
	root := paths.ShadowRootFor(p.code)
	if !paths.IsMovedStore(root) {
		t.Fatalf("%s is not marked as moved:\n%s", root, out)
	}
	got, err := os.ReadFile(filepath.Join(root, ".wipnote", "features", "feat-11111111.html"))
	if err != nil || !strings.Contains(string(got), "data-status") {
		t.Fatalf("the working-tree copy of the item was not moved: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(root, ".wipnote", "writer.lease.pid")); err == nil {
		t.Error("a runtime pid file was copied")
	}
	// The moved store wins although the in-repo folder is still there.
	if owner := paths.StoreOwnerFor(p.code); owner != root {
		t.Fatalf("StoreOwnerFor = %s, want the moved store %s", owner, root)
	}
	if dir, _ := paths.ResolveProjectDir(paths.ProjectDirOptions{EventCWD: p.code}); dir != root {
		t.Fatalf("ResolveProjectDir = %s, want the moved store %s", dir, root)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", p.code)
	if dir, _ := paths.ResolveProjectDir(paths.ProjectDirOptions{}); dir != root {
		t.Fatalf("ResolveProjectDir with the code root in the environment = %s, want %s", dir, root)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	if now := extGit(t, p.code, "rev-parse", "HEAD"); now != head {
		t.Errorf("store move without --untrack committed to the project: %s -> %s", head, now)
	}
	if now := extGit(t, p.code, "status", "--porcelain"); now != status {
		t.Errorf("store move without --untrack changed the project's tree:\n%s\nwas:\n%s", now, status)
	}

	if out, err = runCLI(t, "store", "move"); err != nil || !strings.Contains(out, "already moved") {
		t.Fatalf("second move: %v\n%s", err, out)
	}
}

func TestStoreMove_UntrackCommitsOnlyTheStore(t *testing.T) {
	p := inRepoStoreProject(t)
	if out, err := runCLI(t, "store", "move", "--untrack"); err != nil {
		t.Fatalf("store move --untrack: %v\n%s", err, out)
	}
	if files := extGit(t, p.code, "show", "--name-only", "--format=", "HEAD"); strings.Contains(files, "other.go") ||
		!strings.Contains(files, ".wipnote/features/feat-11111111.html") {
		t.Fatalf("the untrack commit holds the wrong paths:\n%s", files)
	}
	if tracked := extGit(t, p.code, "ls-files", "--", ".wipnote"); tracked != "" {
		t.Errorf("store files are still tracked:\n%s", tracked)
	}
	if staged := extGit(t, p.code, "diff", "--cached", "--name-only"); staged != "other.go" {
		t.Errorf("the unrelated staged change did not survive: %q", staged)
	}
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote")); err == nil {
		t.Error("the in-repo folder was not deleted")
	}

	// A commit from before the move puts the folder back. The store stays put.
	root := paths.ShadowRootFor(p.code)
	extGit(t, p.code, "stash", "-q")
	extGit(t, p.code, "checkout", "-q", "HEAD~1")
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote")); err != nil {
		t.Fatal("the old commit did not restore the in-repo folder")
	}
	if owner := paths.StoreOwnerFor(p.code); owner != root {
		t.Fatalf("on an old commit StoreOwnerFor = %s, want the moved store %s", owner, root)
	}
}

func TestStoreMove_Refusals(t *testing.T) {
	p := newExternalProject(t)
	if out, err := runCLI(t, "store", "move"); err == nil || !strings.Contains(err.Error(), "no .wipnote/ store to move") {
		t.Fatalf("no store: err=%v\n%s", err, out)
	}
	p.store(t)
	if out, err := runCLI(t, "store", "move"); err == nil || !strings.Contains(err.Error(), "already in the app store") {
		t.Fatalf("app store: err=%v\n%s", err, out)
	}
}

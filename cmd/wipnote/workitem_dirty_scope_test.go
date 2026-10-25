package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// Tests for the item-scoped dirty-source completion gate (bug-34c27c22,
// GH-#261): only files that belong to the completing item may block it.

const ownFile = "internal/foo/own.go"

type dirtyFixture struct {
	root, hgDir, id string
	node            *models.Node
}

// newDirtyFixture makes a clean repo holding one item of the given kind. With
// linked=true the item has one commit (naming it) that touches ownFile.
func newDirtyFixture(t *testing.T, kind string, linked bool) *dirtyFixture {
	t.Helper()
	root, hgDir := prepProject(t)
	id := createItem(t, hgDir, kind, "Scoped dirty "+kind, testSetupTrack(t, hgDir))
	if linked {
		seedProvCommit(t, root, id, ownFile)
	}
	node, err := htmlparse.ParseFile(itemArtifactPath(t, hgDir, id))
	if err != nil {
		t.Fatalf("parse artifact: %v", err)
	}
	return &dirtyFixture{root: root, hgDir: hgDir, id: id, node: node}
}

func (f *dirtyFixture) gate(t *testing.T, allowDirty bool) (note, stderr string, err error) {
	t.Helper()
	stderr = captureStderr(t, func() {
		note, err = checkUncommittedSourceCompleteGate(newCompletionEvidence(f.hgDir, f.id, f.node), allowDirty)
	})
	return note, stderr, err
}

// dirtyOwn rewrites the committed own file; dirtyTracked rewrites a tracked
// file no commit of the item touched; dirtyUntracked adds a new file.
func (f *dirtyFixture) dirtyOwn(t *testing.T)     { t.Helper(); appendTo(t, f.root, ownFile) }
func (f *dirtyFixture) dirtyTracked(t *testing.T) { t.Helper(); appendTo(t, f.root, "README") }
func (f *dirtyFixture) dirtyUntracked(t *testing.T) {
	t.Helper()
	writeSourceFile(t, f.root, "scratch/new.go")
}

func appendTo(t *testing.T, root, rel string) {
	t.Helper()
	fh, err := os.OpenFile(filepath.Join(root, rel), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString("// edit\n"); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, label, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s missing %q:\n%s", label, w, text)
		}
	}
}

func mustNotContain(t *testing.T, label, text string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(text, b) {
			t.Errorf("%s must not contain %q:\n%s", label, b, text)
		}
	}
}

// gateOnly runs the gate without an artifact node and returns just the error.
func gateOnly(wipnoteDir, id string, allowDirty bool) error {
	_, err := checkUncommittedSourceCompleteGate(newCompletionEvidence(wipnoteDir, id, nil), allowDirty)
	return err
}

var dirtyScopeKinds = []string{"feature", "spike", "bug"}

func TestUncommittedScope_UnrelatedTrackedDoesNotBlock(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			f.dirtyTracked(t)
			note, stderr, err := f.gate(t, false)
			if err != nil {
				t.Fatalf("unrelated tracked file must not block: %v", err)
			}
			for label, text := range map[string]string{"note": note, "stderr": stderr} {
				mustContain(t, label, text, "unrelated", "README", f.id)
				mustNotContain(t, label, text, "(untracked)")
			}
		})
	}
}

func TestUncommittedScope_UnrelatedUntrackedDoesNotBlock(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			f.dirtyUntracked(t)
			note, stderr, err := f.gate(t, false)
			if err != nil {
				t.Fatalf("unrelated untracked file must not block: %v", err)
			}
			for label, text := range map[string]string{"note": note, "stderr": stderr} {
				mustContain(t, label, text, "scratch/new.go (untracked)")
			}
		})
	}
}

func TestUncommittedScope_OwnDirtyBlocksAndListsOnlyOwn(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			f.dirtyOwn(t)
			_, _, err := f.gate(t, false)
			if err == nil {
				t.Fatal("own dirty file must block")
			}
			msg := err.Error()
			mustContain(t, "error", msg, "refusing to complete "+f.id,
				"files from this item have uncommitted changes", "scanned worktree",
				ownFile, "git add "+ownFile, f.id+": commit implementation", "--allow-dirty")
			mustNotContain(t, "error", msg, "uncommitted source changes outside")
		})
	}
}

func TestUncommittedScope_OwnAndUnrelated_BlockNamesOnlyOwn(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			f.dirtyOwn(t)
			f.dirtyTracked(t)
			f.dirtyUntracked(t)
			_, _, err := f.gate(t, false)
			if err == nil {
				t.Fatal("own dirty file must block")
			}
			mustContain(t, "error", err.Error(), ownFile)
			mustNotContain(t, "error", err.Error(), "README", "scratch/new.go")
		})
	}
}

func TestUncommittedScope_NoLinkedCommitsDoesNotBlock(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, false)
			f.dirtyTracked(t)
			f.dirtyUntracked(t)
			note, _, err := f.gate(t, false)
			if err != nil {
				t.Fatalf("item without commits must not block: %v", err)
			}
			mustContain(t, "note", note, "not attributed to "+f.id, "README", "scratch/new.go (untracked)")
			mustNotContain(t, "note", note, "unrelated")
		})
	}
}

func TestUncommittedScope_AllowDirtyWarnsAndNotes(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			f.dirtyOwn(t)
			f.dirtyTracked(t)
			note, stderr, err := f.gate(t, true)
			if err != nil {
				t.Fatalf("--allow-dirty must waive the own-file block: %v", err)
			}
			mustContain(t, "stderr", stderr, "allow-dirty warning:", ownFile, "unrelated", "README")
			mustContain(t, "note", note, "README")
			mustNotContain(t, "note", note, ownFile)
		})
	}
}

func TestUncommittedScope_CleanTreeIsSilent(t *testing.T) {
	f := newDirtyFixture(t, "feature", true)
	note, stderr, err := f.gate(t, false)
	if err != nil || note != "" || stderr != "" {
		t.Fatalf("clean tree: want no error and no output, got err=%v note=%q stderr=%q", err, note, stderr)
	}
}

func TestUncommittedScope_IgnoredAndWipnoteFilesNeverListed(t *testing.T) {
	f := newDirtyFixture(t, "feature", true)
	writeSourceFile(t, f.root, "provenancegatetest") // matches the fixture .gitignore
	writeSourceFile(t, f.root, ".claude/settings.local.json")
	writeSourceFile(t, f.root, ".wipnote/notes/x.md")
	note, stderr, err := f.gate(t, false)
	if err != nil || note != "" || stderr != "" {
		t.Fatalf("ignored/excluded files must be invisible, got err=%v note=%q stderr=%q", err, note, stderr)
	}
}

// #161: in a linked worktree the scan is that worktree only; a file dirty in a
// sibling tree or the owner's checkout appears in neither list. The branch
// diff counts as own, even for a commit that does not name the item.
func TestUncommittedScope_LinkedWorktreeScopeAndBranchDiff(t *testing.T) {
	f := newDirtyFixture(t, "feature", true)
	wtA := addLinkedWorktree(t, f.root, "feat-a")
	wtB := addLinkedWorktree(t, f.root, "feat-b")
	writeSourceFile(t, wtB, "sibling/dirty.go")
	writeSourceFile(t, f.root, "owner/dirty.go")
	t.Chdir(wtA)

	note, stderr, err := f.gate(t, false)
	if err != nil {
		t.Fatalf("clean worktree must pass: %v", err)
	}
	mustNotContain(t, "output", note+stderr, "sibling/dirty.go", "owner/dirty.go")

	// Anonymous branch commit, then dirty again: own through the branch diff.
	writeSourceFile(t, wtA, "internal/a_branch.go")
	provGit(t, wtA, "add", "--", "internal/a_branch.go")
	provGit(t, wtA, "commit", "-q", "-m", "wip: unlinked")
	appendTo(t, wtA, "internal/a_branch.go")
	writeSourceFile(t, wtA, "a_unrelated.go")
	_, _, err = f.gate(t, false)
	if err == nil {
		t.Fatal("dirty file from the branch diff must block")
	}
	mustContain(t, "error", err.Error(), "scanned worktree", "branch feat-a", "internal/a_branch.go")
	mustNotContain(t, "error", err.Error(), "sibling/dirty.go", "owner/dirty.go", "a_unrelated.go")
}

// A file touched by a sibling item's commit is unrelated here.
func TestUncommittedScope_SiblingItemFileIsUnrelated(t *testing.T) {
	f := newDirtyFixture(t, "feature", true)
	seedProvCommit(t, f.root, "feat-deadbeef", "internal/sibling.go")
	appendTo(t, f.root, "internal/sibling.go")
	note, _, err := f.gate(t, false)
	if err != nil {
		t.Fatalf("sibling item's file must not block: %v", err)
	}
	mustContain(t, "note", note, "internal/sibling.go")
}

// #114: the gate stays project-type agnostic. A manifest-less repo with an own
// dirty file completes through --allow-dirty --accepted-advisory.
func TestUncommittedScope_AllowDirtyAdvisoryCompletesManifestless(t *testing.T) {
	for _, kind := range dirtyScopeKinds {
		t.Run(kind, func(t *testing.T) {
			f := newDirtyFixture(t, kind, true)
			seedImplementedIn(t, f.hgDir, f.id)
			f.dirtyOwn(t)
			f.dirtyUntracked(t)
			wiAcceptedAdvisory, wiAllowDirtyComplete = "no manifest; accepted", true
			t.Cleanup(func() { wiAcceptedAdvisory, wiAllowDirtyComplete = "", false })

			var err error
			stdout := captureStdout(t, func() {
				captureStderr(t, func() { err = runWiSetStatus(kind, f.id, "done") })
			})
			if err != nil {
				t.Fatalf("--allow-dirty --accepted-advisory must complete: %v", err)
			}
			mustContain(t, "stdout", stdout, "Completed: "+f.id, "scratch/new.go (untracked)")
		})
	}
}

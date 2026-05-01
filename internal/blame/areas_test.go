package blame_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shakestzd/htmlgraph/internal/blame"
)

// boolPtr is a test helper to get a *bool from a literal.
func boolPtr(b bool) *bool { return &b }

// gitInit makes root a git repo with the given files committed, so
// blame.WalkAreas (which drives the inventory off `git ls-files`) sees them.
func gitInit(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "--"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed"},
	} {
		if args[len(args)-1] == "--" {
			args = append(args, files...)
		}
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestWalkAreas_ByTrack_Grouping(t *testing.T) {
	database := openTestDB(t)

	insertTrack(t, database, "trk-areas-a", "Track A")
	insertTrack(t, database, "trk-areas-b", "Track B")
	insertFeature(t, database, "feat-areas-a1", "Feature A1", "trk-areas-a")
	insertFeature(t, database, "feat-areas-a2", "Feature A2", "trk-areas-a")
	insertFeature(t, database, "feat-areas-b1", "Feature B1", "trk-areas-b")

	// Build a real temp directory with files.
	root := t.TempDir()
	writeFile(t, root, "file-a.go")
	writeFile(t, root, "file-b.go")
	writeFile(t, root, "file-c.go")
	gitInit(t, root, "file-a.go", "file-b.go", "file-c.go")

	insertFeatureFile(t, database, "ff-areas-1", "feat-areas-a1", "file-a.go")
	insertFeatureFile(t, database, "ff-areas-2", "feat-areas-a2", "file-a.go")
	insertFeatureFile(t, database, "ff-areas-3", "feat-areas-b1", "file-b.go")
	// file-c.go: untracked

	res, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{
		IncludeUntracked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("WalkAreas: %v", err)
	}

	if len(res.ByTrack) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(res.ByTrack))
	}

	// trk-areas-a has 1 file (file-a.go touched by 2 features); should sort first (tied on file count 1 each, falls to ID sort)
	foundA := false
	for _, ta := range res.ByTrack {
		if ta.TrackID == "trk-areas-a" {
			foundA = true
			if len(ta.Files) != 1 {
				t.Errorf("Track A: expected 1 file, got %d", len(ta.Files))
			}
		}
	}
	if !foundA {
		t.Error("Track A not found in ByTrack")
	}

	// file-c.go should be in Untracked
	if len(res.Untracked) != 1 || res.Untracked[0] != "file-c.go" {
		t.Errorf("expected Untracked=[file-c.go], got %v", res.Untracked)
	}
}

func TestWalkAreas_ByFile_Inverse(t *testing.T) {
	database := openTestDB(t)

	insertTrack(t, database, "trk-byfile", "Track ByFile")
	insertFeature(t, database, "feat-byfile", "Feature ByFile", "trk-byfile")

	root := t.TempDir()
	writeFile(t, root, "alpha.go")
	writeFile(t, root, "beta.go")
	gitInit(t, root, "alpha.go", "beta.go")

	insertFeatureFile(t, database, "ff-byfile-1", "feat-byfile", "alpha.go")
	insertFeatureFile(t, database, "ff-byfile-2", "feat-byfile", "beta.go")

	res, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{
		ByFile:           true,
		IncludeUntracked: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("WalkAreas ByFile: %v", err)
	}

	if len(res.ByFile) != 2 {
		t.Fatalf("expected 2 file entries, got %d", len(res.ByFile))
	}
	// Sorted by path: alpha.go < beta.go
	if res.ByFile[0].Path != "alpha.go" {
		t.Errorf("first file: got %q, want alpha.go", res.ByFile[0].Path)
	}
	if len(res.ByFile[0].Tracks) != 1 || res.ByFile[0].Tracks[0].ID != "trk-byfile" {
		t.Errorf("alpha.go tracks: got %v", res.ByFile[0].Tracks)
	}
}

func TestWalkAreas_UntrackedDetection(t *testing.T) {
	database := openTestDB(t)

	root := t.TempDir()
	writeFile(t, root, "untracked1.go")
	writeFile(t, root, "untracked2.go")
	gitInit(t, root, "untracked1.go", "untracked2.go")

	res, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{
		IncludeUntracked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("WalkAreas: %v", err)
	}

	if len(res.Untracked) != 2 {
		t.Errorf("expected 2 untracked files, got %d: %v", len(res.Untracked), res.Untracked)
	}
	if len(res.ByTrack) != 0 {
		t.Errorf("expected no tracks, got %d", len(res.ByTrack))
	}
}

func TestWalkAreas_UntrackedExcluded(t *testing.T) {
	database := openTestDB(t)

	root := t.TempDir()
	writeFile(t, root, "noattr.go")
	gitInit(t, root, "noattr.go")

	res, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{
		IncludeUntracked: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("WalkAreas: %v", err)
	}

	if len(res.Untracked) != 0 {
		t.Errorf("expected empty Untracked when IncludeUntracked=false, got %v", res.Untracked)
	}
}

func TestWalkAreas_RootScoping(t *testing.T) {
	database := openTestDB(t)

	insertTrack(t, database, "trk-scope", "Track Scope")
	insertFeature(t, database, "feat-scope", "Feature Scope", "trk-scope")

	// Two separate roots — only walk the first.
	root1 := t.TempDir()
	root2 := t.TempDir()

	writeFile(t, root1, "in-scope.go")
	writeFile(t, root2, "out-of-scope.go")
	gitInit(t, root1, "in-scope.go")
	gitInit(t, root2, "out-of-scope.go")

	// The DB matches on relative paths; register only out-of-scope.go
	insertFeatureFile(t, database, "ff-scope-out", "feat-scope", "out-of-scope.go")

	res, err := blame.WalkAreas(context.Background(), database, root1, blame.WalkOptions{
		IncludeUntracked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("WalkAreas: %v", err)
	}

	// root1 only has in-scope.go which is untracked (the feature_files row uses "out-of-scope.go")
	if len(res.ByTrack) != 0 {
		t.Errorf("expected 0 tracks when walking root1, got %d", len(res.ByTrack))
	}
	if len(res.Untracked) != 1 {
		t.Errorf("expected 1 untracked file, got %d", len(res.Untracked))
	}
}

// TestWalkAreas_ExcludesHtmlgraphDir verifies that work-item HTML under
// .htmlgraph/ is excluded — those files have their own attribution model
// and would otherwise drown out the source-code inventory.
func TestWalkAreas_ExcludesHtmlgraphDir(t *testing.T) {
	database := openTestDB(t)

	root := t.TempDir()
	writeFile(t, root, "visible.go")
	hgDir := filepath.Join(root, ".htmlgraph")
	if err := os.MkdirAll(hgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, hgDir, "trk-x.html")
	gitInit(t, root, "visible.go", ".htmlgraph/trk-x.html")

	insertTrack(t, database, "trk-htmlgraph", "Track Htmlgraph")
	insertFeature(t, database, "feat-htmlgraph", "Feature Htmlgraph", "trk-htmlgraph")
	insertFeatureFile(t, database, "ff-htmlgraph", "feat-htmlgraph", ".htmlgraph/trk-x.html")

	res, err := blame.WalkAreas(context.Background(), database, root, blame.WalkOptions{
		IncludeUntracked: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("WalkAreas: %v", err)
	}

	for _, ta := range res.ByTrack {
		for _, f := range ta.Files {
			if filepath.Dir(f.Path) == ".htmlgraph" {
				t.Errorf("file under .htmlgraph/ should not appear in ByTrack: %s", f.Path)
			}
		}
	}
	for _, u := range res.Untracked {
		if filepath.Dir(u) == ".htmlgraph" {
			t.Errorf("file under .htmlgraph/ should not appear in Untracked: %s", u)
		}
	}
}

// writeFile creates an empty file at dir/name for walking tests.
func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupWorktreeGitRepo creates a temp git repo with an initial commit and returns its path.
func setupWorktreeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	f, err := os.Create(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("create README: %v", err)
	}
	f.WriteString("# Test")
	f.Close()

	exec.Command("git", "-C", dir, "add", ".").Run() //nolint:errcheck
	exec.Command("git", "-C", dir, "commit",         //nolint:errcheck
		"-c", "user.name=test", "-c", "user.email=test@test.com",
		"-m", "initial",
	).Run()

	return dir
}

// writeFeatureHTML writes a minimal feature HTML file. If trackID is empty, data-track-id is omitted.
func writeFeatureHTML(t *testing.T, dir, featureID, trackID string) {
	t.Helper()
	featureDir := filepath.Join(dir, ".wipnote", "features")
	if err := os.MkdirAll(featureDir, 0755); err != nil {
		t.Fatalf("mkdir features: %v", err)
	}
	trackAttr := ""
	if trackID != "" {
		trackAttr = ` data-track-id="` + trackID + `"`
	}
	html := `<article id="` + featureID + `"` + trackAttr + ` data-status="todo">` +
		`<header><h1>Test Feature</h1></header>` +
		`<section data-content><p>Description</p></section>` +
		`</article>`
	path := filepath.Join(featureDir, featureID+".html")
	if err := os.WriteFile(path, []byte(html), 0644); err != nil {
		t.Fatalf("write feature HTML: %v", err)
	}
}

// TestEnsureForFeature_CreatesOnFirstCall verifies that the first call creates the worktree.
func TestEnsureForFeature_CreatesOnFirstCall(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	writeFeatureHTML(t, dir, "feat-aaa", "")

	path, err := EnsureForFeature("feat-aaa", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForFeature: %v", err)
	}

	expected := filepath.Join(dir, ".claude", "worktrees", "feat-aaa")
	if path != expected {
		t.Errorf("path: got %q, want %q", path, expected)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("worktree dir does not exist: %v", err)
	}
}

// TestEnsureForFeature_IdempotentSecondCall verifies that a second call returns the same path without error.
func TestEnsureForFeature_IdempotentSecondCall(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	writeFeatureHTML(t, dir, "feat-bbb", "")

	path1, err := EnsureForFeature("feat-bbb", dir, io.Discard)
	if err != nil {
		t.Fatalf("first EnsureForFeature: %v", err)
	}

	path2, err := EnsureForFeature("feat-bbb", dir, io.Discard)
	if err != nil {
		t.Fatalf("second EnsureForFeature: %v", err)
	}

	if path1 != path2 {
		t.Errorf("paths differ: %q vs %q", path1, path2)
	}
}

// TestEnsureForFeature_WithParentTrack verifies that when a feature has a parent track,
// the track worktree path is returned (not the feature path).
func TestEnsureForFeature_WithParentTrack(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	writeFeatureHTML(t, dir, "feat-ccc", "trk-parent111")

	path, err := EnsureForFeature("feat-ccc", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForFeature: %v", err)
	}

	expectedTrackPath := filepath.Join(dir, ".claude", "worktrees", "trk-parent111")
	if path != expectedTrackPath {
		t.Errorf("path: got %q, want track path %q", path, expectedTrackPath)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("track worktree dir does not exist: %v", err)
	}
	// Feature worktree should NOT exist
	featurePath := filepath.Join(dir, ".claude", "worktrees", "feat-ccc")
	if _, err := os.Stat(featurePath); err == nil {
		t.Error("feature worktree should NOT exist when feature has a parent track")
	}
}

// TestEnsureForTrack_Idempotent verifies that repeated calls to EnsureForTrack return the same path.
func TestEnsureForTrack_Idempotent(t *testing.T) {
	dir := setupWorktreeGitRepo(t)

	path1, err := EnsureForTrack("trk-ttt111", dir, io.Discard)
	if err != nil {
		t.Fatalf("first EnsureForTrack: %v", err)
	}

	path2, err := EnsureForTrack("trk-ttt111", dir, io.Discard)
	if err != nil {
		t.Fatalf("second EnsureForTrack: %v", err)
	}

	if path1 != path2 {
		t.Errorf("paths differ: %q vs %q", path1, path2)
	}

	expected := filepath.Join(dir, ".claude", "worktrees", "trk-ttt111")
	if path1 != expected {
		t.Errorf("path: got %q, want %q", path1, expected)
	}
}

// TestEnsureForAgent_ThreeArgSignature verifies the three-arg signature and naming convention.
func TestEnsureForAgent_ThreeArgSignature(t *testing.T) {
	// Use setupAgentGitRepo which correctly creates an initial commit.
	dir := setupAgentGitRepo(t)

	// First create the track branch that the agent will branch from
	exec.Command("git", "-C", dir, "branch", "trk-agent111").Run() //nolint:errcheck

	path, err := EnsureForAgent("trk-agent111", "slice-3", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForAgent: %v", err)
	}

	expected := filepath.Join(dir, ".claude", "worktrees", "trk-agent111", "agent-slice-3")
	if path != expected {
		t.Errorf("path: got %q, want %q", path, expected)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("agent worktree dir does not exist: %v", err)
	}
}

// TestEnsureForFeature_WriterReceivesProgress verifies that progress lines are written to the io.Writer.
func TestEnsureForFeature_WriterReceivesProgress(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	writeFeatureHTML(t, dir, "feat-ddd", "")

	var buf bytes.Buffer
	_, err := EnsureForFeature("feat-ddd", dir, &buf)
	if err != nil {
		t.Fatalf("EnsureForFeature: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "feat-ddd") {
		t.Errorf("expected writer to receive progress containing feat-ddd; got: %q", output)
	}
}

// TestEnsureForFeature_DiscardWriter verifies that passing io.Discard does not panic.
func TestEnsureForFeature_DiscardWriter(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	writeFeatureHTML(t, dir, "feat-eee", "")

	// Should not panic — just discard all output
	_, err := EnsureForFeature("feat-eee", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForFeature with Discard: %v", err)
	}
}

// writeTrackHTMLWithTitle writes a minimal track HTML file with the given title.
func writeTrackHTMLWithTitle(t *testing.T, dir, trackID, title string) {
	t.Helper()
	trackDir := filepath.Join(dir, ".wipnote", "tracks")
	if err := os.MkdirAll(trackDir, 0755); err != nil {
		t.Fatalf("mkdir tracks: %v", err)
	}
	html := `<article id="` + trackID + `" data-status="todo">` +
		`<header><h1>` + title + `</h1></header>` +
		`<section data-content><p>Description</p></section>` +
		`</article>`
	path := filepath.Join(trackDir, trackID+".html")
	if err := os.WriteFile(path, []byte(html), 0644); err != nil {
		t.Fatalf("write track HTML: %v", err)
	}
}

// TestEnsureForTrackWithTitle_FeatureParentTrack verifies that when a feature has a
// titled parent track, EnsureForTrackWithTitle is used and produces a path containing
// the track ID (the titled worktree path), not the bare feature worktree path.
// This covers the yolo.go code path at launchYolo/launchYoloDev where featureID != ""
// and the feature resolves to a parent track with a title.
func TestEnsureForTrackWithTitle_FeatureParentTrack(t *testing.T) {
	dir := setupWorktreeGitRepo(t)
	trackID := "trk-titled99"
	featureID := "feat-child99"

	// Create a track with a title.
	writeTrackHTMLWithTitle(t, dir, trackID, "Titled Parent Track")

	// Create a feature that points to the titled track.
	featureDir := filepath.Join(dir, ".wipnote", "features")
	if err := os.MkdirAll(featureDir, 0755); err != nil {
		t.Fatalf("mkdir features: %v", err)
	}
	html := `<article id="` + featureID + `" data-track-id="` + trackID + `" data-status="todo">` +
		`<header><h1>Child Feature</h1></header>` +
		`<section data-content><p>Description</p></section>` +
		`</article>`
	if err := os.WriteFile(filepath.Join(featureDir, featureID+".html"), []byte(html), 0644); err != nil {
		t.Fatalf("write feature HTML: %v", err)
	}

	// Resolve the parent track and its title (mirrors the yolo.go logic).
	parentTrackID := resolveTrackForFeature(featureID, dir)
	if parentTrackID == "" {
		t.Fatal("expected resolveTrackForFeature to return a track ID")
	}
	if parentTrackID != trackID {
		t.Fatalf("resolveTrackForFeature: got %q, want %q", parentTrackID, trackID)
	}

	parentTitle := resolveTrackTitle(parentTrackID, "", dir)
	if parentTitle == "" {
		t.Fatal("expected resolveTrackTitle to return the track title")
	}

	// Call EnsureForTrackWithTitle (the titled path, not bare EnsureForTrack).
	path, err := EnsureForTrackWithTitle(parentTitle, parentTrackID, dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForTrackWithTitle: %v", err)
	}

	// The path must contain the track ID (titled path includes trackID).
	if !strings.Contains(path, trackID) {
		t.Errorf("expected titled worktree path to contain track ID %q, got: %q", trackID, path)
	}

	// The bare feature worktree must NOT exist.
	featurePath := filepath.Join(dir, ".claude", "worktrees", featureID)
	if _, err := os.Stat(featurePath); err == nil {
		t.Error("bare feature worktree should NOT exist when feature has a titled parent track")
	}

	// The titled track worktree must exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("titled track worktree does not exist at %s: %v", path, err)
	}
}

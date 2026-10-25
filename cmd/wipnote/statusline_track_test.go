package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/projection"
)

// countProjectionLoads wraps the loadProjectionFn seam and counts calls.
func countProjectionLoads(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := loadProjectionFn
	loadProjectionFn = func(dir string) (*projection.Snapshot, error) {
		n++
		return orig(dir)
	}
	t.Cleanup(func() { loadProjectionFn = orig })
	return &n
}

// statuslineFixture starts one feature of a two-feature track (the other is
// done) under the fixture session and returns the session id and track id.
func statuslineFixture(t *testing.T) (hgDir, sessionID, trackID string) {
	t.Helper()
	tmpDir, hgDir := prepProject(t)
	trackID = testSetupTrack(t, hgDir)
	for _, title := range []string{"Finished Work", "Status Line Item"} {
		if err := testCreate("feature", title, trackID, "medium", false, false); err != nil {
			t.Fatal(err)
		}
	}
	done, id := featureIDByTitle(t, hgDir, "Finished Work"), featureIDByTitle(t, hgDir, "Status Line Item")
	t.Chdir(tmpDir)
	wiAcceptedAdvisory, wiResearchWaiver = "", "fixture"
	t.Cleanup(func() { wiResearchWaiver = "" })
	if err := wiSetStatusWithAgent("feature", done, "done", "", ""); err != nil {
		t.Fatalf("complete %s: %v", done, err)
	}
	sessionID = os.Getenv("WIPNOTE_SESSION_ID")
	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return hgDir, sessionID, trackID
}

// featureIDByTitle returns the id of the fixture feature titled title.
func featureIDByTitle(t *testing.T, hgDir, title string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(hgDir, "features", "feat-*.html"))
	for _, f := range files {
		if n, err := htmlparse.ParseFile(f); err == nil && n.Title == title {
			return n.ID
		}
	}
	t.Fatalf("no feature titled %q", title)
	return ""
}

// TestStatusline_OpenClaimSkipsProjectLoad pins feat-c30d0792: the status line
// for a session with an open claim renders its track from the item's own
// edges and the track file, without loading the whole project.
func TestStatusline_OpenClaimSkipsProjectLoad(t *testing.T) {
	hgDir, sessionID, _ := statuslineFixture(t)
	loads := countProjectionLoads(t)
	out := captureStdout(t, func() { _ = statuslineFromSession(hgDir, sessionID) })
	want := iconFor("track") + " Test Track [1/2] → " + iconFor("feature") + " Status Line Item\n"
	if out != want {
		t.Errorf("status line = %q, want %q", out, want)
	}
	if *loads != 0 {
		t.Errorf("status line loaded the whole project %d time(s), want 0", *loads)
	}
}

// TestStatusline_MissingTrackFileFallsBack: when the track file is not where
// the fast path looks (archived, or another layout), the status line falls
// back to the projection and renders what it always did.
func TestStatusline_MissingTrackFileFallsBack(t *testing.T) {
	hgDir, sessionID, trackID := statuslineFixture(t)
	if err := os.Remove(filepath.Join(hgDir, "tracks", trackID+".html")); err != nil {
		t.Fatal(err)
	}
	loads := countProjectionLoads(t)
	out := captureStdout(t, func() { _ = statuslineFromSession(hgDir, sessionID) })
	if !strings.HasPrefix(out, iconFor("track")+" "+trackID+" [1/2] → ") {
		t.Errorf("status line = %q, want the track id as title and [1/2]", out)
	}
	if *loads != 1 {
		t.Errorf("fallback loaded the projection %d time(s), want 1", *loads)
	}
}

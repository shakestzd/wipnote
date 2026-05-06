package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecutePreview_BuildsEnvelope is a unit test for buildExecutePreview that
// sets up a minimal .wipnote/ tree and verifies the JSON envelope contains the
// track, linked bugs, and git state fields.
func TestExecutePreview_BuildsEnvelope(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")

	// Create directory skeleton.
	for _, sub := range []string{"tracks", "features", "bugs", "plans", "spikes", "specs"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	// Track with three direct edges: bug, feature, spike. Spike uses the
	// canonical "spk-" prefix to guard against regression of the original
	// "spike-" prefix bug in the first execute-preview implementation.
	trackHTML := `<!DOCTYPE html><html><body>
<article id="trk-test001" data-type="track" data-status="in-progress" data-priority="medium">
<header><h1>Sample Track</h1></header>
<nav data-graph-edges>
  <section data-edge-type="contains">
    <ul>
      <li><a href="bug-test001.html" data-relationship="contains">Sample Bug</a></li>
      <li><a href="feat-test001.html" data-relationship="contains">Sample Feature</a></li>
      <li><a href="spk-test001.html" data-relationship="contains">Sample Spike</a></li>
    </ul>
  </section>
</nav>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "tracks", "trk-test001.html"), trackHTML)

	bugHTML := `<!DOCTYPE html><html><body>
<article id="bug-test001" data-type="bug" data-status="todo" data-priority="medium">
<header><h1>Sample Bug</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "bugs", "bug-test001.html"), bugHTML)

	featHTML := `<!DOCTYPE html><html><body>
<article id="feat-test001" data-type="feature" data-status="done" data-priority="medium">
<header><h1>Sample Feature</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "features", "feat-test001.html"), featHTML)

	spikeHTML := `<!DOCTYPE html><html><body>
<article id="spk-test001" data-type="spike" data-status="todo" data-priority="medium">
<header><h1>Sample Spike</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "spikes", "spk-test001.html"), spikeHTML)

	preview, err := buildExecutePreview(hgDir, "trk-test001")
	if err != nil {
		t.Fatalf("buildExecutePreview: %v", err)
	}

	if preview.Track == nil {
		t.Fatal("Track is nil")
	}
	if preview.Track.ID != "trk-test001" {
		t.Errorf("Track.ID = %q, want trk-test001", preview.Track.ID)
	}
	if got := len(preview.Bugs); got != 1 {
		t.Errorf("len(Bugs) = %d, want 1", got)
	}
	if got := len(preview.Features); got != 1 {
		t.Errorf("len(Features) = %d, want 1", got)
	}
	if got := len(preview.Spikes); got != 1 {
		t.Errorf("len(Spikes) = %d, want 1 (spk- prefix regression)", got)
	}

	// Marshal to JSON to prove the envelope serializes cleanly — mirrors what
	// the --format json path does.
	b, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	s := string(b)
	for _, key := range []string{`"track"`, `"bugs"`, `"features"`, `"git"`} {
		if !strings.Contains(s, key) {
			t.Errorf("json envelope missing %s", key)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// TestExecutePreview_DirectoryBackedTrack verifies tracks stored at
// tracks/<id>/index.html resolve correctly (regression guard for roborev
// finding that resolveNodePath-only lookup regressed the directory form).
func TestExecutePreview_DirectoryBackedTrack(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(hgDir, "tracks", "trk-dirform"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	trackHTML := `<!DOCTYPE html><html><body>
<article id="trk-dirform" data-type="track" data-status="todo" data-priority="medium">
<header><h1>Directory-Backed Track</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "tracks", "trk-dirform", "index.html"), trackHTML)

	preview, err := buildExecutePreview(hgDir, "trk-dirform")
	if err != nil {
		t.Fatalf("buildExecutePreview: %v", err)
	}
	if preview.Track == nil || preview.Track.ID != "trk-dirform" {
		t.Errorf("directory-backed track not resolved: got %+v", preview.Track)
	}
}

// TestExecutePreview_PlanByFeatureID verifies plans that reference a linked
// feature via data-feature-id surface in the envelope even when they are
// not directly linked from the track (regression guard for the HIGH finding
// that plans were under-discovered).
func TestExecutePreview_PlanByFeatureID(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")
	for _, sub := range []string{"tracks", "features", "plans"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	trackHTML := `<!DOCTYPE html><html><body>
<article id="trk-planned" data-type="track" data-status="in-progress" data-priority="medium">
<header><h1>Planned Track</h1></header>
<nav data-graph-edges>
  <section data-edge-type="contains">
    <ul>
      <li><a href="feat-planned.html" data-relationship="contains">Planned Feature</a></li>
    </ul>
  </section>
</nav>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "tracks", "trk-planned.html"), trackHTML)

	featHTML := `<!DOCTYPE html><html><body>
<article id="feat-planned" data-type="feature" data-status="todo" data-priority="medium">
<header><h1>Planned Feature</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "features", "feat-planned.html"), featHTML)

	// Plan is NOT linked from the track — it references feat-planned via
	// data-feature-id only. Execute-preview must still surface it.
	planHTML := `<!DOCTYPE html><html><body>
<article id="plan-indirect" data-type="plan" data-status="draft" data-priority="medium" data-feature-id="feat-planned">
<header><h1>Indirect Plan</h1></header>
</article>
</body></html>`
	writeFile(t, filepath.Join(hgDir, "plans", "plan-indirect.html"), planHTML)

	preview, err := buildExecutePreview(hgDir, "trk-planned")
	if err != nil {
		t.Fatalf("buildExecutePreview: %v", err)
	}
	if got := len(preview.Plans); got != 1 {
		t.Errorf("len(Plans) = %d, want 1 (plan should surface via data-feature-id)", got)
	}
}

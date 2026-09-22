package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/core/workitem"
)

// wipResetFixture builds a .wipnote dir with one live session (open in the
// ledger), one dead session (closed in the ledger), one session never in the
// ledger, and an orphaned item with no session edge. It points projectDirFlag
// at the fixture so runWipReset resolves it.
//
// Items:
//
//	feat-live1   owned by 11111111-1111-4111-8111-111111111111   (open row)        — must survive --dead
//	feat-dead1   owned by 22222222-2222-4222-8222-222222222222   (closed row)      — --dead
//	bug-dead2    owned by 33333333-3333-4333-8333-333333333333   (no ledger row)   — --dead
//	feat-orph1   no implemented_in edge                 — --orphaned only
func wipResetFixture(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	projectDirFlag = tmpDir
	t.Cleanup(func() { projectDirFlag = "" })

	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store := sessionledger.NewStore(hgDir)
	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		if _, err := store.Open(sessionledger.Record{SessionID: id, Harness: "claude-code", StartedAt: start}); err != nil {
			t.Fatalf("ledger open %s: %v", id, err)
		}
	}
	if err := store.Close("22222222-2222-4222-8222-222222222222", start.Add(time.Hour)); err != nil {
		t.Fatalf("ledger close: %v", err)
	}

	writeWipResetNode(t, hgDir, "feature", "feat-live1", "Live item", "11111111-1111-4111-8111-111111111111")
	writeWipResetNode(t, hgDir, "feature", "feat-dead1", "Dead item", "22222222-2222-4222-8222-222222222222")
	writeWipResetNode(t, hgDir, "bug", "bug-dead2", "Gone item", "33333333-3333-4333-8333-333333333333")
	writeWipResetNode(t, hgDir, "feature", "feat-orph1", "Orphan item", "")
	return hgDir
}

func writeWipResetNode(t *testing.T, hgDir, nodeType, id, title, owner string) {
	t.Helper()
	n := &models.Node{
		ID:     id,
		Type:   nodeType,
		Title:  title,
		Status: models.StatusInProgress,
		Edges:  map[string][]models.Edge{},
	}
	if owner != "" {
		n.Edges[string(models.RelImplementedIn)] = []models.Edge{{
			TargetID:     owner,
			Relationship: models.RelImplementedIn,
			Title:        "session " + owner,
			Since:        time.Now().UTC(),
		}}
	}
	if _, err := workitem.WriteNodeHTML(filepath.Join(hgDir, nodeType+"s"), n); err != nil {
		t.Fatalf("WriteNodeHTML %s: %v", id, err)
	}
}

// wipResetStatuses returns id -> status for every fixture item on disk.
func wipResetStatuses(t *testing.T, hgDir string) map[string]models.NodeStatus {
	t.Helper()
	out := map[string]models.NodeStatus{}
	for _, f := range []string{"features/feat-live1", "features/feat-dead1", "bugs/bug-dead2", "features/feat-orph1"} {
		n, err := htmlparse.ParseFile(filepath.Join(hgDir, f+".html"))
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		out[n.ID] = n.Status
	}
	return out
}

func captureWipReset(t *testing.T, force bool, scope wipResetScope) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runWipReset(force, scope)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), runErr
}

func TestWipResetScoped(t *testing.T) {
	cases := []struct {
		name      string
		scope     wipResetScope
		wantReset []string
		wantKept  []string
	}{
		{
			name:      "dead leaves live and orphaned items alone",
			scope:     wipResetScope{Dead: true},
			wantReset: []string{"feat-dead1", "bug-dead2"},
			wantKept:  []string{"feat-live1", "feat-orph1"},
		},
		{
			name:      "session scopes to one owner",
			scope:     wipResetScope{Session: "33333333-3333-4333-8333-333333333333"},
			wantReset: []string{"bug-dead2"},
			wantKept:  []string{"feat-live1", "feat-dead1", "feat-orph1"},
		},
		{
			name:      "session can target a live session explicitly",
			scope:     wipResetScope{Session: "11111111-1111-4111-8111-111111111111"},
			wantReset: []string{"feat-live1"},
			wantKept:  []string{"feat-dead1", "bug-dead2", "feat-orph1"},
		},
		{
			name:      "orphaned selects only edge-less items",
			scope:     wipResetScope{Orphaned: true},
			wantReset: []string{"feat-orph1"},
			wantKept:  []string{"feat-live1", "feat-dead1", "bug-dead2"},
		},
		{
			name:      "dead plus orphaned is a union",
			scope:     wipResetScope{Dead: true, Orphaned: true},
			wantReset: []string{"feat-dead1", "bug-dead2", "feat-orph1"},
			wantKept:  []string{"feat-live1"},
		},
		{
			name:      "unscoped resets everything",
			scope:     wipResetScope{},
			wantReset: []string{"feat-live1", "feat-dead1", "bug-dead2", "feat-orph1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hgDir := wipResetFixture(t)
			out, err := captureWipReset(t, true, tc.scope)
			if err != nil {
				t.Fatalf("runWipReset: %v", err)
			}
			got := wipResetStatuses(t, hgDir)
			for _, id := range tc.wantReset {
				if got[id] != models.StatusTodo {
					t.Errorf("%s: want todo, got %s\n%s", id, got[id], out)
				}
			}
			for _, id := range tc.wantKept {
				if got[id] != models.StatusInProgress {
					t.Errorf("%s: want in-progress (untouched), got %s\n%s", id, got[id], out)
				}
			}
			if !strings.Contains(out, "item(s) reset to todo") {
				t.Errorf("missing summary line:\n%s", out)
			}
		})
	}
}

func TestWipResetDryRunWritesNothing(t *testing.T) {
	hgDir := wipResetFixture(t)
	before := map[string]time.Time{}
	for _, f := range []string{"features/feat-live1", "features/feat-dead1", "bugs/bug-dead2", "features/feat-orph1"} {
		st, err := os.Stat(filepath.Join(hgDir, f+".html"))
		if err != nil {
			t.Fatal(err)
		}
		before[f] = st.ModTime()
	}

	// --dry-run must not need --force.
	out, err := captureWipReset(t, false, wipResetScope{Dead: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run should not error without --force: %v", err)
	}
	if !strings.Contains(out, "DRY RUN") || !strings.Contains(out, "2 item(s) would be reset") {
		t.Errorf("dry-run header missing:\n%s", out)
	}
	for _, want := range []string{"ID", "TYPE", "SESSION", "TITLE", "feat-dead1", "bug-dead2", "[dead?]", "Dead item"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "feat-live1") || strings.Contains(out, "feat-orph1") {
		t.Errorf("dry-run listed out-of-scope items:\n%s", out)
	}

	for id, st := range wipResetStatuses(t, hgDir) {
		if st != models.StatusInProgress {
			t.Errorf("%s: dry-run changed status to %s", id, st)
		}
	}
	for f, mt := range before {
		st, err := os.Stat(filepath.Join(hgDir, f+".html"))
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(mt) {
			t.Errorf("%s: dry-run rewrote the file", f)
		}
	}
}

func TestWipResetScopedWithoutForceRefuses(t *testing.T) {
	hgDir := wipResetFixture(t)
	_, err := captureWipReset(t, false, wipResetScope{Dead: true})
	if err == nil {
		t.Fatal("expected refusal without --force")
	}
	msg := err.Error()
	for _, want := range []string{"2 in-progress item(s)", "dead sessions", "--force", "--dry-run"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal missing %q: %s", want, msg)
		}
	}
	for id, st := range wipResetStatuses(t, hgDir) {
		if st != models.StatusInProgress {
			t.Errorf("%s: refusal changed status to %s", id, st)
		}
	}
}

func TestWipResetScopedNoMatch(t *testing.T) {
	wipResetFixture(t)
	out, err := captureWipReset(t, true, wipResetScope{Session: "44444444-4444-4444-8444-444444444444"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No in-progress items match scope (session 44444444-4444-4444-8444-444444444444)") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestWipResetTargetsOrdering(t *testing.T) {
	items := []*models.Node{
		{ID: "b", Type: "bug", Status: models.StatusInProgress, Edges: map[string][]models.Edge{}},
		{ID: "z", Type: "feature", Status: models.StatusInProgress, Edges: map[string][]models.Edge{
			string(models.RelImplementedIn): {{TargetID: "s2", Relationship: models.RelImplementedIn}},
		}},
		{ID: "a", Type: "feature", Status: models.StatusInProgress, Edges: map[string][]models.Edge{
			string(models.RelImplementedIn): {{TargetID: "s2", Relationship: models.RelImplementedIn}},
		}},
		{ID: "m", Type: "spike", Status: models.StatusInProgress, Edges: map[string][]models.Edge{
			string(models.RelImplementedIn): {{TargetID: "s1", Relationship: models.RelImplementedIn}},
		}},
	}
	got := wipResetTargets(items, map[string]bool{}, wipResetScope{})
	var ids []string
	for _, tg := range got {
		ids = append(ids, tg.Session+"/"+tg.Node.ID)
	}
	want := "s1/m s2/a s2/z unknown/b"
	if strings.Join(ids, " ") != want {
		t.Errorf("order: got %q want %q", strings.Join(ids, " "), want)
	}
}

func TestWipResetHelpDocumentsFlags(t *testing.T) {
	cmd := wipResetCmd()
	for _, f := range []string{"dead", "session", "orphaned", "dry-run", "force"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("flag --%s not registered", f)
		}
	}
	for _, want := range []string{"--dead", "--session <id>", "--orphaned", "--dry-run", "SESSION DEAD?"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("help text missing %q", want)
		}
	}
}

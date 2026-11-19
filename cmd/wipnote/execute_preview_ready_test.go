package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// previewItem describes one promoted slice in a fixture track.
type previewItem struct {
	id        string
	status    string
	blockedBy []string
	files     []string
}

// writePreviewFixture writes a track, one feature per item and a plan YAML
// whose slices carry the item file lists. It returns the .wipnote dir.
func writePreviewFixture(t *testing.T, items []previewItem) string {
	t.Helper()
	hg := filepath.Join(t.TempDir(), ".wipnote")
	for _, sub := range []string{"tracks", "features", "plans"} {
		if err := os.MkdirAll(filepath.Join(hg, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var links, slices strings.Builder
	for i, it := range items {
		fmt.Fprintf(&links, `<li><a href="%s.html" data-relationship="contains">%s</a></li>`, it.id, it.id)
		var edges strings.Builder
		for _, b := range it.blockedBy {
			fmt.Fprintf(&edges, `<li><a href="%s.html" data-relationship="blocked_by">%s</a></li>`, b, b)
		}
		writeFile(t, filepath.Join(hg, "features", it.id+".html"), fmt.Sprintf(`<!DOCTYPE html><html><body>
<article id="%s" data-type="feature" data-status="%s" data-priority="medium">
<header><h1>Title of %s</h1></header>
<nav data-graph-edges><section data-edge-type="blocked_by"><ul>%s</ul></section>
<section data-edge-type="planned_in"><ul><li><a href="plan-prev0001.html" data-relationship="planned_in">plan</a></li></ul></section></nav>
</article></body></html>`, it.id, it.status, it.id, edges.String()))
		fmt.Fprintf(&slices, "  - id: slice-%d\n    feature_id: %s\n    num: %d\n    title: Title of %s\n    files:\n", i+1, it.id, i+1, it.id)
		for _, f := range it.files {
			fmt.Fprintf(&slices, "      - %s\n", f)
		}
	}
	writeFile(t, filepath.Join(hg, "tracks", "trk-prev0001.html"), fmt.Sprintf(`<!DOCTYPE html><html><body>
<article id="trk-prev0001" data-type="track" data-status="in-progress" data-priority="medium">
<header><h1>Preview Track</h1></header>
<nav data-graph-edges><section data-edge-type="contains"><ul>%s</ul></section></nav>
</article></body></html>`, links.String()))
	writeFile(t, filepath.Join(hg, "plans", "plan-prev0001.yaml"),
		"meta:\n  id: plan-prev0001\n  track_id: trk-prev0001\nslices:\n"+slices.String())
	return hg
}

func previewFor(t *testing.T, items []previewItem) *executePreview {
	t.Helper()
	p, err := buildExecutePreview(writePreviewFixture(t, items), "trk-prev0001")
	if err != nil {
		t.Fatalf("buildExecutePreview: %v", err)
	}
	return p
}

func TestExecutePreview_ReadySetExcludesBlocked(t *testing.T) {
	p := previewFor(t, []previewItem{
		{id: "feat-aaaa0001", status: "todo", files: []string{"a.go"}},
		{id: "feat-aaaa0002", status: "todo", blockedBy: []string{"feat-aaaa0001"}, files: []string{"b.go"}},
		{id: "feat-aaaa0003", status: "done", files: []string{"c.go"}},
		{id: "feat-aaaa0004", status: "todo", blockedBy: []string{"feat-aaaa0003"}, files: []string{"d.go"}},
	})
	want := []string{"feat-aaaa0001", "feat-aaaa0004"}
	if !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
	if len(p.Deferred) != 1 || p.Deferred[0].ItemID != "feat-aaaa0002" ||
		!strings.Contains(p.Deferred[0].Reason, "blocked by feat-aaaa0001") {
		t.Errorf("Deferred = %+v, want feat-aaaa0002 blocked by feat-aaaa0001", p.Deferred)
	}
}

func TestExecutePreview_OverlapDefersConflictingTask(t *testing.T) {
	p := previewFor(t, []previewItem{
		{id: "feat-bbbb0001", status: "todo", files: []string{"x/shared.go", "x/one.go"}},
		{id: "feat-bbbb0002", status: "todo", files: []string{"x/shared.go (new)"}},
		{id: "feat-bbbb0003", status: "todo", files: []string{"x/three.go"}},
	})
	want := []string{"feat-bbbb0001", "feat-bbbb0003"}
	if !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
	if len(p.Deferred) != 1 || p.Deferred[0].ItemID != "feat-bbbb0002" {
		t.Fatalf("Deferred = %+v, want only feat-bbbb0002", p.Deferred)
	}
	if r := p.Deferred[0].Reason; !strings.Contains(r, "feat-bbbb0001") || !strings.Contains(r, "x/shared.go") {
		t.Errorf("reason %q should name feat-bbbb0001 and x/shared.go", r)
	}
}

func TestExecutePreview_BriefPerTask(t *testing.T) {
	p := previewFor(t, []previewItem{
		{id: "feat-cccc0001", status: "todo", files: []string{"one.go"}},
		{id: "feat-cccc0002", status: "todo", files: []string{"two.go"}},
	})
	if len(p.Briefs) != 2 {
		t.Fatalf("len(Briefs) = %d, want 2", len(p.Briefs))
	}
	b := p.Briefs[0]
	if b.ItemID != "feat-cccc0001" || !reflect.DeepEqual(b.Files, []string{"one.go"}) {
		t.Errorf("brief = %+v, want id feat-cccc0001 and files [one.go]", b)
	}
	if b.FirstCommand != "wipnote feature start feat-cccc0001" {
		t.Errorf("FirstCommand = %q", b.FirstCommand)
	}
	if len(b.Siblings) != 1 || !strings.HasPrefix(b.Siblings[0], "feat-cccc0002") {
		t.Errorf("Siblings = %v, want feat-cccc0002 listed", b.Siblings)
	}
	if !strings.Contains(b.Pack, "feat-cccc0001") {
		t.Errorf("Pack should come from the context pack and name the item")
	}
}

func TestExecutePreview_TextNamesReviewRule(t *testing.T) {
	p := previewFor(t, []previewItem{{id: "feat-dddd0001", status: "todo", files: []string{"a.go"}}})
	out := renderExecutePreviewText(p)
	for _, want := range []string{
		"Ready (1)", "Code review is required", "Code review is skipped",
		"block only on a failing run or a user-visible bug", "wipnote feature start feat-dddd0001",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestExecutePreview_BlockingQuestionDefers(t *testing.T) {
	hg := writePreviewFixture(t, []previewItem{{id: "feat-eeee0001", status: "todo", files: []string{"a.go"}}})
	p, err := buildExecutePreview(hg, "trk-prev0001")
	if err != nil {
		t.Fatal(err)
	}
	p.Questions = []questionRow{{ItemID: "feat-eeee0001", QuestionID: "q1", Text: "Which?"}}
	planDispatch(hg, p)
	if len(p.Ready) != 0 || len(p.Deferred) != 1 || !strings.Contains(p.Deferred[0].Reason, "blocking question q1") {
		t.Errorf("Ready=%v Deferred=%+v, want deferral for blocking question q1", p.Ready, p.Deferred)
	}
}

func TestExecutePreview_JSONKeepsOldFieldsAndAddsNew(t *testing.T) {
	p := previewFor(t, []previewItem{{id: "feat-ffff0001", status: "todo", files: []string{"a.go"}}})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"track"`, `"features"`, `"questions"`, `"git"`, `"ready"`, `"deferred"`, `"briefs"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("json missing %s", key)
		}
	}
}

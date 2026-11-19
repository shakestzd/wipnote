package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExecutePreview_UnknownFilesDeferred(t *testing.T) {
	p := previewFor(t, []previewItem{
		{id: "feat-ffff0001", status: "todo", files: []string{"a.go"}},
		{id: "feat-ffff0002", status: "todo"},
	})
	if want := []string{"feat-ffff0001"}; !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
	if len(p.Deferred) != 1 || p.Deferred[0].ItemID != "feat-ffff0002" || p.Deferred[0].Reason != "files unknown" {
		t.Errorf("Deferred = %+v, want feat-ffff0002 with reason %q", p.Deferred, "files unknown")
	}
}

func TestExecutePreview_UnknownFilesSoleCandidateReady(t *testing.T) {
	p := previewFor(t, []previewItem{{id: "feat-ffff0003", status: "todo"}})
	if want := []string{"feat-ffff0003"}; !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
}

func TestExecutePreview_PathSpellingsCollide(t *testing.T) {
	p := previewFor(t, []previewItem{
		{id: "feat-ffff0004", status: "todo", files: []string{"x/a.go"}},
		{id: "feat-ffff0005", status: "todo", files: []string{"./x/a.go"}},
	})
	if want := []string{"feat-ffff0004"}; !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
	if len(p.Deferred) != 1 || !strings.Contains(p.Deferred[0].Reason, "x/a.go") {
		t.Errorf("Deferred = %+v, want overlap on x/a.go", p.Deferred)
	}
}

func TestExecutePreview_AbsolutePathUnderRepoCollides(t *testing.T) {
	hg := writePreviewFixture(t, []previewItem{
		{id: "feat-ffff0006", status: "todo", files: []string{"x/a.go"}},
		{id: "feat-ffff0007", status: "todo", files: []string{"PLACEHOLDER"}},
	})
	abs := filepath.Join(filepath.Dir(hg), "x", "a.go")
	planPath := filepath.Join(hg, "plans", "plan-prev0001.yaml")
	data := readFile(t, planPath)
	writeFile(t, planPath, strings.Replace(data, "PLACEHOLDER", abs, 1))
	p, err := buildExecutePreview(hg, "trk-prev0001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"feat-ffff0006"}; !reflect.DeepEqual(p.Ready, want) {
		t.Errorf("Ready = %v, want %v", p.Ready, want)
	}
}

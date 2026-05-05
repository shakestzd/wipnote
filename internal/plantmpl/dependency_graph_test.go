package plantmpl_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shakestzd/erinn/internal/plantmpl"
)

// ---------------------------------------------------------------------------
// DependencyGraph.Render — structural output
// ---------------------------------------------------------------------------

func TestDependencyGraphRenderZoneClass(t *testing.T) {
	g := &plantmpl.DependencyGraph{}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `class="dep-graph"`) {
		t.Error(`output missing class="dep-graph"`)
	}
	if !strings.Contains(html, `data-zone="dependency-graph"`) {
		t.Error(`output missing data-zone="dependency-graph"`)
	}
}

func TestDependencyGraphRenderSVGPlaceholder(t *testing.T) {
	g := &plantmpl.DependencyGraph{}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `<svg id="dep-graph-svg"`) {
		t.Error(`output missing <svg id="dep-graph-svg"`)
	}
}

func TestDependencyGraphRenderNodeAttributes(t *testing.T) {
	g := &plantmpl.DependencyGraph{
		Nodes: []plantmpl.GraphNode{
			{Num: 1, Name: "Feature name", Status: "pending", Deps: "2,3", Files: 5},
		},
	}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `data-node="1"`) {
		t.Error(`output missing data-node="1"`)
	}
	if !strings.Contains(html, `data-name="Feature name"`) {
		t.Error(`output missing data-name="Feature name"`)
	}
	if !strings.Contains(html, `data-status="pending"`) {
		t.Error(`output missing data-status="pending"`)
	}
	if !strings.Contains(html, `data-deps="2,3"`) {
		t.Error(`output missing data-deps="2,3"`)
	}
	if !strings.Contains(html, `data-files="5"`) {
		t.Error(`output missing data-files="5"`)
	}
}

func TestDependencyGraphRenderEmptyNodesSection(t *testing.T) {
	g := &plantmpl.DependencyGraph{Nodes: nil}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `id="graph-data"`) {
		t.Error(`output missing id="graph-data" div`)
	}
	// No data-node elements should be present.
	if strings.Contains(html, `data-node=`) {
		t.Error("empty Nodes should not produce data-node elements")
	}
}

func TestDependencyGraphRenderFilesOmittedWhenZero(t *testing.T) {
	g := &plantmpl.DependencyGraph{
		Nodes: []plantmpl.GraphNode{
			{Num: 2, Name: "No Files", Status: "approved", Deps: "", Files: 0},
		},
	}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if strings.Contains(html, `data-files=`) {
		t.Error("data-files attribute should not render when Files is 0")
	}
}

func TestDependencyGraphRenderDefaultStatusPending(t *testing.T) {
	g := &plantmpl.DependencyGraph{
		Nodes: []plantmpl.GraphNode{
			{Num: 3, Name: "No Status"},
		},
	}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, `data-status="pending"`) {
		t.Error("empty Status should default to pending in data-status attribute")
	}
}

func TestDependencyGraphRenderMultipleNodes(t *testing.T) {
	g := &plantmpl.DependencyGraph{
		Nodes: []plantmpl.GraphNode{
			{Num: 1, Name: "Alpha", Status: "approved", Deps: ""},
			{Num: 2, Name: "Beta", Status: "pending", Deps: "1"},
			{Num: 3, Name: "Gamma", Status: "", Deps: "1,2", Files: 3},
		},
	}

	var buf bytes.Buffer
	if err := g.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	html := buf.String()
	for _, want := range []string{
		`data-node="1"`, `data-name="Alpha"`, `data-status="approved"`,
		`data-node="2"`, `data-name="Beta"`, `data-deps="1"`,
		`data-node="3"`, `data-name="Gamma"`, `data-deps="1,2"`, `data-files="3"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
	// Gamma has empty Status — should default to pending.
	if !strings.Contains(html, `data-status="pending"`) {
		t.Error(`Gamma node with empty Status should render data-status="pending"`)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/orchlink"
)

// These tests dispatch through runLineageProjection, the path a real
// `wipnote lineage feat-XXX` runs, against an orch registry and event logs in
// orch's own file shapes (docs/design/orch-integration.md).

type orchFixture struct {
	wipnoteDir string
	orchDir    string
	registry   string
	feature    string
	session    string // a session the feature was implemented in
}

func newOrchFixture(t *testing.T) orchFixture {
	t.Helper()
	f := orchFixture{wipnoteDir: newLineageProjectionWipnoteDir(t), orchDir: t.TempDir()}
	p, err := workitem.Open(f.wipnoteDir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	feat, err := p.Features.Create("Served by orch", workitem.FeatWithStatus("in-progress"))
	if err != nil {
		t.Fatal(err)
	}
	f.feature, f.session = feat.ID, "0b7c2f1e-1111-4222-8333-944445555666"
	if _, err := p.Features.AddEdge(feat.ID, models.Edge{TargetID: f.session, Relationship: models.RelImplementedIn}); err != nil {
		t.Fatal(err)
	}
	f.registry = filepath.Join(f.orchDir, "registry.json")
	t.Setenv(orchlink.EnvRegistry, f.registry)
	return f
}

// mailbox writes one event per sid into <orchDir>/mail/<name>/events.ndjson.
func (f orchFixture) mailbox(t *testing.T, name string, sids ...string) string {
	t.Helper()
	dir := filepath.Join(f.orchDir, "mail", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, sid := range sids {
		line, _ := json.Marshal(map[string]any{"v": 1, "seq": i + 1, "ts": 1.0, "session": name, "kind": "prompt", "sid": sid})
		b.Write(append(line, '\n'))
	}
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (f orchFixture) writeRegistry(t *testing.T, sessions map[string]map[string]any) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"v": 1, "sessions": sessions})
	if err := os.WriteFile(f.registry, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func lineageJSONFor(t *testing.T, f orchFixture) (lineageJSON, map[string]json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := runLineageProjection(&buf, f.wipnoteDir, f.feature, lineageOpts{depth: 5, jsonOut: true}); err != nil {
		t.Fatalf("runLineageProjection: %v", err)
	}
	var out lineageJSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(buf.Bytes(), &raw)
	return out, raw
}

func lineageTextFor(t *testing.T, f orchFixture) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runLineageProjection(&buf, f.wipnoteDir, f.feature, lineageOpts{depth: 5}); err != nil {
		t.Fatalf("runLineageProjection: %v", err)
	}
	return buf.String()
}

func TestLineageShowsOrchSessionsServingTheWorkItem(t *testing.T) {
	f := newOrchFixture(t)
	f.writeRegistry(t, map[string]map[string]any{
		"tagged":  {"kind": "claude", "state": "idle", "created_at": 1, "work_item": f.feature, "mailbox": f.mailbox(t, "tagged", "sid-x")},
		"derived": {"kind": "claude", "state": "working", "created_at": 2, "container": true, "mailbox": f.mailbox(t, "derived", f.session)},
		"elsewhere": {"kind": "claude", "state": "idle", "created_at": 3, "work_item": "feat-ffffffff",
			"mailbox": f.mailbox(t, "elsewhere", f.session)},
		"unrelated": {"kind": "codex", "state": "idle", "created_at": 4, "mailbox": f.mailbox(t, "unrelated", "sid-y")},
	})
	out, _ := lineageJSONFor(t, f)
	links := map[string]string{}
	for _, s := range out.OrchSessions {
		links[s.Name] = s.Link
	}
	if len(links) != 2 || links["tagged"] != orchlink.LinkTag || links["derived"] != orchlink.LinkSID {
		t.Fatalf("want tagged (tag) and derived (sid) only, got %v", links)
	}
	if len(out.Forward) == 0 {
		t.Error("the graph walk must still be present next to the orch section")
	}
	text := lineageTextFor(t, f)
	for _, want := range []string{"orch sessions (2)", "tagged", "derived: session id", f.session} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"elsewhere", "unrelated", f.orchDir} {
		if strings.Contains(text, unwanted) {
			t.Errorf("text output must not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestLineageWithoutOrchIsUnchanged(t *testing.T) {
	f := newOrchFixture(t) // registry path set but no file: orch not in use
	_, raw := lineageJSONFor(t, f)
	if _, ok := raw["orch_sessions"]; ok {
		t.Error("orch_sessions must be absent when orch is not in use")
	}
	if _, ok := raw["orch_error"]; ok {
		t.Error("orch_error must be absent when orch is not in use")
	}
	if text := lineageTextFor(t, f); strings.Contains(text, "orch sessions") {
		t.Errorf("text must not mention orch when it is not in use:\n%s", text)
	}
	// A registry with sessions that serve other work items also adds nothing.
	f.writeRegistry(t, map[string]map[string]any{
		"w1": {"kind": "claude", "state": "idle", "created_at": 1, "work_item": "feat-ffffffff", "mailbox": f.mailbox(t, "w1", "sid-z")},
	})
	if _, raw := lineageJSONFor(t, f); raw["orch_sessions"] != nil {
		t.Error("orch_sessions must be absent when no orch session serves the work item")
	}
}

func TestUnreadableOrchRegistryIsReportedNotHidden(t *testing.T) {
	f := newOrchFixture(t)
	if err := os.WriteFile(f.registry, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := lineageJSONFor(t, f)
	if out.OrchError == "" || len(out.OrchSessions) != 0 {
		t.Errorf("want orch_error and no sessions, got %+v / %q", out.OrchSessions, out.OrchError)
	}
	if len(out.Forward) == 0 {
		t.Error("a broken orch registry must not break the rest of lineage")
	}
	if text := lineageTextFor(t, f); !strings.Contains(text, "orch sessions: unreadable") {
		t.Errorf("text must say the registry is unreadable:\n%s", text)
	}
}

// orch's files have one writer, orch. Reading lineage must leave them, and the
// .wipnote store, byte-for-byte as they were.
func TestLineageNeverWritesOrchFilesOrWipnoteStore(t *testing.T) {
	f := newOrchFixture(t)
	f.writeRegistry(t, map[string]map[string]any{
		"w1": {"kind": "claude", "state": "idle", "created_at": 1, "work_item": f.feature, "mailbox": f.mailbox(t, "w1", f.session)},
	})
	before := snapshotTree(t, f.orchDir)
	wipBefore := snapshotTree(t, f.wipnoteDir)
	_ = lineageTextFor(t, f)
	_, _ = lineageJSONFor(t, f)
	if after := snapshotTree(t, f.orchDir); !equalTrees(before, after) {
		t.Error("lineage changed orch's files")
	}
	if after := snapshotTree(t, f.wipnoteDir); !equalTrees(wipBefore, after) {
		t.Error("lineage wrote into .wipnote while reading orch")
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(path)
		out[path] = info.ModTime().String() + "|" + string(data)
		return nil
	})
	return out
}

func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestDirectSessionsTakesOnlyTheRootsOwnImplementedInEdges(t *testing.T) {
	nodes := []lineageNode{
		{ID: "s-own", EdgeType: "implemented_in", Parent: "feat-aaaaaaaa"},
		{ID: "s-related", EdgeType: "implemented_in", Parent: "feat-bbbbbbbb"},
		{ID: "trk-cccccccc", EdgeType: "part_of", Parent: "feat-aaaaaaaa"},
	}
	got := directSessions("feat-aaaaaaaa", nodes)
	if len(got) != 1 || !got["s-own"] {
		t.Errorf("want only s-own, got %v", got)
	}
}

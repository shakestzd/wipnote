package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/models"
)

func completeRecord() *Record {
	return &Record{
		Question: "How should header toolbar actions overflow?",
		Sources: []Source{
			{URL: "https://primer.style/components/action-bar", Type: SourcePrimary, Retrieved: "2026-10-21"},
		},
		Exists:   "none found",
		Queries:  []string{"wipnote relevant overflow", "rg -i 'more actions'"},
		Options:  []string{"hamburger menu", "priority overflow into More actions"},
		Chosen:   "priority overflow into More actions",
		Rejected: []Rejected{{Option: "hamburger menu", Reason: "NN/g data concerns site navigation, not actions"}},
	}
}

func TestFromNode_NoPropertyIsNil(t *testing.T) {
	if FromNode(&models.Node{ID: "feat-00000001"}) != nil {
		t.Fatal("node without research property must yield nil record")
	}
	if FromNode(nil) != nil {
		t.Fatal("nil node must yield nil record")
	}
}

func TestRecordRoundTripThroughProperty(t *testing.T) {
	rec := completeRecord()
	n := &models.Node{ID: "feat-00000001", Properties: map[string]any{PropRecord: rec.Property()}}
	got := FromNode(n)
	if got == nil || got.Chosen != rec.Chosen || len(got.Sources) != 1 || got.Rejected[0].Reason == "" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestFromNode_LegacyEvidenceNote(t *testing.T) {
	n := &models.Node{ID: "feat-00000001", Content: "<p>2026-01-01: research-evidence: https://pkg.go.dev/x | waiver: stdlib only</p>"}
	got := FromNode(n)
	if got == nil || len(got.Sources) != 1 || got.Sources[0].URL != "https://pkg.go.dev/x" || got.Waiver != "stdlib only" {
		t.Fatalf("legacy note not read: %+v", got)
	}
}

func TestMergeAppendsListsAndOverwritesScalars(t *testing.T) {
	dst := &Record{Question: "q1", Sources: []Source{{URL: "https://a.example"}}}
	src := &Record{Chosen: "x", Sources: []Source{{URL: "https://a.example"}, {URL: "https://b.example"}}}
	out := Merge(dst, src)
	if out.Question != "q1" || out.Chosen != "x" || len(out.Sources) != 2 {
		t.Fatalf("merge wrong: %+v", out)
	}
}

func TestAssess(t *testing.T) {
	if missing, _ := Assess(completeRecord()); len(missing) != 0 {
		t.Fatalf("complete record reported missing %v", missing)
	}
	missing, _ := Assess(&Record{Question: "q"})
	joined := strings.Join(missing, ",")
	for _, want := range []string{"source", "exists", "decision"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, missing)
		}
	}
	noOptions := completeRecord()
	noOptions.Options = nil
	if m, _ := Assess(noOptions); len(m) == 0 || !strings.Contains(strings.Join(m, ","), "options") {
		t.Errorf("a chosen option with no options considered must not pass, got %v", m)
	}
	noQueries := completeRecord()
	noQueries.Queries = nil
	if m, _ := Assess(noQueries); len(m) == 0 {
		t.Error(`"none found" without queries must not satisfy exists`)
	}
	secondary := completeRecord()
	secondary.Sources[0].Type = SourceSecondary
	if _, warns := Assess(secondary); len(warns) == 0 {
		t.Error("all-secondary sources must warn")
	}
	if m, _ := Assess(&Record{Waiver: "trivial"}); len(m) != 0 {
		t.Error("waiver must satisfy the record")
	}
}

func TestInferKind(t *testing.T) {
	cases := []struct{ typ, title, want string }{
		{"feature", "Rename session column", KindRename},
		{"feature", "Docs: explain the gate", KindDocs},
		{"feature", "Header toolbar overflow menu", KindUI},
		{"feature", "Bump go-git to v6", KindDependency},
		{"feature", "Add research gate", KindCapability},
		{"bug", "Crash on empty title", KindBugfix},
		{"bug", "Fix crash when dashboard loads", KindBugfix}, // bugs never pick up ui/design
		{"bug", "Fix API timeout", KindBugfix},
		{"feature", "Add doc search capability", KindCapability}, // "doc" is not "docs"
		{"spike", "Investigate overflow", ""},
		{"track", "Dashboard redesign", ""},
		{"spec", "API schema", ""},
	}
	for _, c := range cases {
		if got, _ := InferKind(c.typ, c.title); got != c.want {
			t.Errorf("InferKind(%q,%q)=%q want %q", c.typ, c.title, got, c.want)
		}
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(root, ".wipnote", "config.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	cfg := LoadConfig(writeConfig(t, ""))
	if cfg.Mode != ModeWarn || !cfg.Gated(KindUI) || cfg.Gated(KindBugfix) || cfg.Gated("") {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
	cfg = LoadConfig(writeConfig(t, `{"research":{"mode":"block","required_kinds":["docs"]}}`))
	if cfg.Mode != ModeBlock || !cfg.Gated(KindDocs) || cfg.Gated(KindUI) {
		t.Fatalf("overrides wrong: %+v", cfg)
	}
	if LoadConfig(writeConfig(t, `{"research":{"mode":"bogus"}}`)).Mode != ModeWarn {
		t.Fatal("unknown mode must fall back to warn")
	}
}

func gatedNode(rec *Record) *models.Node {
	n := &models.Node{ID: "feat-00000001", Type: "feature", Properties: map[string]any{PropKind: KindUI}}
	if rec != nil {
		n.Properties[PropRecord] = rec.Property()
	}
	return n
}

func TestCheckStartAndDispatch(t *testing.T) {
	warn := Config{Mode: ModeWarn, RequiredKinds: DefaultRequiredKinds}
	block := Config{Mode: ModeBlock, RequiredKinds: DefaultRequiredKinds}
	off := Config{Mode: ModeOff, RequiredKinds: DefaultRequiredKinds}

	if msg := CheckStart(warn, gatedNode(nil)); !strings.Contains(msg, "wipnote research start") {
		t.Errorf("start warning must carry the fix command, got %q", msg)
	}
	if CheckStart(off, gatedNode(nil)) != "" || CheckStart(warn, gatedNode(completeRecord())) != "" {
		t.Error("start must be silent when off or when a record exists")
	}
	if msg, deny := CheckDispatch(warn, gatedNode(nil)); msg == "" || deny {
		t.Errorf("warn dispatch must advise, not deny: %q %v", msg, deny)
	}
	if msg, deny := CheckDispatch(block, gatedNode(nil)); msg == "" || !deny {
		t.Errorf("block dispatch must deny: %q %v", msg, deny)
	}
	if _, deny := CheckDispatch(block, gatedNode(&Record{Waiver: "x"})); deny {
		t.Error("waived item must be dispatchable")
	}
}

func TestCheckComplete(t *testing.T) {
	block := Config{Mode: ModeBlock, RequiredKinds: DefaultRequiredKinds}
	warn := Config{Mode: ModeWarn, RequiredKinds: DefaultRequiredKinds}
	if _, err := CheckComplete(block, gatedNode(nil)); err == nil {
		t.Error("block mode must fail without a record")
	}
	if _, err := CheckComplete(block, gatedNode(completeRecord())); err != nil {
		t.Errorf("complete record must pass: %v", err)
	}
	warns, err := CheckComplete(warn, gatedNode(nil))
	if err != nil || len(warns) == 0 {
		t.Errorf("warn mode must warn not fail: %v %v", warns, err)
	}
	ungated := &models.Node{ID: "bug-00000001", Type: "bug", Properties: map[string]any{PropKind: KindBugfix}}
	if w, err := CheckComplete(block, ungated); err != nil || len(w) != 0 {
		t.Error("ungated item must pass silently")
	}
	legacy := &models.Node{ID: "feat-00000002", Type: "feature"}
	if w, err := CheckComplete(block, legacy); err != nil || len(w) != 0 {
		t.Error("legacy item without change_kind must not be gated")
	}
}

func TestLoadConfigErrorAliasAndNotices(t *testing.T) {
	if cfg := LoadConfig(writeConfig(t, `{"research":{"mode":"error"}}`)); cfg.Mode != ModeBlock {
		t.Fatalf("ESLint-style \"error\" must mean block, got %q", cfg.Mode)
	}
	cfg := LoadConfig(writeConfig(t, `{"research":{"mode":"strict","required_kinds":["ui","bogus"]}}`))
	if cfg.Mode != ModeWarn || !strings.Contains(cfg.Notice, "strict") || !strings.Contains(cfg.Notice, "bogus") || !cfg.Gated(KindUI) {
		t.Fatalf("unknown values must fall back and be reported: %+v", cfg)
	}
	warns, _ := CheckComplete(cfg, gatedNode(completeRecord()))
	if len(warns) == 0 || !strings.Contains(warns[0], "strict") {
		t.Fatalf("config notice must surface at check time: %v", warns)
	}
}

func TestStubRecordDoesNotSatisfyDispatch(t *testing.T) {
	block := Config{Mode: ModeBlock, RequiredKinds: DefaultRequiredKinds}
	stub := &Record{Question: "?"}
	if msg, deny := CheckDispatch(block, gatedNode(stub)); !deny || !strings.Contains(msg, "source") {
		t.Fatalf("a question-only record must not pass dispatch: deny=%v msg=%q", deny, msg)
	}
	if CheckStart(block, gatedNode(stub)) == "" {
		t.Fatal("a question-only record must still warn at start")
	}
}

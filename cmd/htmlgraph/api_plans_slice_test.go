package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/htmlgraph/internal/db"
	"github.com/shakestzd/htmlgraph/internal/planyaml"
	"github.com/shakestzd/htmlgraph/internal/workitem"
)

// setupSlicePlanFixture builds a temp htmlgraph dir layout suitable for the
// slice-level review endpoints. The DB lives at <tmp>/htmlgraph.db and
// HTMLGRAPH_DB_PATH is set so that runApproveSlice / runRejectSlice / etc.
// (which call openPlanDB → storage.CanonicalDBPath) resolve to the same DB
// the test set up. A track row is also inserted so promoteSliceFromYAML can
// resolve the plan's track_id.
//
// fixture.htmlgraphDir is the path to pass to the handlers and helpers.
// fixture.db is opened against the same canonical path so test setup writes
// are visible to the run* helpers.
type sliceTestFixture struct {
	t            *testing.T
	htmlgraphDir string
	planID       string
	trackID      string
	db           *sql.DB
}

func newSliceTestFixture(t *testing.T) *sliceTestFixture {
	t.Helper()
	// Use a single directory both as the .htmlgraph project dir AND the
	// workitem.Open project dir — matches the convention used by
	// plan_promote_slice_test.go. promoteSliceFromYAML opens the project at
	// htmlgraphDir, so the tracks/, features/, plans/ directories must live
	// directly under it.
	htmlgraphDir := t.TempDir()
	for _, sub := range []string{"plans", "features", "tracks", "bugs", "spikes", "sessions"} {
		if err := os.MkdirAll(filepath.Join(htmlgraphDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	// Force openPlanDB / workitem.Open / api handlers to share a single DB
	// at a known temp file by pinning HTMLGRAPH_DB_PATH. CanonicalDBPath
	// short-circuits to this value, so all three paths resolve identically.
	dbPath := filepath.Join(htmlgraphDir, "test.db")
	t.Setenv("HTMLGRAPH_DB_PATH", dbPath)

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Create a real track via workitem.Open so promote can resolve it.
	p, err := workitem.Open(htmlgraphDir, "test-agent")
	if err != nil {
		t.Fatalf("workitem.Open: %v", err)
	}
	track, err := p.Tracks.Create("Slice Track")
	if err != nil {
		p.Close()
		t.Fatalf("create track: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("close project: %v", err)
	}

	planID := "plan-slice-test"
	if _, err := database.Exec(
		`INSERT INTO features (id, type, title, status) VALUES (?, 'plan', 'Slice Plan', 'in-progress')`,
		planID,
	); err != nil {
		t.Fatalf("insert plan feature: %v", err)
	}

	return &sliceTestFixture{
		t:            t,
		htmlgraphDir: htmlgraphDir,
		planID:       planID,
		trackID:      track.ID,
		db:           database,
	}
}

// writePlanYAML saves a plan YAML at the canonical path.
func (f *sliceTestFixture) writePlanYAML(plan *planyaml.PlanYAML) {
	f.t.Helper()
	yamlPath := filepath.Join(f.htmlgraphDir, "plans", f.planID+".yaml")
	if err := planyaml.Save(yamlPath, plan); err != nil {
		f.t.Fatalf("save plan yaml: %v", err)
	}
}

// fixturePlan returns a minimal but complete plan with two slices: slice 1
// has no deps; slice 2 depends on slice 1. Slice 1 has one open question.
func (f *sliceTestFixture) fixturePlan() *planyaml.PlanYAML {
	plan := planyaml.NewPlan(f.planID, "Slice Plan", "fixture")
	plan.Meta.TrackID = f.trackID
	plan.Slices = []planyaml.PlanSlice{
		{
			ID:    "slice-one",
			Num:   1,
			Title: "First Slice",
			What:  "do the first thing",
			Questions: []planyaml.SliceQuestion{
				{ID: "q1", Text: "first?"},
			},
		},
		{
			ID:    "slice-two",
			Num:   2,
			Title: "Second Slice",
			What:  "do the second thing",
			Deps:  []int{1},
		},
	}
	return plan
}

// reloadPlanYAML reads the plan YAML back from disk.
func (f *sliceTestFixture) reloadPlanYAML() *planyaml.PlanYAML {
	f.t.Helper()
	yamlPath := filepath.Join(f.htmlgraphDir, "plans", f.planID+".yaml")
	plan, err := planyaml.Load(yamlPath)
	if err != nil {
		f.t.Fatalf("reload plan yaml: %v", err)
	}
	return plan
}

func (f *sliceTestFixture) router() http.HandlerFunc {
	return planRouter(f.db, f.htmlgraphDir)
}

// doRequest executes a request against the plan router and returns the
// recorder for assertions.
func (f *sliceTestFixture) doRequest(method, path string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			f.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.router()(w, req)
	return w
}

// ---- GET /api/plans/{id} ----------------------------------------------------

func TestPlanGetReturnsValidJSON(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodGet, "/api/plans/"+f.planID, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"meta", "design", "slices", "questions"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing key %q (got keys: %v)", key, mapKeys(resp))
		}
	}

	var slices []sliceJSONItem
	if err := json.Unmarshal(resp["slices"], &slices); err != nil {
		t.Fatalf("decode slices: %v", err)
	}
	if len(slices) != 2 {
		t.Fatalf("slices count: got %d, want 2", len(slices))
	}
	s1 := slices[0]
	if s1.Num != 1 {
		t.Errorf("slice[0].num: got %d, want 1", s1.Num)
	}
	if s1.Title != "First Slice" {
		t.Errorf("slice[0].title: got %q, want %q", s1.Title, "First Slice")
	}
	if !s1.HasUnansweredQuestions {
		t.Error("slice[0].has_unanswered_questions: expected true (one open question)")
	}

	s2 := slices[1]
	if s2.Num != 2 || len(s2.Deps) != 1 || s2.Deps[0] != 1 {
		t.Errorf("slice[1]: unexpected: %+v", s2)
	}
	if s2.HasUnansweredQuestions {
		t.Error("slice[1].has_unanswered_questions: expected false (no questions)")
	}
}

func TestPlanGetNotFound(t *testing.T) {
	f := newSliceTestFixture(t)
	// no YAML written
	w := f.doRequest(http.MethodGet, "/api/plans/"+f.planID, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// ---- GET /api/plans/{id}/slices --------------------------------------------

func TestPlanSlicesEndpoint(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodGet, "/api/plans/"+f.planID+"/slices", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var slices []sliceJSONItem
	if err := json.Unmarshal(w.Body.Bytes(), &slices); err != nil {
		t.Fatalf("decode slices array: %v", err)
	}
	if len(slices) != 2 {
		t.Fatalf("slices count: got %d, want 2", len(slices))
	}
	if slices[0].Num != 1 || slices[0].Title != "First Slice" {
		t.Errorf("slice[0]: got %+v", slices[0])
	}
}

// ---- POST /api/plans/{id}/slice/{n}/approve --------------------------------

func TestSliceApproveEndpoint(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/approve", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Subsequent GET reflects approved state.
	w2 := f.doRequest(http.MethodGet, "/api/plans/"+f.planID, nil)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var slices []sliceJSONItem
	if err := json.Unmarshal(resp["slices"], &slices); err != nil {
		t.Fatalf("decode slices: %v", err)
	}
	if slices[0].ApprovalStatus != "approved" {
		t.Errorf("slice[0].approval_status: got %q, want approved", slices[0].ApprovalStatus)
	}
}

func TestSliceApproveIdempotent(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	for i := 0; i < 2; i++ {
		w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/approve", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("approve iter %d: got %d, want 200; body: %s", i, w.Code, w.Body.String())
		}
	}

	plan := f.reloadPlanYAML()
	if plan.Slices[0].ApprovalStatus != "approved" {
		t.Errorf("yaml approval_status: got %q, want approved", plan.Slices[0].ApprovalStatus)
	}
}

// ---- POST /api/plans/{id}/slice/{n}/reject ---------------------------------

func TestSliceRejectWithChangesRequested(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/reject",
		map[string]any{"changes_requested": true})
	if w.Code != http.StatusOK {
		t.Fatalf("reject status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	plan := f.reloadPlanYAML()
	if got := plan.Slices[0].ApprovalStatus; got != "changes_requested" {
		t.Errorf("yaml approval_status: got %q, want changes_requested", got)
	}
}

// ---- POST /api/plans/{id}/slice/{n}/promote --------------------------------

func TestSlicePromoteSuccess(t *testing.T) {
	f := newSliceTestFixture(t)
	plan := f.fixturePlan()
	// pre-approve slice 1 in YAML so promoteSliceFromYAML's check passes
	plan.Slices[0].ApprovalStatus = "approved"
	f.writePlanYAML(plan)

	// Also approve via plan_feedback so GetSliceApprovals sees it (defensive).
	if err := db.StorePlanFeedback(f.db, f.planID, "slice-1", "approve", "true", ""); err != nil {
		t.Fatalf("seed approval: %v", err)
	}

	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/promote", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("promote status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	featID, _ := resp["feature_id"].(string)
	if !strings.HasPrefix(featID, "feat-") {
		t.Errorf("feature_id: got %q, want feat-* prefix", featID)
	}
}

func TestSlicePromoteBlockedByDeps(t *testing.T) {
	f := newSliceTestFixture(t)
	plan := f.fixturePlan()
	// approve slice 2 (which depends on slice 1) but slice 1 is not done
	plan.Slices[1].ApprovalStatus = "approved"
	f.writePlanYAML(plan)
	if err := db.StorePlanFeedback(f.db, f.planID, "slice-2", "approve", "true", ""); err != nil {
		t.Fatalf("seed approval: %v", err)
	}

	// Without waive_deps → expect 4xx
	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/2/promote", nil)
	if w.Code < 400 || w.Code >= 500 {
		t.Errorf("expected 4xx without waive_deps; got %d; body: %s", w.Code, w.Body.String())
	}

	// With waive_deps → 200
	w2 := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/2/promote",
		map[string]any{"waive_deps": true})
	if w2.Code != http.StatusOK {
		t.Errorf("with waive_deps: got %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
}

// ---- POST /api/plans/{id}/slice/{n}/status ---------------------------------

func TestSliceStatusEndpoint(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/status",
		map[string]any{"execution_status": "in_progress"})
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Invalid status → 400
	w2 := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/status",
		map[string]any{"execution_status": "bogus"})
	if w2.Code != http.StatusBadRequest {
		t.Errorf("invalid status: got %d, want 400; body: %s", w2.Code, w2.Body.String())
	}

	// Round-trip: GET /slices must reflect the new execution_status.
	// runSetSliceStatus writes only to plan_feedback, not the YAML mirror,
	// so the read path must overlay plan_feedback values.
	g := f.doRequest(http.MethodGet, "/api/plans/"+f.planID+"/slices", nil)
	if g.Code != http.StatusOK {
		t.Fatalf("GET /slices: got %d, want 200", g.Code)
	}
	var slices []sliceJSONItem
	if err := json.Unmarshal(g.Body.Bytes(), &slices); err != nil {
		t.Fatalf("decode slices: %v", err)
	}
	var got string
	for _, s := range slices {
		if s.Num == 1 {
			got = s.ExecutionStatus
			break
		}
	}
	if got != "in_progress" {
		t.Errorf("slice 1 execution_status after status POST: got %q, want %q", got, "in_progress")
	}
}

// ---- POST /api/plans/{id}/slice/{n}/answer ---------------------------------

func TestSliceAnswerQuestion(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	w := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/answer",
		map[string]any{"question_id": "q1", "answer_key": "yes"})
	if w.Code != http.StatusOK {
		t.Fatalf("answer status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	// Missing fields → 400
	w2 := f.doRequest(http.MethodPost, "/api/plans/"+f.planID+"/slice/1/answer",
		map[string]any{"question_id": "q1"})
	if w2.Code != http.StatusBadRequest {
		t.Errorf("missing answer_key: got %d, want 400", w2.Code)
	}
}

// ---- 404 / parse errors -----------------------------------------------------

func TestSliceNotFound404(t *testing.T) {
	f := newSliceTestFixture(t)
	f.writePlanYAML(f.fixturePlan())

	cases := []struct {
		name string
		path string
	}{
		{"approve", "/api/plans/" + f.planID + "/slice/999/approve"},
		{"reject", "/api/plans/" + f.planID + "/slice/999/reject"},
		{"promote", "/api/plans/" + f.planID + "/slice/999/promote"},
		{"status", "/api/plans/" + f.planID + "/slice/999/status"},
		{"answer", "/api/plans/" + f.planID + "/slice/999/answer"},
	}
	for _, tc := range cases {
		var body any
		switch tc.name {
		case "status":
			body = map[string]any{"execution_status": "in_progress"}
		case "answer":
			body = map[string]any{"question_id": "q", "answer_key": "y"}
		}
		w := f.doRequest(http.MethodPost, tc.path, body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404; body: %s", tc.name, w.Code, w.Body.String())
		}
	}
}

// ---- parseSlicePath (helper) ------------------------------------------------

func TestParseSlicePath(t *testing.T) {
	cases := []struct {
		path     string
		wantPlan string
		wantNum  int
		wantAct  string
		wantErr  bool
	}{
		{"/api/plans/plan-abc/slice/3/approve", "plan-abc", 3, "approve", false},
		{"/api/plans/plan-abc/slice/12/promote", "plan-abc", 12, "promote", false},
		{"/api/plans/plan-abc/slice/0/approve", "", 0, "", true},
		{"/api/plans/plan-abc/slice/abc/approve", "", 0, "", true},
		{"/api/plans/plan-abc/slice/3", "", 0, "", true},
		{"/api/plans//slice/1/approve", "", 0, "", true},
	}
	for _, tc := range cases {
		gp, gn, ga, err := parseSlicePath(tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSlicePath(%q): expected error, got (%q, %d, %q)", tc.path, gp, gn, ga)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSlicePath(%q): unexpected error: %v", tc.path, err)
			continue
		}
		if gp != tc.wantPlan || gn != tc.wantNum || ga != tc.wantAct {
			t.Errorf("parseSlicePath(%q) = (%q, %d, %q), want (%q, %d, %q)",
				tc.path, gp, gn, ga, tc.wantPlan, tc.wantNum, tc.wantAct)
		}
	}
}

// helper for diagnostics
func mapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

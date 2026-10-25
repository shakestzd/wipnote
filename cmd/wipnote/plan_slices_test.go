package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPlanSlices_ReportsApprovalState(t *testing.T) {
	dir, pID := setupReusePlan(t)
	if err := runRejectSlice(dir, pID, "2", true); err != nil {
		t.Fatalf("reject-slice: %v", err)
	}

	var buf bytes.Buffer
	if err := runPlanSlices(&buf, dir, pID); err != nil {
		t.Fatalf("runPlanSlices: %v", err)
	}
	var got planSlicesResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	if got.PlanID != pID || len(got.Slices) != 2 {
		t.Fatalf("want plan %s with 2 slices, got %+v", pID, got)
	}
	// Slice 1 has only the legacy approved flag; slice 2 was sent back.
	want := map[int]string{1: "approved", 2: "changes_requested"}
	for _, s := range got.Slices {
		if s.ApprovalStatus != want[s.Num] {
			t.Errorf("slice %d: approval_status = %q, want %q", s.Num, s.ApprovalStatus, want[s.Num])
		}
		if s.Complexity != "standard" {
			t.Errorf("slice %d: complexity = %q, want standard", s.Num, s.Complexity)
		}
	}
}

func TestPlanSlices_UnknownPlan(t *testing.T) {
	dir, _ := setupReusePlan(t)
	if err := runPlanSlices(&bytes.Buffer{}, dir, "plan-missing"); err == nil {
		t.Fatal("want an error for a missing plan")
	}
}

func TestPlanSlices_HTMLOnlyLegacyPlan(t *testing.T) {
	dir, _ := setupReusePlan(t)
	html := `<html><body><article id="plan-0ld0ld01" data-type="plan" data-status="finalized"><header><h1>Plan: Legacy HTML plan</h1></header></article></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "plans", "plan-0ld0ld01.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runPlanSlices(&buf, dir, "plan-0ld0ld01"); err != nil {
		t.Fatalf("runPlanSlices: %v", err)
	}
	var got planSlicesResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	if got.PlanID != "plan-0ld0ld01" || got.Title != "Legacy HTML plan" || got.Status != "finalized" {
		t.Errorf("legacy plan = %+v", got)
	}
	if got.Slices == nil || len(got.Slices) != 0 || got.Source != "html" {
		t.Errorf("legacy plan: want empty slices from source html, got %+v", got)
	}
}

func TestPlanSlices_DetailsAddWrittenFields(t *testing.T) {
	dir, pID := setupReusePlan(t)
	var plain, detailed bytes.Buffer
	if err := runPlanSlices(&plain, dir, pID); err != nil {
		t.Fatal(err)
	}
	if err := runPlanSlicesWith(&detailed, dir, pID, true); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain.Bytes(), []byte(`"what"`)) {
		t.Error("plain output should not carry slice details")
	}
	var got planSlicesResult
	if err := json.Unmarshal(detailed.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	s := got.Slices[1]
	if s.What != "do thing 2" || len(s.Deps) != 1 || s.Deps[0] != 1 {
		t.Errorf("slice 2 details = %+v", s)
	}
}

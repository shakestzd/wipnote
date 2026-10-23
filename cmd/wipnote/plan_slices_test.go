package main

import (
	"bytes"
	"encoding/json"
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

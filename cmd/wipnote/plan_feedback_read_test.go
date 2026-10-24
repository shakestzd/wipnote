package main

import (
	"testing"

	dbpkg "github.com/shakestzd/wipnote/core/db"
)

func fb(section, action, value string) dbpkg.PlanFeedback {
	return dbpkg.PlanFeedback{Section: section, Action: action, Value: value}
}

// A section that has only a comment counts as an undecided section, as it did
// in the former dashboard feedback API: the plan stays draft and the section
// reads as not approved.
func TestFeedbackOutput_CommentOnlySectionKeepsDraft(t *testing.T) {
	out := feedbackOutputFromEntries("plan-x", "draft", []dbpkg.PlanFeedback{
		fb("slice-1", "approve", "approved"),
		fb("slice-2", "comment", "split this slice"),
	})
	if out.Status != "draft" {
		t.Fatalf("status = %q, want draft while slice-2 is undecided", out.Status)
	}
	if approved, ok := out.SectionApprovals["slice-2"]; !ok || approved {
		t.Fatalf("section approvals = %v, want slice-2 present and false", out.SectionApprovals)
	}
	if approved, ok := out.SliceApprovals["slice-2"]; !ok || approved {
		t.Fatalf("slice approvals = %v, want slice-2 present and false", out.SliceApprovals)
	}
	if out.Comments["slice-2"] != "split this slice" {
		t.Fatalf("comments = %v", out.Comments)
	}
}

// Every decided section approved, with no undecided comment-only section,
// reads approved while the plan is still in draft or review.
func TestFeedbackOutput_AllApprovedReadsApproved(t *testing.T) {
	entries := []dbpkg.PlanFeedback{
		fb("slice-1", "approve", "approved"),
		fb("slice-1", "comment", "looks good"),
		fb("chat", "comment", "ignored"),
	}
	for _, meta := range []string{"", "draft", "review", "todo", "active", "in-progress"} {
		if out := feedbackOutputFromEntries("plan-x", meta, entries); out.Status != "approved" {
			t.Errorf("meta %q: status = %q, want approved", meta, out.Status)
		}
	}
}

// A plan whose review is over (finalized, completed, done) reports its real
// meta.status, not a status recomputed from section decisions.
func TestFeedbackOutput_LifecycleStatusWins(t *testing.T) {
	entries := []dbpkg.PlanFeedback{fb("slice-1", "approve", "approved")}
	for _, meta := range []string{"finalized", "completed", "done"} {
		if out := feedbackOutputFromEntries("plan-x", meta, entries); out.Status != meta {
			t.Errorf("status = %q, want %q", out.Status, meta)
		}
	}
	if out := feedbackOutputFromEntries("plan-x", "finalized", nil); out.Status != "finalized" {
		t.Errorf("no feedback: status = %q, want finalized", out.Status)
	}
}

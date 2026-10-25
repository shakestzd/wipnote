package db

import (
	"testing"

	_ "modernc.org/sqlite"
)

// bug-7b5339cc / bug-d67e695e style restatement: same defect, lightly reworded.
const (
	dupTitleA = "indexer skips recent orphan sessions on startup"
	dupDescA  = "the indexer drops freshly created orphan sessions during the startup scan because the quiescence check fires too early"

	dupTitleB = "indexer skips recent orphan sessions on startup"
	dupDescB  = "the indexer drops freshly created orphan sessions during the startup scan because the quiescence check fires too early"

	unrelatedTitle = "dashboard activity feed should poll every five seconds"
	unrelatedDesc  = "the web component currently refreshes once and never updates; add a setInterval refresh loop"
)

func TestFindDuplicate_ByteIdenticalMatches(t *testing.T) {
	cands := []DedupCandidate{
		{ID: "bug-7b5339cc", Type: "bug", Status: "done",
			scoringText: normalizeForSimilarity(dupTitleA + " " + dupDescA)},
	}
	got := FindDuplicate(dupTitleB, dupDescB, cands)
	if got == nil {
		t.Fatalf("expected duplicate match for byte-identical text, got nil")
	}
	if got.ID != "bug-7b5339cc" {
		t.Fatalf("matched wrong candidate: %s", got.ID)
	}
	if got.Score < DedupSimilarityThreshold {
		t.Fatalf("score %.3f below threshold %.3f", got.Score, DedupSimilarityThreshold)
	}
}

func TestFindDuplicate_NearDuplicateMatches(t *testing.T) {
	cands := []DedupCandidate{
		{ID: "bug-d67e695e", Type: "bug", Status: "in-progress",
			scoringText: normalizeForSimilarity(
				"Indexer skips recent orphan sessions on startup. " +
					"The indexer drops freshly-created orphan sessions during the startup scan " +
					"because the quiescence check fires too early.")},
	}
	// Reworded but same defect.
	got := FindDuplicate(
		"indexer skips recent orphan sessions on startup",
		"indexer drops freshly created orphan sessions during startup scan; quiescence check fires too early",
		cands)
	if got == nil {
		t.Fatalf("expected near-duplicate to match, got nil")
	}
}

func TestFindDuplicate_UnrelatedNoFalsePositive(t *testing.T) {
	cands := []DedupCandidate{
		{ID: "bug-7b5339cc", Type: "bug", Status: "done",
			scoringText: normalizeForSimilarity(dupTitleA + " " + dupDescA)},
	}
	got := FindDuplicate(unrelatedTitle, unrelatedDesc, cands)
	if got != nil {
		t.Fatalf("expected NO match for unrelated item, got %s (score %.3f)",
			got.ID, got.Score)
	}
}

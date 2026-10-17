package hooks

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// TestGeminiSessionEndRelease verifies the cross-harness session-end contract
// for part (c): Gemini's Stop event is mapped (in packages/plugin-core/
// manifest.json) to geminiEventName "SessionEnd" with geminiHandler
// "session-end", so a Gemini session exit reaches THIS exact SessionEnd
// handler. The handler must release all active claims for the session — the
// same path Codex reaches via its SessionEnd -> session-end manifest wiring
// (repointed from the phantom TaskComplete event in bug-e39d408f).
func TestGeminiSessionEndRelease(t *testing.T) {
	td := setupTestDB(t)
	database := td.DB
	projectDir := t.TempDir()

	td.addFeature("feat-gem01a2", "feature", "gemini work", "in-progress")

	c := &models.Claim{
		ClaimID:        "claim-gemini",
		WorkItemID:     "feat-gem01a2",
		OwnerSessionID: "test-sess",
		OwnerAgent:     "gemini-cli",
		Status:         models.ClaimInProgress,
	}
	if err := db.ClaimItem(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	// Precondition: the claim is active.
	if got, _ := db.GetActiveClaim(database, "feat-gem01a2"); got == nil {
		t.Fatal("precondition: expected an active claim before SessionEnd")
	}

	// Gemini reaches the session-end handler via the manifest geminiHandler
	// mapping; invoking it directly is the function-level equivalent.
	endEvent := &CloudEvent{SessionID: "test-sess", CWD: projectDir}
	res, err := SessionEnd(endEvent, database, projectDir)
	if err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	if !res.Continue {
		t.Error("expected Continue=true from SessionEnd")
	}

	// All active claims for the session must be released (abandoned).
	if got, _ := db.GetActiveClaim(database, "feat-gem01a2"); got != nil {
		t.Fatalf("expected no active claim after SessionEnd, got status=%s", got.Status)
	}
	var status string
	database.QueryRow(
		`SELECT status FROM claims WHERE claim_id = 'claim-gemini'`,
	).Scan(&status)
	if status != "abandoned" {
		t.Fatalf("claim status after SessionEnd = %q, want abandoned", status)
	}
}

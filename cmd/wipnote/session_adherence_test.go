package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

func TestRenderSessionShow_IncludesAdherence(t *testing.T) {
	database, err := dbpkg.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	session := &models.Session{
		SessionID:     "sess-render",
		AgentAssigned: "codex",
		Status:        "completed",
		CreatedAt:     now,
		Adherence: &models.SessionAdherence{
			Score:  75,
			Passed: 2,
			Warned: 1,
			Failed: 1,
			Checks: []models.SessionAdherenceCheck{
				{Key: "gate_ran", Status: models.SessionAdherencePass, Summary: "Latest gate record passed."},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderSessionShow(&buf, database, session); err != nil {
		t.Fatalf("renderSessionShow: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Adherence 75%") {
		t.Fatalf("expected adherence summary in output:\n%s", out)
	}
	if !strings.Contains(out, "gate_ran") {
		t.Fatalf("expected adherence check details in output:\n%s", out)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/core/workitem"
)

const (
	lineageRunLive  = "11111111-2222-4333-8444-555555555555"
	lineageRunEnded = "11111111-2222-4333-8444-666666666666"
)

func seedLineageRuns(t *testing.T) (wipnoteDir, featID string) {
	t.Helper()
	wipnoteDir = newLineageProjectionWipnoteDir(t)
	p, err := workitem.Open(wipnoteDir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	f, err := p.Features.Create("Gate it")
	if err != nil {
		t.Fatal(err)
	}
	featID = f.ID
	at := time.Date(2026, 10, 27, 16, 20, 0, 0, time.UTC)
	exit0 := 0
	store := runledger.NewStore(wipnoteDir)
	ev := func(session, key, phase, desc, item string, offset time.Duration) runledger.Event {
		e := runledger.Event{RunID: runledger.RunIDFor(session, key), Phase: phase, At: at.Add(offset),
			ToolUseID: key, Kind: runledger.KindShell, Description: desc, Session: session,
			WorkItem: item, Commit: "abc1234def"}
		if phase == runledger.PhaseEnd {
			e.Status, e.Exit, e.FirstError = runledger.StatusCompleted, &exit0, "--- FAIL: TestFlaky"
		}
		return e
	}
	for _, e := range []runledger.Event{
		ev(lineageRunLive, "tu1", runledger.PhaseStart, "gate", featID, 0),
		ev(lineageRunLive, "tu1", runledger.PhaseEnd, "", "", 11*time.Minute),
		ev(lineageRunLive, "tu2", runledger.PhaseStart, "long build", featID, time.Minute),
		ev(lineageRunEnded, "tu3", runledger.PhaseStart, "lost watch", featID, 2*time.Minute),
		ev(lineageRunLive, "tu4", runledger.PhaseStart, "other item", "feat-99999999", 0),
	} {
		if err := store.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	sl := sessionledger.NewStore(wipnoteDir)
	if _, err := sl.Open(sessionledger.Record{SessionID: lineageRunEnded, Harness: "claude", StartedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := sl.Close(lineageRunEnded, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	lineageRunsLoc = time.UTC
	t.Cleanup(func() { lineageRunsLoc = nil })
	return wipnoteDir, featID
}

func TestLineage_ShowsRunsOfTheWorkItem(t *testing.T) {
	wipnoteDir, featID := seedLineageRuns(t)
	var buf bytes.Buffer
	if err := runLineageProjection(&buf, wipnoteDir, featID, lineageOpts{depth: 5}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"runs (3):",
		"gate exit 0 at 16:31 on commit abc1234",
		"first error: --- FAIL: TestFlaky",
		"long build running since 16:21 on commit abc1234",
		"lost watch unknown",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lineage output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "other item") {
		t.Errorf("another item's run leaked into this lineage:\n%s", out)
	}
}

func TestLineage_NoRunsSectionWithoutRuns(t *testing.T) {
	wipnoteDir := newLineageProjectionWipnoteDir(t)
	p, _ := workitem.Open(wipnoteDir, "test-agent")
	f, _ := p.Features.Create("Quiet")
	var buf bytes.Buffer
	if err := runLineageProjection(&buf, wipnoteDir, f.ID, lineageOpts{depth: 5}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "runs (") {
		t.Errorf("runs section printed for an item with no runs:\n%s", buf.String())
	}
}

func TestLineage_RunsInJSON(t *testing.T) {
	wipnoteDir, featID := seedLineageRuns(t)
	var buf bytes.Buffer
	if err := runLineageProjection(&buf, wipnoteDir, featID, lineageOpts{depth: 5, jsonOut: true}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Runs []struct {
			Description string `json:"description"`
			Status      string `json:"status"`
			Exit        *int   `json:"exit_code"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(got.Runs) != 3 || got.Runs[0].Description != "gate" || got.Runs[0].Exit == nil || *got.Runs[0].Exit != 0 {
		t.Errorf("runs = %+v", got.Runs)
	}
}

func TestFeatureShow_ListsRuns(t *testing.T) {
	wipnoteDir, featID := seedLineageRuns(t)
	projectDirFlag = filepath.Dir(wipnoteDir)
	t.Cleanup(func() { projectDirFlag = "" })
	out := captureStdout(t, func() {
		if err := runWiShowWithFormat(featID, "text"); err != nil {
			t.Fatalf("show: %v", err)
		}
	})
	if !strings.Contains(out, "gate exit 0 at 16:31 on commit abc1234") || strings.Contains(out, "other item") {
		t.Errorf("feature show runs section wrong:\n%s", out)
	}
}

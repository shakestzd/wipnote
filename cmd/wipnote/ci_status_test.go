package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

const ciStatusChecks = `[
 {"name":"unit","state":"SUCCESS","bucket":"pass","startedAt":"2026-10-20T10:00:00Z","completedAt":"2026-10-20T10:05:00Z","link":"https://example.test/r/1","workflow":"CI","event":"pull_request"},
 {"name":"vet","state":"FAILURE","bucket":"fail","startedAt":"2026-10-20T10:00:00Z","completedAt":"2026-10-20T10:01:00Z","link":"https://example.test/r/2","workflow":"CI","event":"pull_request"},
 {"name":"e2e","state":"IN_PROGRESS","bucket":"pending","startedAt":"2026-10-20T10:00:00Z","completedAt":"0001-01-01T00:00:00Z","link":"https://example.test/r/3","workflow":"CI","event":"pull_request"}
]`

func ciStatusFake() *gitwatch.FakeRunner {
	return &gitwatch.FakeRunner{Results: map[string]gitwatch.FakeResult{
		"gh pr view 7 --json number,title,headRefName,baseRefName,state,url": {
			Stdout: `{"number":7,"title":"Add thing","headRefName":"feat-470715de-x","baseRefName":"main","state":"OPEN","url":"https://example.test/pull/7"}`},
		"gh pr checks 7 --json name,state,bucket,startedAt,completedAt,link,workflow,event": {
			Stdout: ciStatusChecks, Exit: 1},
	}}
}

func TestCIStatus_Text(t *testing.T) {
	var out bytes.Buffer
	if err := runCIStatus(context.Background(), &out, ciStatusFake(), "7", false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"PR #7", "Add thing", "feat-470715de", "1 pass", "1 fail", "1 pending", "https://example.test/r/2"} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "vet") > strings.Index(text, "e2e") {
		t.Errorf("failed checks must come first:\n%s", text)
	}
	if strings.Contains(text, "https://example.test/r/1") {
		t.Errorf("passing checks are summarised, not listed:\n%s", text)
	}
}

func TestCIStatus_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := runCIStatus(context.Background(), &out, ciStatusFake(), "7", true); err != nil {
		t.Fatal(err)
	}
	var snap gitwatch.Snapshot
	if err := json.Unmarshal(out.Bytes(), &snap); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if !snap.Available || snap.Counts.Total != 3 || snap.Counts.Fail != 1 || snap.WorkItem != "feat-470715de" {
		t.Errorf("snapshot: %+v", snap)
	}
}

func TestCIStatus_NotAvailableExitsClean(t *testing.T) {
	f := &gitwatch.FakeRunner{Results: map[string]gitwatch.FakeResult{
		"gh pr view 7 --json number,title,headRefName,baseRefName,state,url": {Err: gitwatch.ErrNotAvailable},
	}}
	for _, jsonOut := range []bool{false, true} {
		var out bytes.Buffer
		if err := runCIStatus(context.Background(), &out, f, "7", jsonOut); err != nil {
			t.Fatalf("json=%v: not-available must exit 0, got %v", jsonOut, err)
		}
		if !strings.Contains(out.String(), "not available") {
			t.Errorf("json=%v: want a not-available note, got %q", jsonOut, out.String())
		}
	}
}

func TestCIStatus_Registered(t *testing.T) {
	cmd, _, err := ciCmd().Find([]string{"status"})
	if err != nil || cmd.Name() != "status" {
		t.Fatalf("ci status not registered: %v", err)
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Error("ci status needs --json")
	}
}

func TestCIStatus_TextReviewLine(t *testing.T) {
	s := &gitwatch.Snapshot{Available: true, PR: &gitwatch.PR{Number: 7, Title: "Add thing"},
		Review: &gitwatch.Review{Decision: "CHANGES_REQUESTED", Requested: []string{"carol"},
			ThreadsOpen: 2, Mergeable: gitwatch.MergeableComputing, MergeState: "UNKNOWN"}}
	var out bytes.Buffer
	writeCIStatusText(&out, s)
	want := "review: CHANGES_REQUESTED · 2 open threads · merge computing"
	if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "carol") {
		t.Errorf("want %q and the requested reviewer in:\n%s", want, out.String())
	}
	s.Review, s.Notes = nil, []string{"review not read: HTTP 403"}
	out.Reset()
	writeCIStatusText(&out, s)
	if strings.Contains(out.String(), "review:") || !strings.Contains(out.String(), "HTTP 403") {
		t.Errorf("no review line, but the note, expected:\n%s", out.String())
	}
}

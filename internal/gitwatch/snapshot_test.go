package gitwatch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var fixedNow = func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC) }

func snapshotFake(t *testing.T) *FakeRunner {
	return &FakeRunner{Results: map[string]FakeResult{
		"gh pr view 42 --json " + prFields: {Stdout: fixture(t, "pr_view.json")},
		checksArgv:                         {Stdout: fixture(t, "checks_mixed.json"), Exit: 1},
	}}
}

func TestBuildSnapshot(t *testing.T) {
	s, err := BuildSnapshot(context.Background(), snapshotFake(t), "42", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Available || s.PR == nil || s.PR.Number != 42 {
		t.Fatalf("snapshot: %+v", s)
	}
	if s.WorkItem != "feat-470715de" {
		t.Errorf("work item from title = %q", s.WorkItem)
	}
	want := Counts{Pass: 1, Fail: 2, Pending: 1, Other: 2, Total: 6}
	if s.Counts != want {
		t.Errorf("counts = %+v, want %+v", s.Counts, want)
	}
	var order []string
	for _, c := range s.Checks {
		order = append(order, c.Bucket+":"+c.Name)
	}
	got := strings.Join(order, " ")
	if got != "fail:setup fail:test cancel:deploy pending:lint pass:build skipping:docs" {
		t.Errorf("sort order = %s", got)
	}
	if !s.FetchedAt.Equal(fixedNow()) {
		t.Errorf("fetched_at = %v", s.FetchedAt)
	}
}

func TestBuildSnapshot_NotAvailableIsNotAnError(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr view 42 --json " + prFields: {Err: ErrNotAvailable},
	}}
	s, err := BuildSnapshot(context.Background(), f, "42", fixedNow)
	if err != nil {
		t.Fatalf("not-available must not be an error: %v", err)
	}
	if s.Available || s.Reason == "" || s.PR != nil {
		t.Errorf("want available=false with a reason, got %+v", s)
	}
}

// Any other gh failure (an HTTP 403 from a proxy, a 502, no network) is also
// reported as available=false with gh's first stderr line, never as an error,
// so the pane and the gate degrade instead of breaking (owner, 2026-10-26).
func TestBuildSnapshot_GhFailureIsUnavailable(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr view 42 --json " + prFields: {Stdout: "", Stderr: "HTTP 403: Forbidden\nmore", Exit: 1},
	}}
	s, err := BuildSnapshot(context.Background(), f, "42", fixedNow)
	if err != nil {
		t.Fatalf("gh failure must not be an error: %v", err)
	}
	if s.Available || s.PR != nil || !strings.Contains(s.Reason, "HTTP 403") {
		t.Errorf("want available=false with the stderr reason, got %+v", s)
	}
}

func TestBuildSnapshot_NoPR(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr list --head nobody --state all --limit 1 --json number": {Stdout: `[]`},
	}}
	s, err := BuildSnapshot(context.Background(), f, "nobody", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Available || s.PR != nil || !strings.Contains(s.Reason, "no pull request") {
		t.Errorf("no PR: %+v", s)
	}
}

func TestSnapshotJSON_StableSnakeCase(t *testing.T) {
	s, err := BuildSnapshot(context.Background(), snapshotFake(t), "42", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(s)
	text := string(data)
	keys := []string{`"available"`, `"pr"`, `"work_item"`, `"checks"`, `"counts"`, `"fetched_at"`}
	last := -1
	for _, k := range keys {
		i := strings.Index(text, k)
		if i < 0 || i < last {
			t.Fatalf("key %s missing or out of order in %s", k, text)
		}
		last = i
	}
	for _, k := range []string{`"started_at"`, `"completed_at"`, `"required"`, `"branch"`, `"base"`} {
		if !strings.Contains(text, k) {
			t.Errorf("missing %s in %s", k, text)
		}
	}
	if strings.Contains(text, `"reason"`) {
		t.Errorf("empty reason should be omitted: %s", text)
	}
}

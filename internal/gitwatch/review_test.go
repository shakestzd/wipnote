package gitwatch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The GraphQL fixtures (graphql_*.json) are hand-written in the shape of
// GitHub's GraphQL reference (docs.github.com/en/graphql/reference): the
// API is not reachable from the environment the tests were written in.

const reviewViewArgv = "gh pr view 42 --json " + reviewFields

var ghPR = &PR{Number: 42, URL: "https://github.com/acme/widgets/pull/42"}

func graphqlArgv(host string) string {
	return "gh " + strings.Join(reviewGraphQLArgs(repoRef{Host: host, Owner: "acme", Name: "widgets"}, 42), " ")
}

func reviewFake(t *testing.T, view string) *FakeRunner {
	return &FakeRunner{Results: map[string]FakeResult{
		reviewViewArgv:            {Stdout: fixture(t, view)},
		graphqlArgv("github.com"): {Stdout: fixture(t, "graphql_review.json")},
	}}
}

func TestFetchReview_States(t *testing.T) {
	cases := []struct {
		fixture, decision, mergeable, state string
		requested                           []string
	}{
		{"review_approved.json", "APPROVED", "MERGEABLE", "CLEAN", []string{}},
		{"review_changes.json", "CHANGES_REQUESTED", "MERGEABLE", "BLOCKED", []string{}},
		{"review_required.json", "REVIEW_REQUIRED", "MERGEABLE", "BLOCKED", []string{"carol", "acme/platform"}},
		{"review_dirty.json", "APPROVED", "CONFLICTING", "DIRTY", []string{}},
		{"review_behind.json", "none", "MERGEABLE", "BEHIND", []string{}},
		{"review_unknown.json", "APPROVED", MergeableComputing, "UNKNOWN", []string{}},
	}
	for _, c := range cases {
		rv, _, err := FetchReview(context.Background(), reviewFake(t, c.fixture), ghPR)
		if err != nil {
			t.Fatalf("%s: %v", c.fixture, err)
		}
		if rv.Decision != c.decision || rv.Mergeable != c.mergeable || rv.MergeState != c.state {
			t.Errorf("%s: got %+v", c.fixture, rv)
		}
		if !reflect.DeepEqual(rv.Requested, c.requested) {
			t.Errorf("%s: requested = %q, want %q", c.fixture, rv.Requested, c.requested)
		}
	}
}

// FetchReview keeps the baseRefOid its view read already asks for.
func TestFetchReview_FillsBaseOID(t *testing.T) {
	f := reviewFake(t, "review_approved.json")
	f.Results[reviewViewArgv] = FakeResult{Stdout: `{"reviewDecision":"APPROVED","baseRefOid":"abc123"}`}
	pr := &PR{Number: 42, URL: ghPR.URL}
	if _, _, err := FetchReview(context.Background(), f, pr); err != nil {
		t.Fatal(err)
	}
	if pr.BaseOID != "abc123" {
		t.Errorf("BaseOID = %q, want abc123", pr.BaseOID)
	}
	if strings.Contains(strings.Join(f.Calls, "\n"), "latestReviews") {
		t.Errorf("latestReviews is unused and must not be asked for: %q", f.Calls)
	}
	data, _ := json.Marshal(pr)
	if !strings.Contains(string(data), `"base_oid":"abc123"`) {
		t.Errorf("missing base_oid in %s", data)
	}
}

// UNKNOWN means GitHub is still computing; it must never read as a conflict.
func TestFetchReview_UnknownIsNotAConflict(t *testing.T) {
	rv, _, err := FetchReview(context.Background(), reviewFake(t, "review_unknown.json"), ghPR)
	if err != nil {
		t.Fatal(err)
	}
	if rv.Mergeable == "CONFLICTING" || rv.Mergeable == "UNKNOWN" {
		t.Errorf("UNKNOWN must map to computing, got %q", rv.Mergeable)
	}
}

func TestFetchReview_ThreadsSkipResolved(t *testing.T) {
	rv, _, err := FetchReview(context.Background(), reviewFake(t, "review_approved.json"), ghPR)
	if err != nil {
		t.Fatal(err)
	}
	// 5 threads: 2 resolved (one outdated), 3 open of which 2 outdated.
	if rv.ThreadsOpen != 3 || rv.ThreadsOutdated != 2 {
		t.Errorf("threads open=%d outdated=%d, want 3 and 2", rv.ThreadsOpen, rv.ThreadsOutdated)
	}
}

func TestMarkRequired_MatchesNameAndWorkflow(t *testing.T) {
	_, req, err := FetchReview(context.Background(), reviewFake(t, "review_approved.json"), ghPR)
	if err != nil {
		t.Fatal(err)
	}
	checks := []Check{
		{Name: "build", Workflow: "CI"},
		{Name: "build", Workflow: "Release"},
		{Name: "test", Workflow: "CI"},
		{Name: "app-check"},
		{Name: "ci/legacy"},
		{Name: "unlisted", Workflow: "CI"},
	}
	MarkRequired(checks, req)
	want := []bool{true, false, false, true, true, false}
	for i, c := range checks {
		if c.Required != want[i] {
			t.Errorf("%s/%s required = %v, want %v", c.Workflow, c.Name, c.Required, want[i])
		}
	}
}

func TestFetchReview_HostOtherThanGitHub(t *testing.T) {
	f := reviewFake(t, "review_approved.json")
	f.Results[graphqlArgv("ghe.example.test")] = FakeResult{Stdout: fixture(t, "graphql_review.json")}
	pr := &PR{Number: 42, URL: "https://ghe.example.test/acme/widgets/pull/42"}
	if _, _, err := FetchReview(context.Background(), f, pr); err != nil {
		t.Fatal(err)
	}
	if last := f.Calls[len(f.Calls)-1]; !strings.Contains(last, "--hostname ghe.example.test") {
		t.Errorf("GraphQL call must name the PR's host: %s", last)
	}
}

func TestFetchReview_Failures(t *testing.T) {
	f := reviewFake(t, "review_approved.json")
	f.Results[graphqlArgv("github.com")] = FakeResult{Stdout: fixture(t, "graphql_errors.json"), Exit: 1}
	_, _, err := FetchReview(context.Background(), f, ghPR)
	if err == nil || !strings.Contains(err.Error(), "Could not resolve") {
		t.Errorf("GraphQL errors must surface: %v", err)
	}
	f.Results[graphqlArgv("github.com")] = FakeResult{Stderr: "HTTP 403: Forbidden", Exit: 1}
	if _, _, err = FetchReview(context.Background(), f, ghPR); err == nil {
		t.Error("a 403 must be an error")
	}
	empty := &FakeRunner{}
	_, _, err = FetchReview(context.Background(), empty, &PR{Number: 42, URL: "https://example.test/pull/42"})
	if err == nil || len(empty.Calls) != 0 {
		t.Errorf("a URL without owner/repo fails before any call: %v, calls %q", err, empty.Calls)
	}
}

func snapshotReviewFake(t *testing.T) *FakeRunner {
	f := reviewFake(t, "review_required.json")
	f.Results["gh pr view 42 --json "+prFields] = FakeResult{Stdout: fixture(t, "pr_view_github.json")}
	f.Results[checksArgv] = FakeResult{Stdout: fixture(t, "checks_mixed.json"), Exit: 1}
	return f
}

func TestBuildSnapshot_Review(t *testing.T) {
	s, err := BuildSnapshot(context.Background(), snapshotReviewFake(t), "42", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if s.Review == nil || s.Review.Decision != "REVIEW_REQUIRED" || s.Review.ThreadsOpen != 3 {
		t.Fatalf("review: %+v", s.Review)
	}
	if n := reviewNotes(s.Notes); len(n) != 0 {
		t.Errorf("no review notes expected: %q", n)
	}
	required := map[string]bool{}
	for _, c := range s.Checks {
		required[c.Workflow+"/"+c.Name] = c.Required
	}
	if !required["CI/build"] || required["CI/test"] {
		t.Errorf("required marking: %v", required)
	}
	data, _ := json.Marshal(s)
	for _, k := range []string{`"review"`, `"threads_open"`, `"threads_outdated"`, `"merge_state"`, `"requested"`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("missing %s in %s", k, data)
		}
	}
}

// reviewNotes keeps the notes this slice adds; branch-health notes from the
// fake's missing git results are another section's concern.
func reviewNotes(notes []string) []string {
	var out []string
	for _, n := range notes {
		if strings.HasPrefix(n, "review") {
			out = append(out, n)
		}
	}
	return out
}

// A failed review read degrades to a note; the checks still come through.
func TestBuildSnapshot_ReviewFailureIsANote(t *testing.T) {
	f := snapshotReviewFake(t)
	f.Results[graphqlArgv("github.com")] = FakeResult{Stderr: "HTTP 403: Forbidden", Exit: 1}
	s, err := BuildSnapshot(context.Background(), f, "42", fixedNow)
	if err != nil {
		t.Fatalf("review failure must not be an error: %v", err)
	}
	if !s.Available || s.PR == nil || s.Counts.Total != 6 {
		t.Errorf("snapshot must survive: %+v", s)
	}
	if n := reviewNotes(s.Notes); s.Review != nil || len(n) != 1 {
		t.Errorf("want nil review and one note, got %+v %q", s.Review, s.Notes)
	}
}

func TestBuildSnapshot_ReviewCancelledIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := snapshotReviewFake(t)
	f.Results[reviewViewArgv] = FakeResult{Err: context.Canceled}
	cancel()
	if _, err := attachReviewTo(ctx, f); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx must be an error, got %v", err)
	}
}

func attachReviewTo(ctx context.Context, r Runner) (*Snapshot, error) {
	s := &Snapshot{Available: true, PR: ghPR, Checks: []Check{}}
	return s, attachReview(ctx, r, s)
}

package gitwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const baseMainArgv = "gh run list --branch main --limit 20 --json " + baseRunFields

func baseFake(t *testing.T, stdout string) *FakeRunner {
	return &FakeRunner{Results: map[string]FakeResult{baseMainArgv: {Stdout: stdout}}}
}

func TestFetchBase_Green(t *testing.T) {
	b, err := FetchBase(context.Background(), baseFake(t, fixture(t, "base_runs_green.json")), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	// CI's older failure is superseded by its later success; a cancelled
	// latest run is not a failure.
	if b.Name != "main" || b.State != BaseGreen || b.RedToo || len(b.Failing) != 0 {
		t.Errorf("green base: %+v", b)
	}
	if len(b.Runs) != 2 {
		t.Errorf("want latest run per workflow (2), got %+v", b.Runs)
	}
}

func TestFetchBase_Red(t *testing.T) {
	b, err := FetchBase(context.Background(), baseFake(t, fixture(t, "base_runs_red.json")), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.State != BaseRed || !b.RedToo {
		t.Fatalf("red base (red beats a run in progress): %+v", b)
	}
	var got []string
	for _, f := range b.Failing {
		got = append(got, f.Workflow+"="+f.Conclusion+"@"+f.URL)
	}
	want := "CI=startup_failure@https://example.test/runs/22 E2E=timed_out@https://example.test/runs/24"
	if strings.Join(got, " ") != want {
		t.Errorf("failing = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestFetchBase_Pending(t *testing.T) {
	b, err := FetchBase(context.Background(), baseFake(t, fixture(t, "base_runs_pending.json")), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.State != BasePending || b.RedToo || len(b.Failing) != 0 {
		t.Errorf("pending base: %+v", b)
	}
}

func TestFetchBase_NoRuns(t *testing.T) {
	b, err := FetchBase(context.Background(), baseFake(t, `[]`), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.State != BaseNone || b.RedToo || b.Runs == nil || len(b.Runs) != 0 {
		t.Errorf("no runs: %+v", b)
	}
}

func TestFetchBase_GhFailureIsError(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{baseMainArgv: {Stderr: "HTTP 403: Forbidden", Exit: 1}}}
	b, err := FetchBase(context.Background(), f, "main", "")
	if err == nil || b != nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("want an error with gh's reason, got %+v, %v", b, err)
	}
}

func TestBaseName(t *testing.T) {
	if got := BaseName(&PR{Base: "develop"}); got != "develop" {
		t.Errorf("PR base: %q", got)
	}
	if got := BaseName(nil); got != "main" {
		t.Errorf("no PR: %q", got)
	}
	if got := BaseName(&PR{}); got != "main" {
		t.Errorf("empty PR base: %q", got)
	}
}

// Base red too also covers the run at the PR's base commit: CI's latest run
// is green, but its earlier run at b1 failed. Two-sided: a base commit whose
// runs passed or were cancelled, and no base commit, stay false.
func TestFetchBase_RedAtPRBaseCommit(t *testing.T) {
	cases := []struct {
		oid  string
		want bool
	}{{"b1", true}, {"b2", false}, {"", false}, {"unknown", false}}
	for _, c := range cases {
		b, err := FetchBase(context.Background(), baseFake(t, fixture(t, "base_runs_green.json")), "main", c.oid)
		if err != nil {
			t.Fatal(err)
		}
		if b.RedToo != c.want || b.State != BaseGreen || len(b.Failing) != 0 {
			t.Errorf("oid %q: red_too = %v, want %v (state %s, failing %+v)", c.oid, b.RedToo, c.want, b.State, b.Failing)
		}
	}
}

// The snapshot hands the review read's baseRefOid to the base read.
func TestBuildSnapshot_BaseRedAtBaseOID(t *testing.T) {
	noReleaseSetting(t)
	runs := `[{"headSha":"%s","status":"completed","conclusion":"failure","workflowName":"CI","createdAt":"2026-10-24T10:00:00Z"},
 {"headSha":"later","status":"completed","conclusion":"success","workflowName":"CI","createdAt":"2026-10-25T10:00:00Z"}]`
	for _, c := range []struct {
		sha  string
		want bool
	}{{"1111111111111111111111111111111111111111", true}, {"2222222222222222222222222222222222222222", false}} {
		f := snapshotReviewFake(t)
		f.Results[baseMainArgv] = FakeResult{Stdout: fmt.Sprintf(runs, c.sha)}
		s, err := BuildSnapshot(context.Background(), f, "42", fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		if s.PR.BaseOID != "1111111111111111111111111111111111111111" {
			t.Fatalf("PR.BaseOID = %q", s.PR.BaseOID)
		}
		if s.Base == nil || s.Base.State != BaseGreen || s.Base.RedToo != c.want {
			t.Errorf("failed run at %s: base %+v, want red_too %v", c.sha[:4], s.Base, c.want)
		}
	}
}

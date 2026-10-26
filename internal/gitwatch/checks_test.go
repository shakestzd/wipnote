package gitwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const checksArgv = "gh pr checks 42 --json " + checkFields

func TestBucketFor(t *testing.T) {
	cases := map[string]string{
		"SUCCESS":         BucketPass,
		"SKIPPED":         BucketSkipping,
		"NEUTRAL":         BucketSkipping,
		"ERROR":           BucketFail,
		"FAILURE":         BucketFail,
		"TIMED_OUT":       BucketFail,
		"ACTION_REQUIRED": BucketFail,
		"STARTUP_FAILURE": BucketFail,
		"CANCELLED":       BucketCancel,
		"IN_PROGRESS":     BucketPending,
		"QUEUED":          BucketPending,
		"PENDING":         BucketPending,
		"EXPECTED":        BucketPending,
		"":                BucketPending,
	}
	for state, want := range cases {
		if got := bucketFor(state); got != want {
			t.Errorf("bucketFor(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestResolveBucket(t *testing.T) {
	cases := []struct{ state, gh, want string }{
		{"STARTUP_FAILURE", "pending", BucketFail}, // fix on copy wins over gh
		{"SUCCESS", "pass", BucketPass},
		{"WEIRD", "fail", BucketFail}, // gh's bucket is trusted
		{"FAILURE", "", BucketFail},   // empty bucket: our own mapping
		{"SUCCESS", "", BucketPass},
	}
	for _, c := range cases {
		if got := resolveBucket(c.state, c.gh); got != c.want {
			t.Errorf("resolveBucket(%q,%q) = %q, want %q", c.state, c.gh, got, c.want)
		}
	}
}

func TestEliminateDuplicates_KeepsLatest(t *testing.T) {
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	in := []rawCheck{
		{Name: "build", Workflow: "CI", Event: "pull_request", State: "FAILURE", StartedAt: t0},
		{Name: "build", Workflow: "CI", Event: "pull_request", State: "SUCCESS", StartedAt: t0.Add(time.Hour)},
		{Name: "build", Workflow: "CI", Event: "push", State: "FAILURE", StartedAt: t0},
	}
	out := eliminateDuplicates(in)
	if len(out) != 2 {
		t.Fatalf("want 2 unique (name, workflow, event), got %d: %+v", len(out), out)
	}
	for _, c := range out {
		if c.Event == "pull_request" && c.State != "SUCCESS" {
			t.Errorf("kept the older pull_request run: %+v", c)
		}
	}
}

func TestFetchChecks_ParsesWhateverTheExit(t *testing.T) {
	for _, exit := range []int{0, 1, 8} {
		f := &FakeRunner{Results: map[string]FakeResult{
			checksArgv: {Stdout: fixture(t, "checks_mixed.json"), Stderr: "some checks were not successful", Exit: exit},
		}}
		checks, err := FetchChecks(context.Background(), f, 42)
		if err != nil {
			t.Fatalf("exit %d: valid stdout must parse, got %v", exit, err)
		}
		if len(checks) != 6 {
			t.Fatalf("exit %d: want 6 checks after dedupe, got %d", exit, len(checks))
		}
	}
}

func TestFetchChecks_BucketsAndDedupe(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		checksArgv: {Stdout: fixture(t, "checks_mixed.json"), Exit: 1},
	}}
	checks, err := FetchChecks(context.Background(), f, 42)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["build"].Bucket != BucketPass || byName["build"].Link != "https://example.test/run/1" {
		t.Errorf("build should be the latest (passing) run: %+v", byName["build"])
	}
	if byName["setup"].Bucket != BucketFail {
		t.Errorf("STARTUP_FAILURE must count as fail: %+v", byName["setup"])
	}
	if byName["deploy"].Bucket != BucketCancel || byName["docs"].Bucket != BucketSkipping {
		t.Errorf("cancel/skipping: %+v %+v", byName["deploy"], byName["docs"])
	}
}

func TestFetchChecks_EmptyBucketUsesOwnMapping(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		checksArgv: {Stdout: fixture(t, "checks_nobucket.json"), Exit: 8},
	}}
	checks, err := FetchChecks(context.Background(), f, 42)
	if err != nil || len(checks) != 3 {
		t.Fatalf("want 3 checks, got %d (%v)", len(checks), err)
	}
	want := map[string]string{"a": BucketPass, "b": BucketFail, "c": BucketPending}
	for _, c := range checks {
		if c.Bucket != want[c.Name] {
			t.Errorf("%s bucket = %q, want %q", c.Name, c.Bucket, want[c.Name])
		}
	}
}

func TestFetchChecks_YearOneTimesAreNil(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		checksArgv: {Stdout: fixture(t, "checks_mixed.json"), Exit: 8},
	}}
	checks, _ := FetchChecks(context.Background(), f, 42)
	if len(checks) == 0 {
		t.Fatal("no checks parsed")
	}
	for _, c := range checks {
		switch c.Name {
		case "lint":
			if c.CompletedAt != nil {
				t.Errorf("unfinished completedAt must be nil, got %v", c.CompletedAt)
			}
			if c.StartedAt == nil || c.StartedAt.Year() != 2026 {
				t.Errorf("real startedAt lost: %v", c.StartedAt)
			}
		case "test":
			if c.CompletedAt == nil {
				t.Error("finished completedAt must be kept")
			}
		}
	}
}

func TestFetchChecks_GarbageStdoutErrorsWithFirstStderrLine(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		checksArgv: {Stdout: "not json", Stderr: "HTTP 502: bad gateway\nretry later\n", Exit: 1},
	}}
	_, err := FetchChecks(context.Background(), f, 42)
	if err == nil {
		t.Fatal("garbage stdout must error")
	}
	if !strings.Contains(err.Error(), "HTTP 502: bad gateway") || strings.Contains(err.Error(), "retry later") {
		t.Errorf("error should carry only the first stderr line: %v", err)
	}
	if errors.Is(err, ErrNotAvailable) {
		t.Errorf("a gh failure is not not-available: %v", err)
	}
}

func TestFetchChecks_NotAvailable(t *testing.T) {
	cases := map[string]FakeResult{
		"missing gh": {Err: ErrNotAvailable},
		"auth exit":  {Stderr: "To get started with GitHub CLI, please run:  gh auth login\n", Exit: 4},
		"bad token":  {Stderr: "HTTP 401: Bad credentials (https://api.github.com/graphql)\n", Exit: 1},
	}
	for name, res := range cases {
		f := &FakeRunner{Results: map[string]FakeResult{checksArgv: res}}
		_, err := FetchChecks(context.Background(), f, 42)
		if !errors.Is(err, ErrNotAvailable) {
			t.Errorf("%s: want ErrNotAvailable, got %v", name, err)
		}
	}
}

// A PR with no CI yet (new, or a repo without workflows) makes gh exit 1 with
// "no checks reported on the '<branch>' branch" and empty stdout. That is zero
// checks, not a failed read; any other exit-1 message is still an error.
func TestFetchChecks_NoChecksReportedIsEmpty(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		checksArgv: {Stderr: "no checks reported on the 'feat-x' branch\n", Exit: 1},
	}}
	checks, err := FetchChecks(context.Background(), f, 42)
	if err != nil || checks == nil || len(checks) != 0 {
		t.Fatalf("want an empty, non-nil list and no error, got %v, %v", checks, err)
	}
	f.Results[checksArgv] = FakeResult{Stderr: "HTTP 502: Bad Gateway\n", Exit: 1}
	if _, err := FetchChecks(context.Background(), f, 42); err == nil {
		t.Fatal("other gh failures must still be errors")
	}
}

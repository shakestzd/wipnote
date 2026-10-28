package gitwatch

import (
	"context"
	"errors"
	"testing"
)

const (
	viewCurrent = "gh pr view --json number"
	listMine    = "gh pr list --author @me --state open --limit 1 --json number"
)

func TestResolvePR_Digits(t *testing.T) {
	f := &FakeRunner{}
	n, err := ResolvePR(context.Background(), f, "267")
	if err != nil || n != 267 {
		t.Fatalf("got %d, %v", n, err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("a number needs no gh call, got %q", f.Calls)
	}
}

func TestResolvePR_Branch(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr list --head feature/x --state all --limit 1 --json number": {Stdout: `[{"number":12}]`},
		"gh pr list --head nobody --state all --limit 1 --json number":    {Stdout: `[]`},
	}}
	n, err := ResolvePR(context.Background(), f, "feature/x")
	if err != nil || n != 12 {
		t.Fatalf("branch: got %d, %v", n, err)
	}
	_, err = ResolvePR(context.Background(), f, "nobody")
	if !errors.Is(err, ErrNoPR) {
		t.Fatalf("branch without PR: want ErrNoPR, got %v", err)
	}
}

func TestResolvePR_CurrentBranch(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		viewCurrent: {Stdout: `{"number":5}`},
	}}
	n, err := ResolvePR(context.Background(), f, "")
	if err != nil || n != 5 {
		t.Fatalf("got %d, %v", n, err)
	}
	if len(f.Calls) != 1 {
		t.Errorf("no fallback expected, calls %q", f.Calls)
	}
}

func TestResolvePR_FallbackToNewestOwnOpen(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		viewCurrent: {Stderr: "no pull requests found for branch \"main\"\n", Exit: 1},
		listMine:    {Stdout: `[{"number":9}]`},
	}}
	n, err := ResolvePR(context.Background(), f, "")
	if err != nil || n != 9 {
		t.Fatalf("got %d, %v", n, err)
	}
	f.Results[listMine] = FakeResult{Stdout: `[]`}
	if _, err := ResolvePR(context.Background(), f, ""); !errors.Is(err, ErrNoPR) {
		t.Fatalf("nothing anywhere: want ErrNoPR, got %v", err)
	}
}

func TestResolvePR_NotAvailableDoesNotFallBack(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		viewCurrent: {Err: ErrNotAvailable},
	}}
	_, err := ResolvePR(context.Background(), f, "")
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("want ErrNotAvailable, got %v", err)
	}
	if len(f.Calls) != 1 {
		t.Errorf("must not try the fallback, calls %q", f.Calls)
	}
}

func TestFetchPR(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr view 42 --json " + prFields: {Stdout: fixture(t, "pr_view.json")},
	}}
	pr, err := FetchPR(context.Background(), f, 42)
	if err != nil {
		t.Fatal(err)
	}
	want := PR{Number: 42, Title: "gitwatch core (feat-470715de)", Branch: "gitwatch/core",
		Base: "main", State: "OPEN", URL: "https://example.test/pull/42"}
	if *pr != want {
		t.Errorf("got %+v, want %+v", *pr, want)
	}
}

// ResolveBranchPR never reads its argument as a PR number.
func TestResolveBranchPR_DigitsAreABranch(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		"gh pr list --head 123 --state all --limit 1 --json number": {Stdout: `[{"number":7}]`},
	}}
	n, err := ResolveBranchPR(context.Background(), f, "123")
	if err != nil || n != 7 {
		t.Fatalf("branch 123: got %d, %v; want PR 7 by head branch", n, err)
	}
}

func TestResolveBranchPR_EmptyBranchIsNoPR(t *testing.T) {
	f := &FakeRunner{}
	if _, err := ResolveBranchPR(context.Background(), f, ""); !errors.Is(err, ErrNoPR) {
		t.Fatalf("empty branch: want ErrNoPR, got %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("an empty branch needs no gh call, got %q", f.Calls)
	}
}

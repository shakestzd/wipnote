package gitwatch

import (
	"context"
	"errors"
	"time"
)

// Counts tallies checks by bucket; Other is skipping plus cancel.
type Counts struct {
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Pending int `json:"pending"`
	Other   int `json:"other"`
	Total   int `json:"total"`
}

// Snapshot is the read state of one pull request. Available is false when gh
// cannot be used or a gh read failed (missing, not signed in, an HTTP or
// network error); Reason says why, or that no pull request matched. Later
// sections (review, branch, base, release) are omitempty; a section whose
// read failed stays nil and adds a line to Notes.
type Snapshot struct {
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"`
	PR        *PR       `json:"pr,omitempty"`
	WorkItem  string    `json:"work_item"`
	Checks    []Check   `json:"checks"`
	Counts    Counts    `json:"counts"`
	Review    *Review   `json:"review,omitempty"`
	Base      *Base     `json:"base,omitempty"`
	Release   *Release  `json:"release,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`

	// Local branch health (branch.go, worktrees.go); read even with no PR.
	Branch    *Branch    `json:"branch,omitempty"`
	Worktrees []Worktree `json:"worktrees,omitempty"`
	Notes     []string   `json:"notes,omitempty"`
}

// BuildSnapshot resolves arg to a PR (see ResolvePR) and reads its basics
// and checks. Every gh failure and an unmatched lookup come back as a
// Snapshot, not an error, so callers degrade instead of breaking; only a
// cancelled or expired ctx is an error.
func BuildSnapshot(ctx context.Context, r Runner, arg string, now func() time.Time) (*Snapshot, error) {
	s := &Snapshot{Available: true, Checks: []Check{}, FetchedAt: now().UTC()}
	defer addBranchHealth(ctx, r, s, now) // local; runs on every return, after s.PR is set
	pr, checks, err := fetchAll(ctx, r, arg)
	switch {
	case errors.Is(err, ErrNoPR):
		s.Reason = err.Error()
		return addBaseAndRelease(ctx, r, s)
	case err != nil && ctx.Err() != nil:
		return nil, err
	case err != nil:
		s.Available, s.Reason = false, err.Error()
		return s, nil
	}
	SortChecks(checks)
	s.PR, s.Checks, s.Counts = pr, checks, countChecks(checks)
	s.WorkItem = WorkItemID(pr.Branch, pr.Title)
	if err := attachReview(ctx, r, s); err != nil {
		return nil, err
	}
	return addBaseAndRelease(ctx, r, s)
}

func fetchAll(ctx context.Context, r Runner, arg string) (*PR, []Check, error) {
	n, err := ResolvePR(ctx, r, arg)
	if err != nil {
		return nil, nil, err
	}
	pr, err := FetchPR(ctx, r, n)
	if err != nil {
		return nil, nil, err
	}
	checks, err := FetchChecks(ctx, r, n)
	if err != nil {
		return nil, nil, err
	}
	return pr, checks, nil
}

func countChecks(checks []Check) Counts {
	var c Counts
	for _, ch := range checks {
		switch ch.Bucket {
		case BucketPass:
			c.Pass++
		case BucketFail:
			c.Fail++
		case BucketPending:
			c.Pending++
		default:
			c.Other++
		}
	}
	c.Total = len(checks)
	return c
}

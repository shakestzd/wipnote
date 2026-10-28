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

	// required is the last review read's required contexts, so a fast
	// refresh of the checks keeps their Required marks.
	required map[checkKey]bool
}

// BuildSnapshot resolves arg to a PR (see ResolvePR) and reads its basics
// and checks. Every gh failure and an unmatched lookup come back as a
// Snapshot, not an error, so callers degrade instead of breaking; only a
// cancelled or expired ctx is an error.
func BuildSnapshot(ctx context.Context, r Runner, arg string, now func() time.Time) (*Snapshot, error) {
	s := &Snapshot{Available: true, Checks: []Check{}, FetchedAt: now().UTC()}
	n, err := ResolvePR(ctx, r, arg)
	if err == nil {
		err = refreshPR(ctx, r, n, s)
	}
	switch {
	case errors.Is(err, ErrNoPR):
		s.Reason = err.Error()
	case err != nil && ctx.Err() != nil:
		return nil, err
	case err != nil:
		s.Available, s.Reason = false, err.Error()
		addBranchHealth(ctx, r, s, now)
		return s, nil
	}
	if err := refreshSlow(ctx, r, s, now); err != nil {
		return nil, err
	}
	return s, nil
}

// refreshPR is the fast section: it re-reads PR n's basics and checks into
// s, keeping the base commit and required marks of the last slow read.
func refreshPR(ctx context.Context, r Runner, n int, s *Snapshot) error {
	pr, err := FetchPR(ctx, r, n)
	if err != nil {
		return err
	}
	checks, err := FetchChecks(ctx, r, n)
	if err != nil {
		return err
	}
	if s.PR != nil {
		pr.BaseOID = s.PR.BaseOID
	}
	SortChecks(checks)
	MarkRequired(checks, s.required)
	s.PR, s.Checks, s.Counts = pr, checks, countChecks(checks)
	s.WorkItem = WorkItemID(pr.Branch, pr.Title)
	return nil
}

// refreshSlow re-reads the slow sections into s from scratch: review (with
// a PR), base and release, then local branch health. Each failed read is a
// note; only a cancelled or expired ctx is an error.
func refreshSlow(ctx context.Context, r Runner, s *Snapshot, now func() time.Time) error {
	s.Review, s.Base, s.Release, s.Branch, s.Worktrees, s.Notes = nil, nil, nil, nil, nil, nil
	if s.PR != nil {
		if err := attachReview(ctx, r, s); err != nil {
			return err
		}
	}
	if _, err := addBaseAndRelease(ctx, r, s); err != nil {
		return err
	}
	addBranchHealth(ctx, r, s, now)
	return nil
}

// CountChecks counts checks by bucket; skipping and cancel count as other.
func CountChecks(checks []Check) Counts { return countChecks(checks) }

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

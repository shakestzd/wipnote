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

// Snapshot is the read state of one pull request. Available is false only
// when gh cannot be used (missing or not signed in); Reason says why, or
// that no pull request matched. Later sections (review, branch, base,
// release) join as omitempty pointers after Counts.
type Snapshot struct {
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"`
	PR        *PR       `json:"pr,omitempty"`
	WorkItem  string    `json:"work_item"`
	Checks    []Check   `json:"checks"`
	Counts    Counts    `json:"counts"`
	FetchedAt time.Time `json:"fetched_at"`
}

// BuildSnapshot resolves arg to a PR (see ResolvePR) and reads its basics
// and checks. A missing or signed-out gh and an unmatched lookup come back
// as a Snapshot, not an error; other gh failures are errors.
func BuildSnapshot(ctx context.Context, r Runner, arg string, now func() time.Time) (*Snapshot, error) {
	s := &Snapshot{Available: true, Checks: []Check{}, FetchedAt: now().UTC()}
	pr, checks, err := fetchAll(ctx, r, arg)
	switch {
	case errors.Is(err, ErrNotAvailable):
		s.Available, s.Reason = false, err.Error()
		return s, nil
	case errors.Is(err, ErrNoPR):
		s.Reason = err.Error()
		return s, nil
	case err != nil:
		return nil, err
	}
	SortChecks(checks)
	s.PR, s.Checks, s.Counts = pr, checks, countChecks(checks)
	s.WorkItem = WorkItemID(pr.Branch, pr.Title)
	return s, nil
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

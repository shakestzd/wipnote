package gitwatch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Buckets a check falls into, as gh names them.
const (
	BucketPass     = "pass"
	BucketFail     = "fail"
	BucketPending  = "pending"
	BucketSkipping = "skipping"
	BucketCancel   = "cancel"
)

// checkFields is the --json field list asked of gh pr checks. gh has no
// required-ness field here, so Check.Required stays false for now.
const checkFields = "name,state,bucket,startedAt,completedAt,link,workflow,event"

// Check is one CI check on a pull request.
type Check struct {
	Name        string     `json:"name"`
	Workflow    string     `json:"workflow"`
	Bucket      string     `json:"bucket"`
	Required    bool       `json:"required"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Link        string     `json:"link"`
}

// rawCheck is one row of gh pr checks --json output.
type rawCheck struct {
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Bucket      string    `json:"bucket"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	Link        string    `json:"link"`
	Workflow    string    `json:"workflow"`
	Event       string    `json:"event"`
}

// FetchChecks reads the checks of PR number n, latest run per check.
func FetchChecks(ctx context.Context, r Runner, n int) ([]Check, error) {
	var raw []rawCheck
	if err := ghJSON(ctx, r, &raw, "pr", "checks", strconv.Itoa(n), "--json", checkFields); err != nil {
		return nil, fmt.Errorf("checks for PR #%d: %w", n, err)
	}
	unique := eliminateDuplicates(raw)
	checks := make([]Check, 0, len(unique))
	for _, c := range unique {
		checks = append(checks, Check{
			Name:        c.Name,
			Workflow:    c.Workflow,
			Bucket:      resolveBucket(c.State, c.Bucket),
			StartedAt:   realTime(c.StartedAt),
			CompletedAt: realTime(c.CompletedAt),
			Link:        c.Link,
		})
	}
	return checks, nil
}

// realTime is nil for a zero or placeholder time. GitHub reports an
// unfinished completedAt as year 1, so anything before 2000 is "not yet".
func realTime(t time.Time) *time.Time {
	if t.Year() < 2000 {
		return nil
	}
	return &t
}

// resolveBucket trusts gh's bucket when it sent one, except that
// STARTUP_FAILURE is always a failure (gh files it under pending).
func resolveBucket(state, ghBucket string) string {
	if state == "STARTUP_FAILURE" || ghBucket == "" {
		return bucketFor(state)
	}
	return ghBucket
}

// bucketRank orders buckets for display: what needs attention first.
var bucketRank = map[string]int{
	BucketFail: 0, BucketCancel: 1, BucketPending: 2, BucketPass: 3, BucketSkipping: 4,
}

func rankOf(bucket string) int {
	if r, ok := bucketRank[bucket]; ok {
		return r
	}
	return len(bucketRank)
}

// SortChecks orders checks fail, cancel, pending, pass, skipping, then name.
func SortChecks(checks []Check) {
	sort.SliceStable(checks, func(i, j int) bool {
		ri, rj := rankOf(checks[i].Bucket), rankOf(checks[j].Bucket)
		if ri != rj {
			return ri < rj
		}
		return checks[i].Name < checks[j].Name
	})
}

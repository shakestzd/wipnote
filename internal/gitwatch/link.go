package gitwatch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// linkFields is the --json field list of both PR list lookups. url is asked
// for too, so the gate's review read needs no extra gh pr view.
const linkFields = "number,title,headRefName,state,updatedAt,url"

// LinkTimeout caps the gate's whole work-item PR lookup.
const LinkTimeout = 10 * time.Second

// ShowTimeout caps the show line's lookup; show is read often, so it is
// shorter than the gate's.
const ShowTimeout = 5 * time.Second

// LinkedPR is a pull request found for a work item.
type LinkedPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Branch    string    `json:"headRefName"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FindPRForWorkItem finds the most recently updated PR whose title or head
// branch contains id. Two reads: a title search over all states, then the
// open PRs, whose head branches are matched locally (GitHub search has no
// substring match on branch names). A failed second read is ignored when
// the first already matched. No match is ErrNoPR.
func FindPRForWorkItem(ctx context.Context, r Runner, id string) (*LinkedPR, error) {
	var byTitle, open []LinkedPR
	err := ghJSON(ctx, r, &byTitle, "pr", "list", "--state", "all",
		"--search", id+" in:title", "--json", linkFields, "--limit", "20")
	if err != nil {
		return nil, err
	}
	err = ghJSON(ctx, r, &open, "pr", "list", "--state", "open", "--json", linkFields, "--limit", "100")
	if err != nil && (ctx.Err() != nil || errors.Is(err, ErrNotAvailable)) {
		return nil, err
	}
	matches := linkMatches(id, append(byTitle, open...))
	if len(matches) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrNoPR
	}
	return &matches[0], nil
}

// linkMatches keeps the rows whose title or branch contains id (search hits
// match on tokens, so they are re-checked), newest updated first.
func linkMatches(id string, rows []LinkedPR) []LinkedPR {
	seen := map[int]bool{}
	var out []LinkedPR
	for _, p := range rows {
		if seen[p.Number] || !(strings.Contains(p.Title, id) || strings.Contains(p.Branch, id)) {
			continue
		}
		seen[p.Number] = true
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// BuildSnapshotForWorkItem resolves id to its linked PR, then builds the
// usual snapshot for it. No linked PR is a snapshot with that reason; gh
// missing or failing is a not-available snapshot. Only a cancelled or
// expired ctx is an error.
func BuildSnapshotForWorkItem(ctx context.Context, r Runner, id string, now func() time.Time) (*Snapshot, error) {
	pr, err := FindPRForWorkItem(ctx, r, id)
	if err == nil {
		return BuildSnapshot(ctx, r, strconv.Itoa(pr.Number), now)
	}
	if ctx.Err() != nil {
		return nil, err
	}
	s := &Snapshot{Available: true, WorkItem: id, Checks: []Check{}, FetchedAt: now().UTC()}
	if errors.Is(err, ErrNoPR) {
		s.Reason = fmt.Sprintf("no pull request found for %s", id)
	} else {
		s.Available, s.Reason = false, err.Error()
	}
	return s, nil
}

// prHealth is the fast state the gate and feature show read: the linked PR,
// its checks and (for the gate) its review. reviewFailed is set when the
// review was asked for and could not be read.
type prHealth struct {
	pr           *LinkedPR
	checks       []Check
	review       *Review
	reviewFailed bool
}

// readPRHealth finds id's PR and reads its checks and, when withReview, its
// mergeability and review threads. A failed lookup or checks read is an
// error; a failed review read only sets reviewFailed.
func readPRHealth(ctx context.Context, r Runner, id string, withReview bool) (*prHealth, error) {
	pr, err := FindPRForWorkItem(ctx, r, id)
	if err != nil {
		return nil, err
	}
	checks, err := FetchChecks(ctx, r, pr.Number)
	if err != nil {
		return nil, err
	}
	h := &prHealth{pr: pr, checks: checks}
	if withReview {
		rv, _, err := FetchReview(ctx, r, &PR{Number: pr.Number, URL: pr.URL})
		h.review, h.reviewFailed = rv, err != nil
	}
	return h, nil
}

// warnReasons names what makes the PR need attention: failed or cancelled
// checks, a conflict, unresolved review threads. Empty means healthy.
func (h *prHealth) warnReasons() []string {
	var failed, cancelled int
	for _, c := range h.checks {
		switch c.Bucket {
		case BucketFail:
			failed++
		case BucketCancel:
			cancelled++
		}
	}
	var reasons []string
	if failed > 0 {
		reasons = append(reasons, plural(failed, "failed check"))
	}
	if cancelled > 0 {
		reasons = append(reasons, plural(cancelled, "cancelled check"))
	}
	if rv := h.review; rv != nil {
		if rv.Mergeable == "CONFLICTING" || rv.MergeState == "DIRTY" {
			reasons = append(reasons, "merge conflict")
		}
		if rv.ThreadsOpen > 0 {
			reasons = append(reasons, plural(rv.ThreadsOpen, "unresolved review thread"))
		}
	}
	return reasons
}

// plural is "1 <noun>" or "<n> <noun>s".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// GateWarning returns the one WARN line for id's linked PR, or "" when it
// is healthy, not OPEN, none is linked, or the lookup or checks read fails.
// A failed review read still warns from the checks, marked "(review not
// read)". The whole lookup is bounded by timeout.
func GateWarning(ctx context.Context, r Runner, id string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h, err := readPRHealth(ctx, r, id, true)
	if err != nil || h.pr.State != "OPEN" {
		return ""
	}
	reasons := h.warnReasons()
	if len(reasons) == 0 {
		return ""
	}
	why := strings.Join(reasons, ", ")
	if h.reviewFailed {
		why += " (review not read)"
	}
	return fmt.Sprintf("WARN: linked PR #%d (%s) needs attention: %s — %s", h.pr.Number, h.pr.State, why, h.pr.URL)
}

// ShowLine returns "PR #N <state> · CI <counts>" for id's linked PR, or ""
// when none is linked or any gh read fails. Bounded by timeout.
func ShowLine(ctx context.Context, r Runner, id string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h, err := readPRHealth(ctx, r, id, false)
	if err != nil {
		return ""
	}
	c := countChecks(h.checks)
	return fmt.Sprintf("PR #%d %s · CI %d pass, %d fail, %d pending", h.pr.Number, h.pr.State, c.Pass, c.Fail, c.Pending)
}

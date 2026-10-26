package gitwatch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// reviewFields is the --json field list asked of gh pr view for review state.
const reviewFields = "reviewDecision,reviewRequests,mergeable,mergeStateStatus,baseRefOid"

// MergeableComputing replaces GitHub's UNKNOWN mergeable: GitHub is still
// computing mergeability in the background. It is never a conflict.
const MergeableComputing = "computing"

// decisionNone stands for an empty reviewDecision (no review policy applies).
const decisionNone = "none"

// reviewQuery reads, in one call, the PR's review threads and whether each
// check context on its head commit is required. Field names follow GitHub's
// GraphQL reference (PullRequest.reviewThreads, StatusCheckRollup.contexts,
// CheckRun/StatusContext.isRequired(pullRequestNumber:)).
const reviewQuery = `query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){pullRequest(number:$n){` +
	`reviewThreads(first:100){nodes{isResolved isOutdated}} ` +
	`commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100){nodes{__typename ` +
	`... on CheckRun{name isRequired(pullRequestNumber:$n) checkSuite{workflowRun{workflow{name}}}} ` +
	`... on StatusContext{context isRequired(pullRequestNumber:$n)}}}}}}}}}}`

// Review is a pull request's review and merge state.
type Review struct {
	Decision        string   `json:"decision"`
	Requested       []string `json:"requested"`
	ThreadsOpen     int      `json:"threads_open"`
	ThreadsOutdated int      `json:"threads_outdated"`
	Mergeable       string   `json:"mergeable"`
	MergeState      string   `json:"merge_state"`
}

type rawReviewView struct {
	ReviewDecision string `json:"reviewDecision"`
	ReviewRequests []struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
	} `json:"reviewRequests"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	BaseRefOid       string `json:"baseRefOid"`
}

// repoRef names the repository a PR lives in, read from its URL.
type repoRef struct{ Host, Owner, Name string }

// checkKey identifies a check context: its name and, for an Actions run,
// its workflow name (empty for a status context or a non-Actions app).
type checkKey struct{ Name, Workflow string }

// FetchReview reads the review state of pr and which of its check contexts
// are required. pr.URL supplies owner and repo, so no extra call is made.
// The same view read fills pr.BaseOID.
func FetchReview(ctx context.Context, r Runner, pr *PR) (*Review, map[checkKey]bool, error) {
	repo, err := repoFromURL(pr.URL)
	if err != nil {
		return nil, nil, err
	}
	var view rawReviewView
	if err := ghJSON(ctx, r, &view, "pr", "view", strconv.Itoa(pr.Number), "--json", reviewFields); err != nil {
		return nil, nil, fmt.Errorf("review of PR #%d: %w", pr.Number, err)
	}
	var gql rawReviewGraphQL
	if err := ghJSON(ctx, r, &gql, reviewGraphQLArgs(repo, pr.Number)...); err != nil {
		return nil, nil, fmt.Errorf("review threads of PR #%d: %w", pr.Number, err)
	}
	node, err := gql.pullRequest()
	if err != nil {
		return nil, nil, fmt.Errorf("review threads of PR #%d: %w", pr.Number, err)
	}
	pr.BaseOID = view.BaseRefOid
	rv := reviewFromView(view)
	rv.ThreadsOpen, rv.ThreadsOutdated = countThreads(node)
	return rv, requiredContexts(node), nil
}

func reviewFromView(v rawReviewView) *Review {
	rv := &Review{Decision: v.ReviewDecision, Requested: []string{},
		Mergeable: v.Mergeable, MergeState: v.MergeStateStatus}
	if rv.Decision == "" {
		rv.Decision = decisionNone
	}
	if rv.Mergeable == "UNKNOWN" || rv.Mergeable == "" {
		rv.Mergeable = MergeableComputing
	}
	for _, req := range v.ReviewRequests {
		// gh exports a user as login, a team as name and slug.
		who := req.Login
		if who == "" {
			who = req.Slug
		}
		if who == "" {
			who = req.Name
		}
		if who != "" {
			rv.Requested = append(rv.Requested, who)
		}
	}
	return rv
}

// reviewGraphQLArgs is the gh argv (without "gh") for the review query.
// --hostname is added only for a host other than github.com.
func reviewGraphQLArgs(repo repoRef, n int) []string {
	args := []string{"api", "graphql"}
	if repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	return append(args, "-f", "query="+reviewQuery,
		"-F", "owner="+repo.Owner, "-F", "name="+repo.Name, "-F", "n="+strconv.Itoa(n))
}

// repoFromURL reads host, owner and repo from a PR URL of the form
// https://<host>/<owner>/<repo>/pull/<n>.
func repoFromURL(raw string) (repoRef, error) {
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "pull" && parts[0] != "" && parts[1] != "" {
			return repoRef{Host: u.Host, Owner: parts[0], Name: parts[1]}, nil
		}
	}
	return repoRef{}, fmt.Errorf("cannot read owner/repo from PR URL %q", raw)
}

// MarkRequired sets Check.Required for each check whose name and workflow
// match a required context.
func MarkRequired(checks []Check, required map[checkKey]bool) {
	for i := range checks {
		if required[checkKey{Name: checks[i].Name, Workflow: checks[i].Workflow}] {
			checks[i].Required = true
		}
	}
}

// attachReview reads s.PR's review into s and marks required checks. Any
// failure becomes a note and leaves s.Review nil; only a cancelled or
// expired ctx is returned as an error.
func attachReview(ctx context.Context, r Runner, s *Snapshot) error {
	rv, required, err := FetchReview(ctx, r, s.PR)
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), err)
		}
		s.Notes = append(s.Notes, "review not read: "+err.Error())
		return nil
	}
	s.Review, s.required = rv, required
	MarkRequired(s.Checks, required)
	return nil
}

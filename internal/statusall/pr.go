package statusall

import (
	"context"
	"errors"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// readPR fills the PR for the project's current branch through gitwatch:
// at most four gh calls (find, basics, checks, review). gh skipped, missing
// or failing marks only Sources["gh"]; no PR for the branch is "none".
func readPR(ctx context.Context, o Options, p *Project, dir string) {
	if o.GH == nil {
		p.Sources["gh"] = o.GHReason
		return
	}
	if p.Branch == "" {
		p.Sources["gh"] = SourceNone
		return
	}
	pr, err := fetchPR(ctx, o.GH(dir), p.Branch)
	switch {
	case errors.Is(err, gitwatch.ErrNoPR):
		p.Sources["gh"] = SourceNone
	case err != nil:
		p.Sources["gh"] = SourceNotAvailable + ": " + err.Error()
	default:
		p.PR, p.Sources["gh"] = pr, SourceOK
	}
}

func fetchPR(ctx context.Context, r gitwatch.Runner, branch string) (*PR, error) {
	n, err := gitwatch.ResolvePR(ctx, r, branch)
	if err != nil {
		return nil, err
	}
	basics, err := gitwatch.FetchPR(ctx, r, n)
	if err != nil {
		return nil, err
	}
	checks, err := gitwatch.FetchChecks(ctx, r, n)
	if err != nil {
		return nil, err
	}
	pr := &PR{Number: basics.Number, Title: basics.Title, State: basics.State, URL: basics.URL,
		Checks: gitwatch.CountChecks(checks), Failing: []string{}}
	for _, c := range checks {
		if c.Bucket == gitwatch.BucketFail {
			pr.Failing = append(pr.Failing, c.Name)
		}
	}
	if rv, err := gitwatch.FetchReviewState(ctx, r, n); err == nil {
		pr.Review, pr.Mergeable = rv.Decision, rv.Mergeable
	} else if ctx.Err() != nil {
		return nil, err
	}
	return pr, nil
}

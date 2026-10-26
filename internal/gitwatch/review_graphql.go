package gitwatch

import (
	"errors"
	"strings"
)

// rawReviewGraphQL is the response of reviewQuery, as gh api graphql prints it.
type rawReviewGraphQL struct {
	Data struct {
		Repository *struct {
			PullRequest *rawPRNode `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type rawPRNode struct {
	ReviewThreads struct {
		Nodes []struct {
			IsResolved bool `json:"isResolved"`
			IsOutdated bool `json:"isOutdated"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					Contexts struct {
						Nodes []rawContext `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// rawContext is one StatusCheckRollupContext: a CheckRun (name, checkSuite)
// or a StatusContext (context).
type rawContext struct {
	TypeName   string `json:"__typename"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	IsRequired bool   `json:"isRequired"`
	CheckSuite *struct {
		WorkflowRun *struct {
			Workflow struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
}

// pullRequest returns the PR node, or GraphQL's own error messages when the
// response carries no PR (gh prints them on stdout and exits 1).
func (g rawReviewGraphQL) pullRequest() (*rawPRNode, error) {
	if g.Data.Repository != nil && g.Data.Repository.PullRequest != nil {
		return g.Data.Repository.PullRequest, nil
	}
	msgs := make([]string, 0, len(g.Errors))
	for _, e := range g.Errors {
		msgs = append(msgs, e.Message)
	}
	if len(msgs) == 0 {
		return nil, errors.New("GraphQL response has no pull request")
	}
	return nil, errors.New(strings.Join(msgs, "; "))
}

// countThreads counts unresolved threads, and among them the outdated ones.
// Resolved threads are ignored.
func countThreads(pr *rawPRNode) (open, outdated int) {
	for _, t := range pr.ReviewThreads.Nodes {
		if t.IsResolved {
			continue
		}
		open++
		if t.IsOutdated {
			outdated++
		}
	}
	return open, outdated
}

// requiredContexts collects the required check contexts on the head commit.
func requiredContexts(pr *rawPRNode) map[checkKey]bool {
	required := map[checkKey]bool{}
	for _, c := range pr.Commits.Nodes {
		rollup := c.Commit.StatusCheckRollup
		if rollup == nil {
			continue
		}
		for _, ctxNode := range rollup.Contexts.Nodes {
			if ctxNode.IsRequired {
				required[ctxNode.key()] = true
			}
		}
	}
	return required
}

func (c rawContext) key() checkKey {
	if c.TypeName == "StatusContext" {
		return checkKey{Name: c.Context}
	}
	k := checkKey{Name: c.Name}
	if c.CheckSuite != nil && c.CheckSuite.WorkflowRun != nil {
		k.Workflow = c.CheckSuite.WorkflowRun.Workflow.Name
	}
	return k
}

package gitwatch

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Base and release-workflow states.
const (
	BaseGreen   = "green"
	BaseRed     = "red"
	BasePending = "pending"
	BaseNone    = "none"
)

// DefaultBase is the base branch assumed when there is no pull request.
const DefaultBase = "main"

// baseRunLimit is how many recent base-branch runs are read.
const baseRunLimit = "20"

// baseRunFields is the --json field list asked of gh run list for the base.
const baseRunFields = "headSha,status,conclusion,workflowName,url,createdAt"

// failedConclusions are the run conclusions that make a workflow red.
var failedConclusions = map[string]bool{"failure": true, "startup_failure": true, "timed_out": true}

// Base is the CI state of the branch a pull request merges into. State is
// red when any workflow's latest completed run failed, else pending when any
// workflow's newest run is still going, else green; none when nothing ran.
// RedToo (base_red_too) lets a reader tell a PR's own failure from a broken
// base: it is set when the base is red or when a run at the PR's base
// commit (PR.BaseOID) failed. Runs holds the latest completed run per workflow.
type Base struct {
	Name    string    `json:"name"`
	State   string    `json:"state"`
	RedToo  bool      `json:"base_red_too"`
	Failing []BaseRun `json:"failing"`
	Runs    []BaseRun `json:"runs"`
}

// BaseRun is one workflow run on the base branch.
type BaseRun struct {
	Workflow   string    `json:"workflow"`
	HeadSha    string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"created_at"`
}

type rawRun struct {
	HeadSha      string    `json:"headSha"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	WorkflowName string    `json:"workflowName"`
	URL          string    `json:"url"`
	CreatedAt    time.Time `json:"createdAt"`
}

// BaseName is the PR's base branch, or DefaultBase with no PR.
func BaseName(pr *PR) string {
	if pr == nil || pr.Base == "" {
		return DefaultBase
	}
	return pr.Base
}

// FetchBase reads the recent runs on branch name and summarises them.
// prBaseOID is the PR's base commit ("" with no PR); a failed run there also
// sets RedToo.
func FetchBase(ctx context.Context, r Runner, name, prBaseOID string) (*Base, error) {
	var raw []rawRun
	err := ghJSON(ctx, r, &raw, "run", "list", "--branch", name, "--limit", baseRunLimit, "--json", baseRunFields)
	if err != nil {
		return nil, fmt.Errorf("base %s runs: %w", name, err)
	}
	b := summariseBase(raw)
	b.Name = name
	if failedAt(raw, prBaseOID) {
		b.RedToo = true
	}
	return b, nil
}

// summariseBase keeps the latest completed run per workflow and notes any
// workflow whose newest run is still going.
func summariseBase(raw []rawRun) *Base {
	sort.SliceStable(raw, func(i, j int) bool { return raw[i].CreatedAt.After(raw[j].CreatedAt) })
	b := &Base{State: BaseNone, Failing: []BaseRun{}, Runs: []BaseRun{}}
	seen, done, pending := map[string]bool{}, map[string]bool{}, false
	for _, run := range raw {
		if !seen[run.WorkflowName] && run.Status != "completed" {
			pending = true
		}
		seen[run.WorkflowName] = true
		if run.Status != "completed" || done[run.WorkflowName] {
			continue
		}
		done[run.WorkflowName] = true
		br := BaseRun{Workflow: run.WorkflowName, HeadSha: run.HeadSha, Status: run.Status,
			Conclusion: run.Conclusion, URL: run.URL, CreatedAt: run.CreatedAt}
		b.Runs = append(b.Runs, br)
		if failedConclusions[run.Conclusion] {
			b.Failing = append(b.Failing, br)
		}
	}
	sort.SliceStable(b.Failing, func(i, j int) bool { return b.Failing[i].Workflow < b.Failing[j].Workflow })
	switch {
	case len(b.Failing) > 0:
		b.State, b.RedToo = BaseRed, true
	case pending:
		b.State = BasePending
	case len(raw) > 0:
		b.State = BaseGreen
	}
	return b
}

// failedAt reports whether any completed run at commit oid failed.
func failedAt(raw []rawRun, oid string) bool {
	for _, run := range raw {
		if oid != "" && run.HeadSha == oid && run.Status == "completed" && failedConclusions[run.Conclusion] {
			return true
		}
	}
	return false
}

// addBaseAndRelease adds the base CI and release sections to s. A failed
// read leaves its section nil and adds a note; only a cancelled or expired
// ctx is an error.
func addBaseAndRelease(ctx context.Context, r Runner, s *Snapshot) (*Snapshot, error) {
	name := BaseName(s.PR)
	oid := ""
	if s.PR != nil {
		oid = s.PR.BaseOID
	}
	base, err := FetchBase(ctx, r, name, oid)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		s.Notes = append(s.Notes, err.Error())
	}
	s.Base = base
	rel, notes, err := FetchRelease(ctx, r, name, releaseWorkflowSetting())
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		notes = append(notes, err.Error())
	}
	s.Release, s.Notes = rel, append(s.Notes, notes...)
	return s, nil
}

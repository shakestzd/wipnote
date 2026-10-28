package gitwatch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// ErrNoPR means no pull request matched the lookup.
var ErrNoPR = errors.New("no pull request found")

// prFields is the --json field list asked of gh pr view.
const prFields = "number,title,headRefName,baseRefName,state,url"

// PR is the basics of a pull request. Later sections (reviews and so on)
// hang off Snapshot rather than growing this struct's JSON shape.
type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	State  string `json:"state"`
	URL    string `json:"url"`
	// BaseOID is the base commit (baseRefOid), filled by FetchReview's read.
	BaseOID string `json:"base_oid"`
}

type rawPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	State       string `json:"state"`
	URL         string `json:"url"`
}

type prNumber struct {
	Number int `json:"number"`
}

// ResolvePR turns a lookup argument into a PR number: all digits is that
// number; any other text is a head branch; empty is the current branch's
// PR, else the newest open PR of the signed-in user.
func ResolvePR(ctx context.Context, r Runner, arg string) (int, error) {
	if isDigits(arg) {
		return strconv.Atoi(arg)
	}
	if arg != "" {
		return ResolveBranchPR(ctx, r, arg)
	}
	var cur prNumber
	err := ghJSON(ctx, r, &cur, "pr", "view", "--json", "number")
	if errors.Is(err, ErrNotAvailable) {
		return 0, err
	}
	if err == nil && cur.Number > 0 {
		return cur.Number, nil
	}
	return firstListed(ctx, r, "--author", "@me", "--state", "open")
}

// ResolveBranchPR is the PR whose head is branch, in any state. Unlike
// ResolvePR it never reads an all-digits argument as a PR number, so callers
// holding a branch name (one may be called "123") use it. An empty branch is
// ErrNoPR.
func ResolveBranchPR(ctx context.Context, r Runner, branch string) (int, error) {
	if branch == "" {
		return 0, ErrNoPR
	}
	return firstListed(ctx, r, "--head", branch, "--state", "all")
}

// firstListed runs gh pr list with filter and returns the first number.
func firstListed(ctx context.Context, r Runner, filter ...string) (int, error) {
	args := append([]string{"pr", "list"}, filter...)
	args = append(args, "--limit", "1", "--json", "number")
	var list []prNumber
	if err := ghJSON(ctx, r, &list, args...); err != nil {
		return 0, err
	}
	if len(list) == 0 || list[0].Number <= 0 {
		return 0, ErrNoPR
	}
	return list[0].Number, nil
}

// FetchPR reads the basics of PR number n.
func FetchPR(ctx context.Context, r Runner, n int) (*PR, error) {
	var raw rawPR
	if err := ghJSON(ctx, r, &raw, "pr", "view", strconv.Itoa(n), "--json", prFields); err != nil {
		return nil, fmt.Errorf("PR #%d: %w", n, err)
	}
	return &PR{Number: raw.Number, Title: raw.Title, Branch: raw.HeadRefName,
		Base: raw.BaseRefName, State: raw.State, URL: raw.URL}, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

package gitwatch

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// Release states.
const (
	ReleasePublished = "published"
	ReleaseNone      = "none"
)

const (
	releaseFields    = "tagName,publishedAt,url"
	releaseRunFields = "status,conclusion,url,createdAt"
	// releaseNotFound is gh release view's stderr when the repo has none.
	releaseNotFound = "release not found"
)

// Release is the repository's latest release, the commits on the base since
// its tag, and the release workflow's latest run. State none (no release
// yet) is a normal answer, not an error. CommitsSince and Workflow are nil
// when unknown or when there is no release workflow.
type Release struct {
	State        string           `json:"state"`
	Tag          string           `json:"tag,omitempty"`
	PublishedAt  *time.Time       `json:"published_at,omitempty"`
	URL          string           `json:"url,omitempty"`
	CommitsSince *int             `json:"commits_since,omitempty"`
	Workflow     *ReleaseWorkflow `json:"workflow,omitempty"`
}

// ReleaseWorkflow is the latest run of the release workflow. State is
// green, red, pending, none (never ran) or the conclusion as gh gives it.
type ReleaseWorkflow struct {
	Name       string     `json:"name"`
	State      string     `json:"state"`
	Status     string     `json:"status,omitempty"`
	Conclusion string     `json:"conclusion,omitempty"`
	URL        string     `json:"url,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
}

type rawRelease struct {
	TagName     string    `json:"tagName"`
	PublishedAt time.Time `json:"publishedAt"`
	URL         string    `json:"url"`
}

type rawWorkflow struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// releaseWorkflowSetting gives BuildSnapshot the configured release
// workflow name; tests replace it so they never read a real config.
var releaseWorkflowSetting = func() string {
	dir, _ := paths.ResolveProjectDir(paths.ProjectDirOptions{})
	return ReleaseWorkflowSetting(dir)
}

// ReleaseWorkflowSetting reads gitwatch.release_workflow from the layered
// config (user < project < local); "" when unset.
func ReleaseWorkflowSetting(projectDir string) string {
	var cfg struct {
		ReleaseWorkflow string `json:"release_workflow"`
	}
	paths.ReadLayeredConfig(projectDir, paths.LayeredOptions{Section: "gitwatch"}, &cfg)
	return strings.TrimSpace(cfg.ReleaseWorkflow)
}

// FetchRelease reads the latest release; with a tag, the commits on base
// since it and the release workflow's latest run (workflow, or the default
// one when empty). The error is for the release read; the later reads fail
// into notes and leave their field nil.
func FetchRelease(ctx context.Context, r Runner, base, workflow string) (*Release, []string, error) {
	var raw rawRelease
	err := ghJSON(ctx, r, &raw, "release", "view", "--json", releaseFields)
	if err != nil && strings.Contains(err.Error(), releaseNotFound) {
		return &Release{State: ReleaseNone}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("release: %w", err)
	}
	rel := &Release{State: ReleasePublished, Tag: raw.TagName, PublishedAt: realTime(raw.PublishedAt), URL: raw.URL}
	var notes []string
	if n, err := CommitsSince(ctx, r, raw.TagName, base); err != nil {
		notes = append(notes, fmt.Sprintf("commits since %s: %v", raw.TagName, err))
	} else {
		rel.CommitsSince = &n
	}
	if rel.Workflow, err = fetchReleaseWorkflow(ctx, r, workflow); err != nil {
		notes = append(notes, fmt.Sprintf("release workflow: %v", err))
	}
	return rel, notes, nil
}

// fetchReleaseWorkflow reads the latest run of name, finding the default
// release workflow first when name is empty. No workflow is nil, nil.
func fetchReleaseWorkflow(ctx context.Context, r Runner, name string) (*ReleaseWorkflow, error) {
	if name == "" {
		var err error
		if name, err = DefaultReleaseWorkflow(ctx, r); err != nil || name == "" {
			return nil, err
		}
	}
	var runs []rawRun
	err := ghJSON(ctx, r, &runs, "run", "list", "--workflow", name, "--limit", "1", "--json", releaseRunFields)
	if err != nil {
		return nil, err
	}
	w := &ReleaseWorkflow{Name: name, State: BaseNone}
	if len(runs) > 0 {
		run := runs[0]
		w.State, w.Status, w.Conclusion, w.URL = runState(run.Status, run.Conclusion), run.Status, run.Conclusion, run.URL
		w.CreatedAt = realTime(run.CreatedAt)
	}
	return w, nil
}

// DefaultReleaseWorkflow is the name of the first workflow whose file name
// contains "release", or "" when there is none.
func DefaultReleaseWorkflow(ctx context.Context, r Runner) (string, error) {
	var list []rawWorkflow
	if err := ghJSON(ctx, r, &list, "workflow", "list", "--json", "name,path"); err != nil {
		return "", err
	}
	for _, w := range list {
		if strings.Contains(strings.ToLower(path.Base(w.Path)), "release") {
			return w.Name, nil
		}
	}
	return "", nil
}

// CommitsSince counts commits in tag..origin/<base>, falling back to
// tag..<base> when the remote-tracking branch is missing.
func CommitsSince(ctx context.Context, r Runner, tag, base string) (int, error) {
	var last string
	for _, ref := range []string{"origin/" + base, base} {
		stdout, stderr, code, err := r.Run(ctx, "git", "rev-list", "--count", tag+".."+ref)
		if err != nil {
			return 0, err
		}
		if code == 0 {
			return strconv.Atoi(strings.TrimSpace(string(stdout)))
		}
		last = firstLine(stderr)
	}
	return 0, fmt.Errorf("git rev-list: %s", last)
}

// runState maps a run's status and conclusion to green, red or pending;
// another conclusion (cancelled, skipped) is returned as is.
func runState(status, conclusion string) string {
	switch {
	case status != "completed":
		return BasePending
	case conclusion == "success":
		return BaseGreen
	case failedConclusions[conclusion]:
		return BaseRed
	default:
		return conclusion
	}
}

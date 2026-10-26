package gitwatch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	releaseViewArgv  = "gh release view --json " + releaseFields
	workflowListArgv = "gh workflow list --json name,path"
	publishRunsArgv  = "gh run list --workflow Publish --limit 1 --json " + releaseRunFields
	countOriginArgv  = "git rev-list --count v1.4.0..origin/main"
	countLocalArgv   = "git rev-list --count v1.4.0..main"
)

func releaseFake(t *testing.T) *FakeRunner {
	return &FakeRunner{Results: map[string]FakeResult{
		releaseViewArgv:  {Stdout: fixture(t, "release_view.json")},
		workflowListArgv: {Stdout: fixture(t, "workflow_list.json")},
		publishRunsArgv:  {Stdout: fixture(t, "release_runs.json")},
		countOriginArgv:  {Stdout: "4\n"},
	}}
}

func TestFetchRelease_Present(t *testing.T) {
	rel, notes, err := FetchRelease(context.Background(), releaseFake(t), "main", "")
	if err != nil || len(notes) != 0 {
		t.Fatalf("err %v, notes %q", err, notes)
	}
	if rel.State != ReleasePublished || rel.Tag != "v1.4.0" || rel.PublishedAt == nil || rel.URL == "" {
		t.Errorf("release: %+v", rel)
	}
	if rel.CommitsSince == nil || *rel.CommitsSince != 4 {
		t.Errorf("commits since: %v", rel.CommitsSince)
	}
	w := rel.Workflow
	if w == nil || w.Name != "Publish" || w.State != BaseGreen || w.URL != "https://example.test/runs/90" {
		t.Errorf("release workflow: %+v", w)
	}
}

func TestFetchRelease_Absent(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		releaseViewArgv:  {Stderr: "release not found\n", Exit: 1},
		workflowListArgv: {Stdout: `[{"name":"CI","path":".github/workflows/ci.yml"}]`},
	}}
	rel, notes, err := FetchRelease(context.Background(), f, "main", "")
	if err != nil || len(notes) != 0 {
		t.Fatalf("no release must be none, not an error: %v, %q", err, notes)
	}
	if rel.State != ReleaseNone || rel.Tag != "" || rel.CommitsSince != nil || rel.Workflow != nil {
		t.Errorf("absent release: %+v", rel)
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "git ") {
			t.Errorf("no tag, so no commit count: %q", c)
		}
	}
}

func TestFetchRelease_GhFailureIsError(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{releaseViewArgv: {Stderr: "HTTP 502: Bad Gateway", Exit: 1}}}
	rel, _, err := FetchRelease(context.Background(), f, "main", "")
	if err == nil || rel != nil {
		t.Errorf("want an error, got %+v, %v", rel, err)
	}
}

func TestFetchRelease_PartsFailIntoNotes(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{
		releaseViewArgv:  {Stdout: fixture(t, "release_view.json")},
		workflowListArgv: {Stderr: "HTTP 403: Forbidden", Exit: 1},
		countOriginArgv:  {Stderr: "fatal: bad revision", Exit: 128},
		countLocalArgv:   {Stderr: "fatal: bad revision", Exit: 128},
	}}
	rel, notes, err := FetchRelease(context.Background(), f, "main", "")
	if err != nil || rel == nil || rel.Tag != "v1.4.0" {
		t.Fatalf("release itself is fine: %+v, %v", rel, err)
	}
	if rel.CommitsSince != nil || rel.Workflow != nil || len(notes) != 2 {
		t.Errorf("want two notes and nil parts, got %+v, %q", rel, notes)
	}
}

func TestReleaseWorkflow_DefaultDetection(t *testing.T) {
	name, err := DefaultReleaseWorkflow(context.Background(), releaseFake(t))
	if err != nil || name != "Publish" {
		t.Errorf("want the first workflow whose file name contains release, got %q, %v", name, err)
	}
	f := &FakeRunner{Results: map[string]FakeResult{workflowListArgv: {Stdout: `[{"name":"Release notes","path":".github/workflows/notes.yml"}]`}}}
	if name, err := DefaultReleaseWorkflow(context.Background(), f); err != nil || name != "" {
		t.Errorf("a name match alone is not a release workflow: %q, %v", name, err)
	}
}

func TestFetchRelease_ConfiguredWorkflowSkipsDetection(t *testing.T) {
	f := releaseFake(t)
	f.Results["gh run list --workflow Ship it --limit 1 --json "+releaseRunFields] = FakeResult{Stdout: `[]`}
	rel, notes, err := FetchRelease(context.Background(), f, "main", "Ship it")
	if err != nil || len(notes) != 0 {
		t.Fatalf("%v %q", err, notes)
	}
	if rel.Workflow == nil || rel.Workflow.Name != "Ship it" || rel.Workflow.State != BaseNone {
		t.Errorf("configured workflow with no runs: %+v", rel.Workflow)
	}
	for _, c := range f.Calls {
		if c == workflowListArgv {
			t.Errorf("configured name must not list workflows")
		}
	}
}

func TestReleaseWorkflowSetting(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	if got := ReleaseWorkflowSetting(project); got != "" {
		t.Errorf("unset: %q", got)
	}
	writeConfig(t, filepath.Join(home, ".wipnote", "config.json"), `{"gitwatch":{"release_workflow":"User one"}}`)
	writeConfig(t, filepath.Join(project, ".wipnote", "config.json"), `{"gitwatch":{"release_workflow":"Publish"}}`)
	if got := ReleaseWorkflowSetting(project); got != "Publish" {
		t.Errorf("project layer beats user layer: %q", got)
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCommitsSince_TempRepo counts commits with real git: a tag, three
// commits after it on main, and origin/main one commit behind main.
func TestCommitsSince_TempRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", dir, "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
			"-c", "user.name=Test", "-c", "user.email=test@example.test"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	for i, msg := range []string{"one", "two", "three", "four"} {
		git("commit", "-q", "--allow-empty", "-m", msg)
		if i == 0 {
			git("tag", "v1.4.0")
		}
	}
	t.Chdir(dir)
	n, err := CommitsSince(context.Background(), ExecRunner{}, "v1.4.0", "main")
	if err != nil || n != 3 {
		t.Fatalf("no origin/main falls back to main: got %d, %v", n, err)
	}
	git("update-ref", "refs/remotes/origin/main", git("rev-parse", "main~1"))
	n, err = CommitsSince(context.Background(), ExecRunner{}, "v1.4.0", "main")
	if err != nil || n != 2 {
		t.Fatalf("origin/main wins: got %d, %v", n, err)
	}
	if _, err := CommitsSince(context.Background(), ExecRunner{}, "v9.9.9", "main"); err == nil {
		t.Errorf("unknown tag must be an error")
	}
}

// snapshotWithBase is the slice-1 snapshot fake plus base and release reads.
func snapshotWithBase(t *testing.T) *FakeRunner {
	f := snapshotFake(t)
	for k, v := range releaseFake(t).Results {
		f.Results[k] = v
	}
	f.Results[baseMainArgv] = FakeResult{Stdout: fixture(t, "base_runs_red.json")}
	return f
}

func noReleaseSetting(t *testing.T) {
	old := releaseWorkflowSetting
	releaseWorkflowSetting = func() string { return "" }
	t.Cleanup(func() { releaseWorkflowSetting = old })
}

func TestBuildSnapshot_BaseAndRelease(t *testing.T) {
	noReleaseSetting(t)
	s, err := BuildSnapshot(context.Background(), snapshotWithBase(t), "42", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if s.Base == nil || s.Base.Name != "main" || !s.Base.RedToo {
		t.Errorf("base: %+v", s.Base)
	}
	if s.Release == nil || s.Release.Tag != "v1.4.0" || len(s.Notes) != 0 {
		t.Errorf("release: %+v, notes %q", s.Release, s.Notes)
	}
}

func TestBuildSnapshot_BaseFailureIsANote(t *testing.T) {
	noReleaseSetting(t)
	f := snapshotWithBase(t)
	f.Results[baseMainArgv] = FakeResult{Stderr: "HTTP 403: Forbidden", Exit: 1}
	f.Results[releaseViewArgv] = FakeResult{Stderr: "HTTP 403: Forbidden", Exit: 1}
	s, err := BuildSnapshot(context.Background(), f, "42", fixedNow)
	if err != nil {
		t.Fatalf("base/release failure must not fail the snapshot: %v", err)
	}
	if !s.Available || s.PR == nil || s.Base != nil || s.Release != nil || len(s.Notes) != 2 {
		t.Errorf("want nil base/release and two notes: %+v", s)
	}
}

func TestBuildSnapshot_NoPRStillReadsMain(t *testing.T) {
	noReleaseSetting(t)
	f := snapshotWithBase(t)
	f.Results["gh pr list --head nobody --state all --limit 1 --json number"] = FakeResult{Stdout: `[]`}
	s, err := BuildSnapshot(context.Background(), f, "nobody", fixedNow)
	if err != nil || s.PR != nil || s.Base == nil || s.Base.Name != "main" {
		t.Errorf("no PR defaults the base to main: %+v, %v", s, err)
	}
}

func TestBuildSnapshot_CancelledCtxIsError(t *testing.T) {
	noReleaseSetting(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := snapshotWithBase(t)
	f.Results[baseMainArgv] = FakeResult{Err: context.Canceled}
	_, err := BuildSnapshot(ctx, f, "42", fixedNow)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx must be an error, got %v", err)
	}
}

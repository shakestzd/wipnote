package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// noGhRunner answers every call as if gh were not installed. TestMain makes
// it the package's linkRunner so no test ever reaches a real gh.
type noGhRunner struct{}

func (noGhRunner) Run(context.Context, string, ...string) ([]byte, []byte, int, error) {
	return nil, nil, -1, gitwatch.ErrNotAvailable
}

const (
	linkTestID  = "bug-aabbccdd"
	linkFieldsT = "number,title,headRefName,state,updatedAt,url"
	linkPRRow   = `{"number":12,"title":"Fix (` + linkTestID + `)","headRefName":"x","state":"OPEN",` +
		`"url":"https://github.com/acme/widgets/pull/12","updatedAt":"2026-10-20T10:00:00Z"}`
	linkRed   = `[{"name":"unit","state":"FAILURE","bucket":"fail","workflow":"CI","event":"pull_request"},{"name":"e2e","state":"QUEUED","bucket":"pending","workflow":"CI","event":"pull_request"}]`
	linkGreen = `[{"name":"unit","state":"SUCCESS","bucket":"pass","workflow":"CI","event":"pull_request"}]`
	linkClean = `{"reviewDecision":"APPROVED","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN"}`
	linkGQL   = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`
)

// linkTestFake finds PR 12 for linkTestID with the given checks and a clean
// review. The GraphQL argv is matched by prefix through linkFakeRunner.
func linkTestFake(checks string) *linkFakeRunner {
	return &linkFakeRunner{f: &gitwatch.FakeRunner{Results: map[string]gitwatch.FakeResult{
		"gh pr list --state all --search " + linkTestID + " in:title --json " + linkFieldsT + " --limit 20": {Stdout: "[" + linkPRRow + "]"},
		"gh pr list --state open --json " + linkFieldsT + " --limit 100":                                    {Stdout: "[]"},
		"gh pr checks 12 --json name,state,bucket,startedAt,completedAt,link,workflow,event":                {Stdout: checks},
		"gh pr view 12 --json reviewDecision,reviewRequests,mergeable,mergeStateStatus,baseRefOid":          {Stdout: linkClean},
		"gh pr view 12 --json number,title,headRefName,baseRefName,state,url":                               {Stdout: linkPRRow},
	}}}
}

// linkFakeRunner wraps FakeRunner, answering any gh api graphql call with
// an empty thread list (its argv embeds the whole query text).
type linkFakeRunner struct{ f *gitwatch.FakeRunner }

func (l *linkFakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	if len(args) > 1 && args[0] == "api" && args[1] == "graphql" {
		return []byte(linkGQL), nil, 0, nil
	}
	return l.f.Run(ctx, name, args...)
}

func useLinkRunner(t *testing.T, r gitwatch.Runner) {
	t.Helper()
	old := linkRunner
	linkRunner = r
	t.Cleanup(func() { linkRunner = old })
}

func TestWriteGatePRWarning(t *testing.T) {
	var out bytes.Buffer
	writeGatePRWarning(&out, linkTestFake(linkRed), linkTestID)
	if !strings.Contains(out.String(), "WARN: linked PR #12") || !strings.Contains(out.String(), "pull/12") {
		t.Errorf("red PR: want a WARN line, got %q", out.String())
	}
	out.Reset()
	writeGatePRWarning(&out, linkTestFake(linkGreen), linkTestID)
	if out.Len() != 0 {
		t.Errorf("green PR: want no line, got %q", out.String())
	}
	writeGatePRWarning(&out, noGhRunner{}, linkTestID)
	writeGatePRWarning(&out, linkTestFake(linkRed), "")
	if out.Len() != 0 {
		t.Errorf("no gh or no work item: want no line, got %q", out.String())
	}
}

func TestCIStatus_WorkItemResolvesPR(t *testing.T) {
	var out bytes.Buffer
	if err := runCIStatusWorkItem(context.Background(), &out, linkTestFake(linkRed), linkTestID, true); err != nil {
		t.Fatal(err)
	}
	var snap gitwatch.Snapshot
	if err := json.Unmarshal(out.Bytes(), &snap); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if snap.PR == nil || snap.PR.Number != 12 || snap.Counts.Fail != 1 || snap.WorkItem != linkTestID {
		t.Errorf("snapshot: %+v", snap)
	}
	out.Reset()
	if err := runCIStatusWorkItem(context.Background(), &out, noGhRunner{}, linkTestID, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not available") {
		t.Errorf("no gh: %q", out.String())
	}
}

func TestCIStatusCmd_RejectsWorkItemWithArg(t *testing.T) {
	cmd := ciStatusCmd()
	cmd.SetArgs([]string{"7", "--work-item", linkTestID})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("a PR argument and --work-item together must be refused")
	}
}

func TestFeatureShow_PrintsLinkedPRLine(t *testing.T) {
	tmpDir := makeShowFixture(t, "bugs", "bug-aabbccdd.html", sampleBugHTML)
	projectDirFlag = tmpDir
	defer func() { projectDirFlag = "" }()

	useLinkRunner(t, linkTestFake(linkRed))
	out := captureStdout(t, func() {
		if err := runWiShowWithFormat(linkTestID, "text"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "PR #12 OPEN · CI 0 pass, 1 fail, 1 pending") {
		t.Errorf("show output missing the PR line:\n%s", out)
	}

	useLinkRunner(t, noGhRunner{})
	out = captureStdout(t, func() {
		if err := runWiShowWithFormat(linkTestID, "text"); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "PR #") || !strings.Contains(out, "Sample Bug Title") {
		t.Errorf("no gh: show must print as before, no PR line:\n%s", out)
	}
}

// The gate end to end: a red linked PR adds a WARN line and the gate still
// passes; without gh there is no line and the gate still passes.
func TestCheckCmd_GateWarnsOnRedLinkedPR(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real Go gate commands")
	}
	cases := []struct {
		name   string
		runner gitwatch.Runner
		warn   bool
	}{
		{"red", linkTestFake(linkRed), true},
		{"no gh", noGhRunner{}, false},
	}
	for _, c := range cases {
		projectRoot := setupGateTestProject(t)
		seedGateWorkItemArtifact(t, projectRoot, linkTestID, "in-progress")
		projectDirFlag = projectRoot
		useLinkRunner(t, c.runner)
		cmd := checkCmd()
		cmd.SetArgs([]string{"--gate", "--work-item", linkTestID})
		var execErr error
		captured := captureStdout(t, func() { execErr = cmd.Execute() })
		projectDirFlag = ""
		if execErr != nil {
			t.Fatalf("%s: the gate must still pass (warn only), got %v\n%s", c.name, execErr, captured)
		}
		if got := strings.Contains(captured, "WARN: linked PR #12"); got != c.warn {
			t.Errorf("%s: WARN line present = %v, want %v\n%s", c.name, got, c.warn, captured)
		}
	}
}

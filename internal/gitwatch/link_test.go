package gitwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const linkID = "feat-0a1b2c3d"

var (
	titleArgv  = "gh pr list --state all --search " + linkID + " in:title --json " + linkFields + " --limit 20"
	branchArgv = "gh pr list --state open --json " + linkFields + " --limit 100"
	linkChecks = "gh pr checks 12 --json " + checkFields
	linkView   = "gh pr view 12 --json " + reviewFields
	linkGQL    = "gh " + strings.Join(reviewGraphQLArgs(repoRef{Host: "github.com", Owner: "acme", Name: "widgets"}, 12), " ")
)

const prRow12 = `{"number":12,"title":"Do thing (` + linkID + `)","headRefName":"x","state":"OPEN",` +
	`"url":"https://github.com/acme/widgets/pull/12","updatedAt":"2026-10-20T10:00:00Z"}`

const (
	checksGreen  = `[{"name":"unit","state":"SUCCESS","bucket":"pass","workflow":"CI","event":"pull_request"}]`
	checksRed    = `[{"name":"unit","state":"FAILURE","bucket":"fail","workflow":"CI","event":"pull_request"},{"name":"lint","state":"CANCELLED","bucket":"cancel","workflow":"CI","event":"pull_request"}]`
	checksMixed  = `[{"name":"a","state":"SUCCESS","bucket":"pass","workflow":"CI","event":"pull_request"},{"name":"b","state":"FAILURE","bucket":"fail","workflow":"CI","event":"pull_request"},{"name":"c","state":"QUEUED","bucket":"pending","workflow":"CI","event":"pull_request"}]`
	viewClean    = `{"reviewDecision":"APPROVED","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN"}`
	viewConflict = `{"reviewDecision":"APPROVED","mergeable":"CONFLICTING","mergeStateStatus":"DIRTY"}`
	gqlNoThreads = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":true}]}}}}}`
	gqlThreads   = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":false},{"isResolved":false,"isOutdated":true}]}}}}}`
)

// linkFake answers a lookup that finds PR 12 by title, with the given
// checks, review view and GraphQL threads.
func linkFake(checks, view, gql string) *FakeRunner {
	return &FakeRunner{Results: map[string]FakeResult{
		titleArgv:  {Stdout: "[" + prRow12 + "]"},
		branchArgv: {Stdout: "[]"},
		linkChecks: {Stdout: checks},
		linkView:   {Stdout: view},
		linkGQL:    {Stdout: gql},
	}}
}

func TestFindPRForWorkItem_NewestWins(t *testing.T) {
	older := `{"number":3,"title":"old ` + linkID + `","headRefName":"a","state":"MERGED","url":"u3","updatedAt":"2026-09-01T00:00:00Z"}`
	newer := `{"number":9,"title":"new ` + linkID + `","headRefName":"b","state":"OPEN","url":"u9","updatedAt":"2026-10-01T00:00:00Z"}`
	f := &FakeRunner{Results: map[string]FakeResult{
		titleArgv:  {Stdout: "[" + older + "," + newer + "]"},
		branchArgv: {Stdout: "[]"},
	}}
	pr, err := FindPRForWorkItem(context.Background(), f, linkID)
	if err != nil || pr == nil || pr.Number != 9 {
		t.Fatalf("want newest PR #9, got %+v, %v", pr, err)
	}
	// Two-sided: when the older one is updated last, it wins.
	older = strings.Replace(older, "2026-09-01", "2026-10-25", 1)
	f.Results[titleArgv] = FakeResult{Stdout: "[" + newer + "," + older + "]"}
	pr, err = FindPRForWorkItem(context.Background(), f, linkID)
	if err != nil || pr.Number != 3 {
		t.Fatalf("want #3 once it is the newest, got %+v, %v", pr, err)
	}
}

func TestFindPRForWorkItem_IDInBranchOnly(t *testing.T) {
	branchRow := `{"number":21,"title":"Untitled work","headRefName":"wip/` + linkID + `-x","state":"OPEN","url":"u21","updatedAt":"2026-10-02T00:00:00Z"}`
	other := `{"number":22,"title":"Other","headRefName":"wip/feat-99999999","state":"OPEN","url":"u22","updatedAt":"2026-10-03T00:00:00Z"}`
	f := &FakeRunner{Results: map[string]FakeResult{
		titleArgv:  {Stdout: "[]"},
		branchArgv: {Stdout: "[" + branchRow + "," + other + "]"},
	}}
	pr, err := FindPRForWorkItem(context.Background(), f, linkID)
	if err != nil || pr.Number != 21 {
		t.Fatalf("want branch-only match #21, got %+v, %v", pr, err)
	}
	f.Results[branchArgv] = FakeResult{Stdout: "[" + other + "]"}
	if _, err := FindPRForWorkItem(context.Background(), f, linkID); !errors.Is(err, ErrNoPR) {
		t.Fatalf("no title or branch match: want ErrNoPR, got %v", err)
	}
}

// A search hit whose title and branch do not contain the id (GitHub search
// matches tokens) is not a link.
func TestFindPRForWorkItem_RejectsTokenOnlyHits(t *testing.T) {
	fuzzy := `{"number":5,"title":"feat 0a1b2c3d nearby","headRefName":"z","state":"OPEN","url":"u5","updatedAt":"2026-10-01T00:00:00Z"}`
	f := &FakeRunner{Results: map[string]FakeResult{
		titleArgv:  {Stdout: "[" + fuzzy + "]"},
		branchArgv: {Stdout: "[]"},
	}}
	if _, err := FindPRForWorkItem(context.Background(), f, linkID); !errors.Is(err, ErrNoPR) {
		t.Fatalf("want ErrNoPR, got %v", err)
	}
}

func TestFindPRForWorkItem_NotAvailable(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Err: ErrNotAvailable}}}
	if _, err := FindPRForWorkItem(context.Background(), f, linkID); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("want ErrNotAvailable, got %v", err)
	}
	if len(f.Calls) != 1 {
		t.Errorf("no second call once gh is unavailable, calls %q", f.Calls)
	}
}

func TestGateWarning_RedPR(t *testing.T) {
	got := GateWarning(context.Background(), linkFake(checksRed, viewClean, gqlNoThreads), linkID, time.Second)
	for _, want := range []string{"WARN", "#12", "1 failed check,", "1 cancelled check —", "https://github.com/acme/widgets/pull/12"} {
		if !strings.Contains(got, want) {
			t.Errorf("red PR warning missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "conflict") || strings.Contains(got, "thread") {
		t.Errorf("only the CI reason applies: %q", got)
	}
}

func TestGateWarning_GreenPRNoLine(t *testing.T) {
	if got := GateWarning(context.Background(), linkFake(checksGreen, viewClean, gqlNoThreads), linkID, time.Second); got != "" {
		t.Errorf("green PR must print no line, got %q", got)
	}
}

func TestGateWarning_Conflict(t *testing.T) {
	got := GateWarning(context.Background(), linkFake(checksGreen, viewConflict, gqlNoThreads), linkID, time.Second)
	if !strings.Contains(got, "merge conflict") || !strings.Contains(got, "pull/12") {
		t.Errorf("conflict warning: %q", got)
	}
	if strings.Contains(got, "failed") {
		t.Errorf("checks are green: %q", got)
	}
}

func TestGateWarning_OpenThreads(t *testing.T) {
	got := GateWarning(context.Background(), linkFake(checksGreen, viewClean, gqlThreads), linkID, time.Second)
	if !strings.Contains(got, "2 unresolved review threads") {
		t.Errorf("threads warning: %q", got)
	}
}

func TestGateWarning_AllReasons(t *testing.T) {
	got := GateWarning(context.Background(), linkFake(checksRed, viewConflict, gqlThreads), linkID, time.Second)
	for _, want := range []string{"1 failed check,", "merge conflict", "2 unresolved review threads"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %q", want, got)
		}
	}
}

func TestGateWarning_NoGhOrNoPRNoLine(t *testing.T) {
	noGh := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Err: ErrNotAvailable}}}
	if got := GateWarning(context.Background(), noGh, linkID, time.Second); got != "" {
		t.Errorf("no gh: want no line, got %q", got)
	}
	noPR := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Stdout: "[]"}, branchArgv: {Stdout: "[]"}}}
	if got := GateWarning(context.Background(), noPR, linkID, time.Second); got != "" {
		t.Errorf("no PR: want no line, got %q", got)
	}
	// A failed checks read is a gh failure: no line.
	f := linkFake(checksRed, viewClean, gqlNoThreads)
	delete(f.Results, linkChecks)
	if got := GateWarning(context.Background(), f, linkID, time.Second); got != "" {
		t.Errorf("checks not read: want no line, got %q", got)
	}
}

// Only an OPEN PR warns; the same red PR merged or closed gives no line.
func TestGateWarning_OnlyOpenPRs(t *testing.T) {
	for _, state := range []string{"MERGED", "CLOSED"} {
		f := linkFake(checksRed, viewConflict, gqlThreads)
		f.Results[titleArgv] = FakeResult{Stdout: "[" + strings.Replace(prRow12, `"OPEN"`, `"`+state+`"`, 1) + "]"}
		if got := GateWarning(context.Background(), f, linkID, time.Second); got != "" {
			t.Errorf("%s PR: want no line, got %q", state, got)
		}
	}
	if got := GateWarning(context.Background(), linkFake(checksRed, viewConflict, gqlThreads), linkID, time.Second); got == "" {
		t.Error("the same PR OPEN must warn")
	}
}

// A failed review read still warns from red checks alone, marked as such;
// with green checks it gives no line.
func TestGateWarning_ReviewNotRead(t *testing.T) {
	f := linkFake(checksRed, viewConflict, gqlThreads)
	delete(f.Results, linkGQL)
	got := GateWarning(context.Background(), f, linkID, time.Second)
	for _, want := range []string{"1 failed check,", "1 cancelled check", "(review not read)", "pull/12"} {
		if !strings.Contains(got, want) {
			t.Errorf("review not read: missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "conflict") || strings.Contains(got, "thread") {
		t.Errorf("review reasons are unknown here: %q", got)
	}
	g := linkFake(checksGreen, viewConflict, gqlThreads)
	delete(g.Results, linkView)
	if got := GateWarning(context.Background(), g, linkID, time.Second); got != "" {
		t.Errorf("green checks, review not read: want no line, got %q", got)
	}
}

func TestPlural(t *testing.T) {
	cases := map[int]string{1: "1 failed check", 2: "2 failed checks", 0: "0 failed checks"}
	for n, want := range cases {
		if got := plural(n, "failed check"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
	gqlOne := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":false}]}}}}}`
	got := GateWarning(context.Background(), linkFake(checksGreen, viewClean, gqlOne), linkID, time.Second)
	if !strings.Contains(got, "1 unresolved review thread —") {
		t.Errorf("one thread: %q", got)
	}
}

func TestLinkTimeouts(t *testing.T) {
	if LinkTimeout != 10*time.Second || ShowTimeout != 5*time.Second {
		t.Errorf("gate cap %s (want 10s), show cap %s (want 5s)", LinkTimeout, ShowTimeout)
	}
}

// slowRunner blocks every call until its context ends.
type slowRunner struct{}

func (slowRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, []byte, int, error) {
	select {
	case <-ctx.Done():
		return nil, nil, -1, ctx.Err()
	case <-time.After(30 * time.Second):
		return []byte("[]"), nil, 0, nil
	}
}

func TestGateWarning_TimeoutBoundsTheLookup(t *testing.T) {
	start := time.Now()
	got := GateWarning(context.Background(), slowRunner{}, linkID, 100*time.Millisecond)
	if got != "" {
		t.Errorf("timed-out lookup must print no line, got %q", got)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("lookup not bounded by its timeout: took %s", el)
	}
	start = time.Now()
	if got := ShowLine(context.Background(), slowRunner{}, linkID, 100*time.Millisecond); got != "" {
		t.Errorf("timed-out show line must be empty, got %q", got)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("show lookup not bounded: took %s", el)
	}
}

func TestShowLine(t *testing.T) {
	got := ShowLine(context.Background(), linkFake(checksMixed, viewClean, gqlNoThreads), linkID, time.Second)
	if got != "PR #12 OPEN · CI 1 pass, 1 fail, 1 pending" {
		t.Errorf("show line = %q", got)
	}
	noGh := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Err: ErrNotAvailable}}}
	if got := ShowLine(context.Background(), noGh, linkID, time.Second); got != "" {
		t.Errorf("no gh: want empty, got %q", got)
	}
}

func TestBuildSnapshotForWorkItem(t *testing.T) {
	f := linkFake(checksMixed, viewClean, gqlNoThreads)
	f.Results["gh pr view 12 --json "+prFields] = FakeResult{Stdout: prRow12}
	snap, err := BuildSnapshotForWorkItem(context.Background(), f, linkID, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Available || snap.PR == nil || snap.PR.Number != 12 || snap.Counts.Fail != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}
	none := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Stdout: "[]"}, branchArgv: {Stdout: "[]"}}}
	snap, err = BuildSnapshotForWorkItem(context.Background(), none, linkID, time.Now)
	if err != nil || snap.PR != nil || !strings.Contains(snap.Reason, linkID) || snap.WorkItem != linkID {
		t.Fatalf("no PR: %+v, %v", snap, err)
	}
	noGh := &FakeRunner{Results: map[string]FakeResult{titleArgv: {Err: ErrNotAvailable}}}
	snap, err = BuildSnapshotForWorkItem(context.Background(), noGh, linkID, time.Now)
	if err != nil || snap.Available {
		t.Fatalf("no gh: want not available, got %+v, %v", snap, err)
	}
}

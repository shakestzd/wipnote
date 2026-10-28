package statusall

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// Two projects, one unreadable: the unreadable one is an error row and the
// other still prints in full.
func TestCollect_UnreadableProjectIsAnErrorRow(t *testing.T) {
	good := tempProject(t, "good commit", testNow.Add(-time.Hour))
	writeItem(t, good, "feat-aaaa0001", "feature", "in-progress", "Build the good thing")
	writeItem(t, good, "feat-aaaa0002", "feature", "todo", "Later")
	gone := filepath.Join(t.TempDir(), "gone")

	r := Collect(context.Background(), baseOptions(entry(good, "good", testNow), entry(gone, "gone", testNow)))

	if len(r.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(r.Projects))
	}
	if r.Projects[1].Name != "gone" {
		t.Fatalf("an unreadable project sorts last: %q first", r.Projects[0].Name)
	}
	bad := findProject(t, r, "gone")
	if len(bad.Errors) == 0 || !strings.Contains(bad.Errors[0], "cannot read project") {
		t.Fatalf("unreadable project errors = %q", bad.Errors)
	}
	ok := findProject(t, r, "good")
	if len(ok.Errors) != 0 {
		t.Fatalf("readable project has errors %q", ok.Errors)
	}
	if len(ok.InProgress) != 1 || ok.InProgress[0].ID != "feat-aaaa0001" {
		t.Fatalf("in progress = %+v, want only feat-aaaa0001", ok.InProgress)
	}
	if ok.LastCommit == nil || ok.LastCommit.Subject != "good commit" || ok.Branch != "work" {
		t.Fatalf("last commit = %+v branch %q", ok.LastCommit, ok.Branch)
	}

	var buf bytes.Buffer
	WriteText(&buf, r, testNow)
	out := buf.String()
	if !strings.Contains(out, "gone") || !strings.Contains(out, "error: cannot read project") {
		t.Fatalf("text lacks the error line:\n%s", out)
	}
	if !strings.Contains(out, "feat-aaaa0001 Build the good thing") || !strings.Contains(out, "good commit") {
		t.Fatalf("text lacks the readable project:\n%s", out)
	}
}

func TestCollect_GHPresentFillsPR(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	o := baseOptions(entry(p, "p", testNow))
	o.GH = ghFactory(ghWithPR())

	got := findProject(t, Collect(context.Background(), o), "p")

	if got.Sources["gh"] != SourceOK {
		t.Fatalf("gh source = %q", got.Sources["gh"])
	}
	pr := got.PR
	if pr == nil || pr.Number != 42 || pr.State != "OPEN" || pr.Review != "APPROVED" || pr.Mergeable != "MERGEABLE" {
		t.Fatalf("pr = %+v", pr)
	}
	if pr.Checks.Pass != 1 || pr.Checks.Fail != 1 || pr.Checks.Total != 2 || len(pr.Failing) != 1 || pr.Failing[0] != "lint" {
		t.Fatalf("checks = %+v failing %q", pr.Checks, pr.Failing)
	}
}

func TestCollect_GHAbsentIsNotAvailableForThatPartOnly(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writeItem(t, p, "bug-bbbb0001", "bug", "in-progress", "Fix it")
	o := baseOptions(entry(p, "p", testNow))
	o.GH = ghFactory(&fakeGH{err: gitwatch.ErrNotAvailable})

	got := findProject(t, Collect(context.Background(), o), "p")

	if got.PR != nil || !strings.HasPrefix(got.Sources["gh"], SourceNotAvailable) {
		t.Fatalf("pr = %+v, gh source = %q; want nil and not available", got.PR, got.Sources["gh"])
	}
	if len(got.Errors) != 0 || len(got.InProgress) != 1 || got.LastCommit == nil {
		t.Fatalf("the rest of the row must still read: %+v", got)
	}
}

func TestCollect_NoGHSkipsEveryGHCall(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	got := findProject(t, Collect(context.Background(), baseOptions(entry(p, "p", testNow))), "p")
	if got.Sources["gh"] != SourceSkipped || got.PR != nil {
		t.Fatalf("gh source = %q pr = %+v", got.Sources["gh"], got.PR)
	}
}

func TestCollect_NoPRForBranchIsNone(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	o := baseOptions(entry(p, "p", testNow))
	o.GH = ghFactory(&fakeGH{rules: [][2]string{{"gh pr list --head work", `[]`}}})
	got := findProject(t, Collect(context.Background(), o), "p")
	if got.Sources["gh"] != SourceNone || got.PR != nil {
		t.Fatalf("gh source = %q pr = %+v", got.Sources["gh"], got.PR)
	}
}

// A hung gh is cut by the run's deadline; the run still finishes in bounded
// time and the row keeps its local parts.
func TestCollect_HungGHIsBoundedByTheDeadline(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	o := baseOptions(entry(p, "p", testNow))
	o.GH = ghFactory(&fakeGH{block: true})
	o.Deadline = 300 * time.Millisecond

	start := time.Now()
	got := findProject(t, Collect(context.Background(), o), "p")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Collect took %s with a hung gh", d)
	}
	if !strings.HasPrefix(got.Sources["gh"], SourceNotAvailable) || got.LastCommit == nil {
		t.Fatalf("gh source = %q commit %+v", got.Sources["gh"], got.LastCommit)
	}
}

func herdrAgent(pane, kind, cwd, name string, state herdr.AgentState) herdr.Agent {
	return herdr.Agent{Pane: herdr.PaneID(pane), TerminalID: "t-" + pane, Kind: kind, Name: name, State: state, Cwd: cwd}
}

func TestCollect_BlockedPaneNeedsYouIdleDoesNot(t *testing.T) {
	blocked := tempProject(t, "c1", testNow.Add(-time.Hour))
	idle := tempProject(t, "c2", testNow.Add(-2*time.Hour))
	writeItem(t, blocked, "feat-cccc0001", "feature", "in-progress", "Wait for the owner")
	o := baseOptions(entry(blocked, "blocked", testNow), entry(idle, "idle", testNow))
	o.Herdr = &fakeHerdr{screen: "Allow this command?\n\n", agents: []herdr.Agent{
		herdrAgent("w1:p2", "claude", filepath.Join(blocked, "sub"), "feat-cccc0001: Wait for the owner", herdr.StateBlocked),
		herdrAgent("w1:p3", "codex", idle, "", herdr.StateIdle),
	}}

	r := Collect(context.Background(), o)

	b := findProject(t, r, "blocked")
	if len(b.NeedsYou) != 1 || b.NeedsYou[0].Kind != NeedBlockedAgent || b.NeedsYou[0].WorkItem != "feat-cccc0001" {
		t.Fatalf("blocked needs_you = %+v", b.NeedsYou)
	}
	if len(b.Agents) != 1 || b.Agents[0].State != "blocked" || b.Agents[0].Title != "Wait for the owner" ||
		b.Agents[0].LastLine != "Allow this command?" {
		t.Fatalf("blocked agents = %+v", b.Agents)
	}
	i := findProject(t, r, "idle")
	if len(i.NeedsYou) != 0 {
		t.Fatalf("an idle pane must not need the owner: %+v", i.NeedsYou)
	}
	if len(i.Agents) != 1 || i.Agents[0].Harness != "codex" || i.Agents[0].State != "idle" {
		t.Fatalf("idle agents = %+v", i.Agents)
	}
	if b.Sources["herdr"] != SourceOK {
		t.Fatalf("herdr source = %q", b.Sources["herdr"])
	}
}

// Without a cwd, a pane is placed by the work item in its label.
func TestCollect_HerdrPaneWithoutCwdMatchesByWorkItemLabel(t *testing.T) {
	a := tempProject(t, "c1", testNow.Add(-time.Hour))
	b := tempProject(t, "c2", testNow.Add(-time.Hour))
	writeItem(t, b, "feat-dddd0001", "feature", "in-progress", "Labelled")
	o := baseOptions(entry(a, "a", testNow), entry(b, "b", testNow))
	o.Herdr = &fakeHerdr{agents: []herdr.Agent{herdrAgent("w2:p1", "claude", "", "feat-dddd0001: Labelled", herdr.StateWorking)}}

	r := Collect(context.Background(), o)
	if got := findProject(t, r, "a").Agents; len(got) != 0 {
		t.Fatalf("project a got agents %+v", got)
	}
	if got := findProject(t, r, "b").Agents; len(got) != 1 || got[0].WorkItem != "feat-dddd0001" {
		t.Fatalf("project b agents = %+v", got)
	}
}

func TestCollect_HerdrAbsentKeepsLedgerAgents(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writeItem(t, p, "feat-eeee0001", "feature", "in-progress", "Claimed work")
	openSession(t, p, sessA, "claude", testNow.Add(-30*time.Minute), time.Time{})
	claim(t, p, sessA, "feat-eeee0001", testNow.Add(-20*time.Minute))
	openSession(t, p, sessB, "codex", testNow.Add(-5*time.Hour), testNow.Add(-4*time.Hour))
	claim(t, p, sessB, "feat-eeee0001", testNow.Add(-5*time.Hour))

	got := findProject(t, Collect(context.Background(), baseOptions(entry(p, "p", testNow))), "p")

	if !strings.HasPrefix(got.Sources["herdr"], SourceNotAvailable) {
		t.Fatalf("herdr source = %q", got.Sources["herdr"])
	}
	if len(got.Agents) != 1 {
		t.Fatalf("agents = %+v; want only the live session's claim", got.Agents)
	}
	ag := got.Agents[0]
	if ag.Source != "ledger" || ag.Harness != "claude" || ag.State != "claimed" || ag.WorkItem != "feat-eeee0001" || ag.Title != "Claimed work" {
		t.Fatalf("agent = %+v", ag)
	}
}

// A herdr pane with no label joins the one ledger claim of its harness.
func TestCollect_HerdrPaneJoinsTheOnlyLedgerClaimOfItsHarness(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writeItem(t, p, "feat-ffff0001", "feature", "in-progress", "Joined")
	openSession(t, p, sessA, "claude", testNow.Add(-30*time.Minute), time.Time{})
	claim(t, p, sessA, "feat-ffff0001", testNow.Add(-20*time.Minute))
	o := baseOptions(entry(p, "p", testNow))
	o.Herdr = &fakeHerdr{agents: []herdr.Agent{herdrAgent("w1:p4", "claude", p, "", herdr.StateWorking)}}

	got := findProject(t, Collect(context.Background(), o), "p")
	if len(got.Agents) != 1 {
		t.Fatalf("agents = %+v, want one joined row", got.Agents)
	}
	if a := got.Agents[0]; a.Source != "herdr+ledger" || a.State != "working" || a.WorkItem != "feat-ffff0001" || a.Pane != "w1:p4" {
		t.Fatalf("agent = %+v", a)
	}
}

func TestCollect_HerdrListFailureIsNotAvailable(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	o := baseOptions(entry(p, "p", testNow))
	o.Herdr = &fakeHerdr{err: herdr.ErrAbsent}
	got := findProject(t, Collect(context.Background(), o), "p")
	if !strings.HasPrefix(got.Sources["herdr"], SourceNotAvailable) || len(got.Errors) != 0 {
		t.Fatalf("herdr source = %q errors %q", got.Sources["herdr"], got.Errors)
	}
}

func TestCollect_OrdersByNewestActivity(t *testing.T) {
	old := tempProject(t, "old", testNow.Add(-48*time.Hour))
	mid := tempProject(t, "mid", testNow.Add(-5*time.Hour))
	fresh := tempProject(t, "fresh", testNow.Add(-72*time.Hour))
	// fresh's newest activity is a run, not its commit.
	run(t, fresh, sessA, "k1", "go test ./...", "completed", 0, testNow.Add(-10*time.Minute), testNow.Add(-5*time.Minute))

	r := Collect(context.Background(), baseOptions(entry(old, "old", testNow), entry(mid, "mid", testNow), entry(fresh, "fresh", testNow)))

	var names []string
	for _, p := range r.Projects {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "fresh,mid,old" {
		t.Fatalf("order = %v, want fresh,mid,old", names)
	}
	if got := findProject(t, r, "fresh").LastActivity; got != "2026-10-28T11:55:00Z" {
		t.Fatalf("fresh last_activity = %q", got)
	}
}

func TestCollect_RunsAndRunNeeds(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	openSession(t, p, sessA, "claude", testNow.Add(-3*time.Hour), testNow.Add(-90*time.Minute))
	openSession(t, p, sessB, "claude", testNow.Add(-1*time.Hour), time.Time{})
	run(t, p, sessA, "k1", "gate", "failed", 1, testNow.Add(-170*time.Minute), testNow.Add(-160*time.Minute))
	run(t, p, sessA, "k2", "lint", "failed", 1, testNow.Add(-150*time.Minute), testNow.Add(-140*time.Minute))
	run(t, p, sessA, "k3", "lint", "completed", 0, testNow.Add(-130*time.Minute), testNow.Add(-120*time.Minute))
	run(t, p, sessA, "k4", "dev server", "", 0, testNow.Add(-110*time.Minute), time.Time{})
	run(t, p, sessB, "k5", "watch", "", 0, testNow.Add(-30*time.Minute), time.Time{})
	run(t, p, sessA, "k6", "ancient", "failed", 1, testNow.Add(-72*time.Hour), testNow.Add(-71*time.Hour))

	o := baseOptions(entry(p, "p", testNow))
	o.RunsLimit = 3
	got := findProject(t, Collect(context.Background(), o), "p")

	if len(got.Runs) != 3 || got.Runs[0].Description != "watch" || got.Runs[0].Status != "running" ||
		got.Runs[1].Status != "unknown" || got.Runs[2].Description != "lint" {
		t.Fatalf("runs = %+v", got.Runs)
	}
	kinds := map[string]string{}
	for _, n := range got.NeedsYou {
		kinds[n.Kind] += n.What + "|"
	}
	if !strings.Contains(kinds[NeedFailedRun], "gate") || strings.Contains(kinds[NeedFailedRun], "lint") ||
		strings.Contains(kinds[NeedFailedRun], "ancient") {
		t.Fatalf("failed-run needs = %q; want gate only (lint re-ran green, ancient is old)", kinds[NeedFailedRun])
	}
	if !strings.Contains(kinds[NeedLostRun], "dev server") || strings.Contains(kinds[NeedLostRun], "watch") {
		t.Fatalf("lost-run needs = %q; want dev server only (watch's session is live)", kinds[NeedLostRun])
	}
}

const planBody = `meta:
    id: plan-0000aaaa
    title: Ship the widget
    status: finalized
slices:
    - feature_id: feat-11110001
      num: 1
      title: First
      execution_status: done
    - feature_id: feat-11110002
      num: 2
      title: Second
      execution_status: promoted
    - feature_id: feat-11110003
      num: 3
      title: Third
      execution_status: not_started
    - num: 4
      title: Dropped
      execution_status: superseded
`

const donePlan = `meta:
    id: plan-0000bbbb
    title: Old plan
    status: completed
slices:
    - num: 1
      title: Only
      execution_status: done
`

func TestCollect_ActivePlanAndCurrentSlice(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writePlan(t, p, "plan-0000aaaa", planBody)
	writePlan(t, p, "plan-0000bbbb", donePlan)
	writeItem(t, p, "feat-11110002", "feature", "in-progress", "Second")

	got := findProject(t, Collect(context.Background(), baseOptions(entry(p, "p", testNow))), "p")

	pl := got.Plan
	if pl == nil || pl.ID != "plan-0000aaaa" || pl.Done != 1 || pl.Total != 3 {
		t.Fatalf("plan = %+v", pl)
	}
	if pl.Slice == nil || pl.Slice.Num != 2 || pl.Slice.FeatureID != "feat-11110002" || pl.Slice.Status != "promoted" {
		t.Fatalf("slice = %+v", pl.Slice)
	}
}

func TestCollect_NoActivePlanIsNull(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writePlan(t, p, "plan-0000bbbb", donePlan)
	got := findProject(t, Collect(context.Background(), baseOptions(entry(p, "p", testNow))), "p")
	if got.Plan != nil {
		t.Fatalf("plan = %+v, want none (only a completed plan)", got.Plan)
	}
}

func TestCollect_BadPlanIsAnErrorButTheRowStillReads(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writePlan(t, p, "plan-0000cccc", "meta: [unclosed")
	writeItem(t, p, "feat-22220001", "feature", "in-progress", "Still here")
	got := findProject(t, Collect(context.Background(), baseOptions(entry(p, "p", testNow))), "p")
	if len(got.Errors) != 1 || !strings.Contains(got.Errors[0], "plan-0000cccc") || len(got.InProgress) != 1 {
		t.Fatalf("errors = %q in progress = %+v", got.Errors, got.InProgress)
	}
}

// Collect reads only: no ledger, index or registry file appears.
func TestCollect_WritesNothing(t *testing.T) {
	p := tempProject(t, "c1", testNow.Add(-time.Hour))
	writeItem(t, p, "feat-33330001", "feature", "in-progress", "Read only")
	before := listTree(t, p)
	Collect(context.Background(), baseOptions(entry(p, "p", testNow)))
	if after := listTree(t, p); after != before {
		t.Fatalf("files changed:\nbefore %s\nafter  %s", before, after)
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var names []string
	_ = filepath.Walk(filepath.Join(root, ".wipnote"), func(path string, info os.FileInfo, err error) error {
		if err == nil {
			names = append(names, path+":"+info.ModTime().String())
		}
		return nil
	})
	return strings.Join(names, ";")
}

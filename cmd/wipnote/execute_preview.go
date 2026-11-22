package main

import (
	"bytes"
	"fmt"
	"github.com/shakestzd/wipnote/core/paths"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// executePreview is the JSON envelope returned by `wipnote execute-preview`.
// It aggregates everything an orchestrator needs to start dispatching work on a
// track: track metadata, linked work items grouped by kind, the open blocking
// owner questions on those items, and current git state.
type executePreview struct {
	Track    *models.Node   `json:"track"`
	Features []*models.Node `json:"features,omitempty"`
	Bugs     []*models.Node `json:"bugs,omitempty"`
	Plans    []*models.Node `json:"plans,omitempty"`
	Spikes   []*models.Node `json:"spikes,omitempty"`
	// Questions holds every open blocking question on the linked features,
	// bugs and spikes, one row per question naming its item_id. An item with
	// one must not be dispatched. Always a list, never null.
	Questions []questionRow   `json:"questions"`
	Git       executeGitState `json:"git"`
	// Ready lists the todo tasks to dispatch together: no open blocker, no
	// blocking question, and no file shared with an earlier ready task.
	Ready []string `json:"ready"`
	// Deferred lists the todo tasks held back, each with its reason.
	Deferred []deferredTask `json:"deferred"`
	// Briefs holds one brief per ready task.
	Briefs []taskBrief `json:"briefs"`
}

type executeGitState struct {
	Branch            string `json:"branch"`
	HeadSHA           string `json:"head_sha"`
	CommitsAheadMain  int    `json:"commits_ahead_main"`
	CommitsBehindMain int    `json:"commits_behind_main"`
	WorktreePath      string `json:"worktree_path"`
}

func executePreviewCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "execute-preview <trk-id>",
		Short: "Compute the ready set, file overlap and a brief per task for a track — one call",
		Long: `Reads a track and prints what an orchestrator needs to dispatch a wave.

Output: the linked work items, open blocking questions, git state, the ready
tasks, the deferred tasks with reasons, one brief per ready task, and the
review rule. --format json adds ready, deferred and briefs to the envelope.

A task is a todo feature on the track. It is ready when it has no open
blocked_by item, no open blocking question, and no file shared with an
earlier ready task. File sets come from the plan slice files list, or from
wipnote trace for a feature with no slice. Overlap is settled greedily by
feature id, so the same input gives the same wave.

Dispatch loop:
  1. Run execute-preview once. Do not call track show, feature show or plan
     show for the same facts.
  2. Start the ready tasks together, one worktree each, with wipnote delegate
     (see wipnote delegate --help). Each brief names its first command.
     The worker rules reach a worker through the SubagentStart hook, so a
     brief does not repeat them.
  3. Merge each finished branch and run wipnote check --gate on the result.
     If the gate fails, fix it or revert the merge before the next wave.
  4. Rerun execute-preview. Deferred tasks come back once their blocker is
     done. Stop when no todo task remains.

A blocking question holds its item. Answer it with
wipnote question answer <question-id> <key>, then rerun. A non-blocking
question does not hold dispatch.

Use native /batch instead for 5-30 independent units with no wipnote plan.
Every brief still carries its work item id so commits stay attributed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runExecutePreview(args[0], format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: json or text")
	return cmd
}

func runExecutePreview(id, format string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	resolved, err := workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(resolved, "trk-") {
		return fmt.Errorf("execute-preview: expected a track id, got %q", resolved)
	}

	preview, err := buildExecutePreview(dir, resolved)
	if err != nil {
		return err
	}

	switch format {
	case "json":
		if err := printJSON(preview); err != nil {
			return err
		}
	default:
		fmt.Print(renderExecutePreviewText(preview))
	}
	return nil
}

func buildExecutePreview(dir, trackID string) (*executePreview, error) {
	trackPath := resolveTrackPath(dir, trackID)
	if trackPath == "" {
		return nil, workitem.ErrNotFoundOnDisk("track", trackID)
	}
	trackNode, err := htmlparse.ParseFile(trackPath)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", trackPath, err)
	}

	preview := &executePreview{Track: trackNode}
	seen := make(map[string]bool)

	// 1. Direct edges from the track — features, bugs, spikes, plans.
	for _, edges := range trackNode.Edges {
		for _, edge := range edges {
			addLinkedNode(dir, edge.TargetID, preview, seen)
		}
	}

	// 2. Indirect plans — plans commonly link back to a feature or track via
	//    data-feature-id, or a feature's planned_in edges point to a plan.
	//    Walk linked features' planned_in targets and scan plans/ for any
	//    plan whose data-feature-id matches the track or a linked feature.
	for _, feat := range preview.Features {
		for _, edges := range feat.Edges {
			for _, edge := range edges {
				if strings.HasPrefix(edge.TargetID, "plan-") {
					addLinkedNode(dir, edge.TargetID, preview, seen)
				}
			}
		}
	}
	discoverPlansByFeatureID(dir, trackID, preview, seen)
	for _, feat := range preview.Features {
		discoverPlansByFeatureID(dir, feat.ID, preview, seen)
	}

	sort.Slice(preview.Features, func(i, j int) bool { return preview.Features[i].ID < preview.Features[j].ID })
	sort.Slice(preview.Bugs, func(i, j int) bool { return preview.Bugs[i].ID < preview.Bugs[j].ID })
	sort.Slice(preview.Plans, func(i, j int) bool { return preview.Plans[i].ID < preview.Plans[j].ID })
	sort.Slice(preview.Spikes, func(i, j int) bool { return preview.Spikes[i].ID < preview.Spikes[j].ID })

	preview.Questions = blockingQuestionRows(time.Now(), preview.Features, preview.Bugs, preview.Spikes)
	preview.Git = currentGitState(dir)
	planDispatch(dir, preview)
	return preview, nil
}

// resolveTrackPath mirrors runTrackShowWithFormat's track-path lookup: flat
// tracks/<id>.html first, then the directory-backed tracks/<id>/index.html
// fallback. resolveNodePath only handles the flat form, so execute-preview
// needs its own resolver to avoid regressing directory-backed tracks.
func resolveTrackPath(wipnoteDir, id string) string {
	flat := filepath.Join(wipnoteDir, "tracks", id+".html")
	if _, err := os.Stat(flat); err == nil {
		return flat
	}
	indexed := filepath.Join(wipnoteDir, "tracks", id, "index.html")
	if _, err := os.Stat(indexed); err == nil {
		return indexed
	}
	return ""
}

// addLinkedNode resolves a node path, parses the HTML, and appends it to the
// correct preview bucket based on the canonical id prefix. Spikes use spk-
// (matching internal/models/enums.go and history.go). Idempotent via seen.
func addLinkedNode(wipnoteDir, id string, p *executePreview, seen map[string]bool) {
	if id == "" || seen[id] {
		return
	}
	seen[id] = true
	path := workitem.NodePath(wipnoteDir, id)
	if path == "" {
		if strings.HasPrefix(id, "trk-") {
			path = resolveTrackPath(wipnoteDir, id)
		}
		if path == "" {
			return
		}
	}
	node, err := htmlparse.ParseFile(path)
	if err != nil {
		return
	}
	switch {
	case strings.HasPrefix(id, "feat-"):
		p.Features = append(p.Features, node)
	case strings.HasPrefix(id, "bug-"):
		p.Bugs = append(p.Bugs, node)
	case strings.HasPrefix(id, "plan-"):
		p.Plans = append(p.Plans, node)
	case strings.HasPrefix(id, "spk-"):
		p.Spikes = append(p.Spikes, node)
	}
}

// discoverPlansByFeatureID scans .wipnote/plans/*.html for any plan whose
// data-feature-id attribute matches sourceID, and adds matching plans to the
// preview. Mirrors findExistingPlanForSource in plan_cmds.go but collects
// all hits rather than returning the first.
func discoverPlansByFeatureID(wipnoteDir, sourceID string, p *executePreview, seen map[string]bool) {
	plansDir := filepath.Join(wipnoteDir, "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return
	}
	needle := []byte(fmt.Sprintf(`data-feature-id="%s"`, sourceID))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		planID := strings.TrimSuffix(e.Name(), ".html")
		if seen[planID] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(plansDir, e.Name()))
		if err != nil {
			continue
		}
		if !bytes.Contains(data, needle) {
			continue
		}
		addLinkedNode(wipnoteDir, planID, p, seen)
	}
}

// currentGitState resolves git state for the caller's current working directory
// when it belongs to the same repository as the wipnote project. If the
// caller is inside a nested/submodule repo (different git-common-dir), probes
// run against the wipnote project root instead so the preview never reports
// branch/ahead/behind for an unrelated repository. Mirrors the same guard as
// `wipnote history` (see resolveHistoryRoot in history.go).
func currentGitState(wipnoteDir string) executeGitState {
	repoRoot := resolveGitProbeRoot(wipnoteDir)
	state := executeGitState{WorktreePath: repoRoot}

	if branch, err := gitOutputIn(repoRoot, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		state.Branch = branch
	}
	if sha, err := gitOutputIn(repoRoot, "rev-parse", "HEAD"); err == nil {
		state.HeadSHA = sha
	}
	// --left-right main...HEAD with --count emits "<behind>\t<ahead>".
	if counts, err := gitOutputIn(repoRoot, "rev-list", "--left-right", "--count", "main...HEAD"); err == nil {
		parts := strings.Fields(counts)
		if len(parts) == 2 {
			if behind, err := strconv.Atoi(parts[0]); err == nil {
				state.CommitsBehindMain = behind
			}
			if ahead, err := strconv.Atoi(parts[1]); err == nil {
				state.CommitsAheadMain = ahead
			}
		}
	}
	return state
}

// resolveGitProbeRoot picks the repo root for git probes. Prefers the caller's
// cwd when it shares a git-common-dir with the wipnote project (linked
// worktree — branch-local state is correct). Otherwise falls back to the
// project owner so nested/submodule CWDs don't leak unrelated repo state.
// Reuses resolveHistoryRoot which already implements this check.
//
// On error from resolveHistoryRoot (permission error, missing git, etc.) we
// silently fall back to the project owner rather than aborting — execute-
// preview must stay non-fatal — but emit a stderr diagnostic so confused
// ahead/behind counts have a visible trail for debugging.
func resolveGitProbeRoot(wipnoteDir string) string {
	owner := paths.CodeRoot(filepath.Dir(wipnoteDir))
	root, err := resolveHistoryRoot(owner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "execute-preview: resolveHistoryRoot(%s) failed, falling back to project owner: %v\n", owner, err)
		return owner
	}
	return root
}

// gitOutputIn runs a git sub-command with cwd set to repoRoot, returning
// trimmed stdout. Distinct from gitOutput in review.go (which runs in the
// current working directory) because execute-preview needs to resolve paths
// relative to the discovered wipnote project root, not the caller's cwd.
func gitOutputIn(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func renderExecutePreviewText(p *executePreview) string {
	w := &strings.Builder{}
	fmt.Fprintf(w, "Track: %s  %s  [%s]\n", p.Track.ID, p.Track.Title, p.Track.Status)
	fmt.Fprintf(w, "Git:   branch=%s  ahead=%d  behind=%d  head=%s\n",
		p.Git.Branch, p.Git.CommitsAheadMain, p.Git.CommitsBehindMain, firstN(p.Git.HeadSHA, 8))
	printNodeGroup(w, "Features", p.Features)
	printNodeGroup(w, "Bugs", p.Bugs)
	printNodeGroup(w, "Plans", p.Plans)
	printNodeGroup(w, "Spikes", p.Spikes)
	printBlockingQuestions(w, p.Questions)
	printDispatch(w, p)
	return w.String()
}

// blockingQuestionRows returns the open blocking questions on the nodes, in
// node order.
func blockingQuestionRows(now time.Time, groups ...[]*models.Node) []questionRow {
	rows := []questionRow{}
	for _, nodes := range groups {
		for _, n := range nodes {
			for _, q := range question.FromNode(n) {
				if q.Blocking && q.IsOpen() {
					rows = append(rows, newQuestionRow(n, q, now))
				}
			}
		}
	}
	return rows
}

func printBlockingQuestions(w io.Writer, rows []questionRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\nBlocking questions (%d) — do not dispatch these items:\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(w, "  %-20s  %-12s  %s\n", r.ItemID, r.QuestionID, r.Text)
	}
}

func printNodeGroup(w io.Writer, label string, nodes []*models.Node) {
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s (%d):\n", label, len(nodes))
	for _, n := range nodes {
		fmt.Fprintf(w, "  %-20s  %-12s  %s\n", n.ID, n.Status, n.Title)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

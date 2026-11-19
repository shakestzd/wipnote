package main

import (
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// deferredTask is a pending task that execute-preview holds back, with the
// reason an orchestrator can quote.
type deferredTask struct {
	ItemID string `json:"item_id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// taskBrief is what a dispatched worker needs before its first command. The
// worker rules arrive through the SubagentStart hook, so the brief does not
// repeat them.
type taskBrief struct {
	ItemID       string   `json:"item_id"`
	Title        string   `json:"title"`
	Files        []string `json:"files"`
	Siblings     []string `json:"siblings"`
	FirstCommand string   `json:"first_command"`
	Pack         string   `json:"pack"`
}

// reviewRule states when code review is required. The rule replaces the
// "review is mandatory" step of the retired dispatch guidance (feat-d06e3fb2).
const reviewRule = "Code review is required once, on the track branch, before it merges, when any task changed code.\n" +
	"Code review is skipped when the tasks changed only docs, plans or generated files.\n" +
	"Reviewers block only on a failing run or a user-visible bug. Report every other finding and do not block on it."

// planDispatch fills Ready, Deferred and Briefs. A task is a feature in the
// todo state. It waits on an open blocker, an open blocking question, or a
// file shared with an earlier ready task. Order is by id, so the choice is
// greedy and stable.
func planDispatch(wipnoteDir string, p *executePreview) {
	p.Ready, p.Deferred, p.Briefs = []string{}, []deferredTask{}, []taskBrief{}
	status := map[string]models.NodeStatus{}
	for _, f := range p.Features {
		status[f.ID] = f.Status
	}
	asked := map[string]string{}
	for _, q := range p.Questions {
		if _, ok := asked[q.ItemID]; !ok {
			asked[q.ItemID] = q.QuestionID
		}
	}
	slices := sliceFiles(wipnoteDir, p.Features)
	owner := map[string]string{} // file -> ready item that holds it
	files := map[string][]string{}
	for _, f := range p.Features {
		if f.Status != models.StatusTodo {
			continue
		}
		if reason := holdReason(f, status, asked); reason != "" {
			p.Deferred = append(p.Deferred, deferredTask{f.ID, f.Title, reason})
			continue
		}
		fs := slices[f.ID]
		if len(fs) == 0 {
			fs = tracedFiles(wipnoteDir, f.ID)
		}
		if holder, file := firstOverlap(fs, owner); holder != "" {
			p.Deferred = append(p.Deferred, deferredTask{f.ID, f.Title,
				fmt.Sprintf("shares %s with %s, which runs first", file, holder)})
			continue
		}
		for _, file := range fs {
			owner[file] = f.ID
		}
		files[f.ID] = fs
		p.Ready = append(p.Ready, f.ID)
	}
	for _, f := range p.Features {
		if _, ok := files[f.ID]; ok {
			p.Briefs = append(p.Briefs, buildBrief(f, files[f.ID], p))
		}
	}
}

// holdReason returns why a todo feature cannot start, or "" when it can.
func holdReason(f *models.Node, status map[string]models.NodeStatus, asked map[string]string) string {
	for _, edge := range f.Edges[string(models.RelBlockedBy)] {
		if st, known := status[edge.TargetID]; !known || st != models.StatusDone {
			return "blocked by " + edge.TargetID
		}
	}
	if q, ok := asked[f.ID]; ok {
		return "open blocking question " + q
	}
	return ""
}

// firstOverlap returns the holder and file for the first file already owned.
func firstOverlap(files []string, owner map[string]string) (string, string) {
	for _, file := range files {
		if holder, ok := owner[file]; ok {
			return holder, file
		}
	}
	return "", ""
}

// sliceFiles maps a feature id to its slice file list from the plans that
// the features point at with a planned_in edge.
func sliceFiles(wipnoteDir string, feats []*models.Node) map[string][]string {
	out := map[string][]string{}
	loaded := map[string]bool{}
	for _, f := range feats {
		for _, edge := range f.Edges[string(models.RelPlannedIn)] {
			if loaded[edge.TargetID] {
				continue
			}
			loaded[edge.TargetID] = true
			py, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", edge.TargetID+".yaml"))
			if err != nil {
				continue
			}
			for _, s := range py.Slices {
				if s.FeatureID != "" {
					out[s.FeatureID] = cleanSliceFiles(s.Files)
				}
			}
		}
	}
	return out
}

// cleanSliceFiles drops the "(new)" style note after a path.
func cleanSliceFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if i := strings.Index(f, " ("); i > 0 {
			f = f[:i]
		}
		out = append(out, strings.TrimSpace(f))
	}
	return out
}

// tracedFiles is the fallback file set: the files a promoted feature has
// already touched, per wipnote trace. It returns nil when the database or
// the rows are missing.
func tracedFiles(wipnoteDir, id string) []string {
	var database *sql.DB
	database, err := openReadOnlyDB(wipnoteDir)
	if err != nil {
		return nil
	}
	defer database.Close()
	rows, err := filesByFeature(database, id)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.FilePath] {
			seen[r.FilePath] = true
			out = append(out, r.FilePath)
		}
	}
	sort.Strings(out)
	return out
}

// buildBrief assembles one task brief. The pack comes from renderContextPack
// with no track history, because the preview already holds the git state.
func buildBrief(f *models.Node, files []string, p *executePreview) taskBrief {
	var siblings []string
	for _, s := range p.Features {
		if s.ID != f.ID && s.Status != models.StatusDone {
			siblings = append(siblings, fmt.Sprintf("%s %s [%s]", s.ID, s.Title, s.Status))
		}
	}
	pack := renderContextPack(f, p.Git.Branch, p.Git.CommitsAheadMain, p.Git.CommitsBehindMain, nil, nil, nil)
	return taskBrief{
		ItemID: f.ID, Title: f.Title, Files: files, Siblings: siblings,
		FirstCommand: fmt.Sprintf("wipnote %s start %s", f.Type, f.ID), Pack: pack,
	}
}

// printDispatch writes the ready set, the deferred tasks, a brief for each
// ready task and the review rule.
func printDispatch(w io.Writer, p *executePreview) {
	fmt.Fprintf(w, "\nReady (%d) — dispatch these together, one worktree each:\n", len(p.Ready))
	for _, b := range p.Briefs {
		fmt.Fprintf(w, "  %s  %s\n    files:    %s\n    siblings: %s\n    first command: %s\n",
			b.ItemID, b.Title, joinOrNone(b.Files), joinOrNone(b.Siblings), b.FirstCommand)
	}
	if len(p.Deferred) > 0 {
		fmt.Fprintf(w, "\nDeferred (%d) — rerun after the ready tasks merge:\n", len(p.Deferred))
		for _, d := range p.Deferred {
			fmt.Fprintf(w, "  %s  %s  (%s)\n", d.ItemID, d.Title, d.Reason)
		}
	}
	fmt.Fprintf(w, "\nReview rule:\n%s\n", reviewRule)
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

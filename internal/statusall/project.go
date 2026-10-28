package statusall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/registry"
)

// local is what one project's .wipnote/ holds, read once.
type local struct {
	dir     string // the .wipnote folder
	items   map[string]*models.Node
	ended   map[string]bool // session id -> ledger records its end
	harness map[string]string
	runs    []runledger.Record
}

// collectProject reads one project. Unreadable parts become errors on the
// row; an unreadable project is a row with only its error.
func collectProject(ctx context.Context, o Options, e registry.Entry, hs herdrState, placed []int) Project {
	p := newProject(e)
	if err := readable(e.ProjectDir); err != nil {
		p.Errors = append(p.Errors, "cannot read project: "+err.Error())
		p.LastActivity = e.LastSeen
		return p
	}
	lc := readLocal(&p, filepath.Join(e.ProjectDir, ".wipnote"))
	p.InProgress = inProgress(lc.items)
	p.Plan = activePlan(&p, lc.dir, lc.items)
	p.Runs = recentRuns(lc.runs, o.RunsLimit, o.Loc)
	readGit(ctx, &p, e.ProjectDir)
	readPR(ctx, o, &p, e.ProjectDir)
	p.Agents = projectAgents(ctx, &p, hs, placed, lc)
	p.Sources["herdr"] = hs.state
	p.NeedsYou = needs(p.Agents, lc.runs, o.Now(), o.Window, o.Loc)
	p.LastActivity = stamp(lastActivity(&p, lc, e))
	return p
}

// readable reports why a project folder cannot be read, or nil.
func readable(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("no project folder in the registry entry")
	}
	info, err := os.Stat(filepath.Join(dir, ".wipnote"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no .wipnote folder")
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf(".wipnote is not a folder")
	}
	return nil
}

// readLocal loads work items, the sessions ledger and the runs. Each failed
// read is an error on the row; the rest still load.
func readLocal(p *Project, dir string) local {
	lc := local{dir: dir, items: map[string]*models.Node{}}
	for _, sub := range []string{"features", "bugs", "spikes"} {
		nodes, err := graph.LoadDir(filepath.Join(dir, sub))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			p.Errors = append(p.Errors, fmt.Sprintf("%s: %v", sub, err))
		}
		for _, n := range nodes {
			lc.items[n.ID] = n
		}
	}
	readSessions(p, &lc)
	recs, err := runledger.NewStore(dir).Records(func(s string) bool { return lc.ended[s] })
	if err != nil {
		p.Errors = append(p.Errors, "runs: "+err.Error())
	}
	lc.runs = recs
	return lc
}

// inProgress lists the in-progress items, by id.
func inProgress(items map[string]*models.Node) []Item {
	out := []Item{}
	for _, n := range items {
		if n.Status == models.StatusInProgress {
			out = append(out, Item{ID: n.ID, Title: n.Title, Type: n.Type})
		}
	}
	sortItems(out)
	return out
}

// readGit fills the branch and last commit through bounded local git calls.
func readGit(ctx context.Context, p *Project, dir string) {
	r := gitwatch.ExecRunner{Timeout: GitTimeout, Dir: dir}
	out, _, code, err := r.Run(ctx, "git", "log", "-1", "--format=%H%x1f%s%x1f%cI")
	if err != nil || code != 0 {
		p.Sources["git"] = SourceNotAvailable + ": " + gitReason(err, code)
		return
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\x1f", 3)
	if len(parts) == 3 {
		p.LastCommit = &Commit{SHA: parts[0], Subject: parts[1], At: stamp(parseGitTime(parts[2]))}
	}
	p.Sources["git"] = SourceOK
	if b, _, code, err := r.Run(ctx, "git", "symbolic-ref", "--short", "-q", "HEAD"); err == nil && code == 0 {
		p.Branch = strings.TrimSpace(string(b))
	}
}

func gitReason(err error, code int) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("git log exit %d (no commits, or not a git repository)", code)
}

func parseGitTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// lastActivity is the newest of the last commit, the runs, the agents and
// the in-progress items' updates; the registry's last-seen time when none.
func lastActivity(p *Project, lc local, e registry.Entry) time.Time {
	var newest time.Time
	bump := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}
	if p.LastCommit != nil {
		bump(parseStamp(p.LastCommit.At))
	}
	for _, r := range lc.runs {
		bump(r.StartedAt)
		bump(r.EndedAt)
	}
	for _, a := range p.Agents {
		bump(parseStamp(a.Since))
	}
	for _, it := range p.InProgress {
		bump(lc.items[it.ID].UpdatedAt)
	}
	if newest.IsZero() {
		newest = parseStamp(e.LastSeen)
	}
	return newest
}

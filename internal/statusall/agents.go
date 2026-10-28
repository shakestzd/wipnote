package statusall

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/registry"
)

// herdrReadLines is how many screen lines a last-line read asks for.
const herdrReadLines = 20

// lastLineWidth caps an agent's last line in runes.
const lastLineWidth = 160

// labelPattern finds the work item a pane label names ("<id>: <title>").
var labelPattern = regexp.MustCompile(`\b((?:feat|bug|spk)-[0-9a-z]+)\b`)

// herdrState is the one herdr listing shared by every project.
type herdrState struct {
	src      HerdrSource
	agents   []herdr.Agent
	state    string // Sources["herdr"]
	unplaced []int  // agents no project folder holds; placed by label
}

// listHerdr lists herdr's agents once, under the run's deadline.
func listHerdr(ctx context.Context, o Options) herdrState {
	if o.Herdr == nil {
		return herdrState{state: o.HerdrReason}
	}
	agents, err := o.Herdr.AgentList(ctx)
	if err != nil {
		return herdrState{state: SourceNotAvailable + ": " + err.Error()}
	}
	return herdrState{src: o.Herdr, agents: agents, state: SourceOK}
}

// placeByCwd gives each herdr agent with a working folder to the project
// whose folder holds it (the deepest one), or whose main repository it is a
// linked worktree of. It returns the agent indexes per entry, and records the
// agents no project holds in hs.unplaced.
func placeByCwd(ctx context.Context, entries []registry.Entry, hs *herdrState) [][]int {
	roots := make([]string, len(entries))
	for i, e := range entries {
		roots[i] = clean(e.ProjectDir)
	}
	placed := make([][]int, len(entries))
	for ai, a := range hs.agents {
		owner := -1
		if a.Cwd != "" {
			cwd := clean(a.Cwd)
			if owner = deepestOwner(roots, cwd); owner < 0 {
				owner = deepestOwner(roots, worktreeMain(ctx, cwd))
			}
		}
		if owner < 0 {
			hs.unplaced = append(hs.unplaced, ai)
			continue
		}
		placed[owner] = append(placed[owner], ai)
	}
	return placed
}

func clean(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(paths.EvalSymlinksBestEffort(p))
}

func deepestOwner(roots []string, dir string) int {
	best := -1
	for i, root := range roots {
		if root == "" || dir == "" {
			continue
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if best < 0 || len(root) > len(roots[best]) {
			best = i
		}
	}
	return best
}

// worktreeMain is the main repository of a linked worktree at dir, or "".
func worktreeMain(ctx context.Context, dir string) string {
	r := gitwatch.ExecRunner{Timeout: GitTimeout, Dir: dir}
	out, _, code, err := r.Run(ctx, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || code != 0 {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) != ".git" {
		return ""
	}
	return clean(filepath.Dir(common))
}

// projectAgents is the project's live agents: its herdr panes (by folder or
// by work-item label) joined with the claims the ledgers hold.
func projectAgents(ctx context.Context, p *Project, hs herdrState, placed []int, lc local) []Agent {
	idx := append([]int(nil), placed...)
	for _, i := range hs.unplaced {
		if id := labelItem(hs.agents[i].Name); id != "" && lc.items[id] != nil {
			idx = append(idx, i)
		}
	}
	rows := make([]Agent, 0, len(idx))
	for _, i := range idx {
		rows = append(rows, herdrRow(ctx, hs.src, hs.agents[i], lc))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Pane < rows[j].Pane })
	return joinLedger(rows, ledgerAgents(p, lc))
}

func herdrRow(ctx context.Context, src HerdrSource, a herdr.Agent, lc local) Agent {
	row := Agent{Source: "herdr", Harness: normHarness(a.Kind), State: string(a.State), Pane: string(a.Pane)}
	if id := labelItem(a.Name); id != "" {
		row.WorkItem = id
		if n := lc.items[id]; n != nil {
			row.Title = n.Title
		} else if _, t, ok := strings.Cut(a.Name, ": "); ok {
			row.Title = t
		}
	}
	if src != nil {
		if text, err := src.Read(ctx, herdr.PinOf(a), herdrReadLines); err == nil {
			row.LastLine = lastLine(text)
		}
	}
	return row
}

func labelItem(name string) string {
	if m := labelPattern.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return ""
}

// lastLine is the last non-blank screen line, trimmed and cut to width.
func lastLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if rs := []rune(l); len(rs) > lastLineWidth {
				l = string(rs[:lastLineWidth-1]) + "…"
			}
			return l
		}
	}
	return ""
}

// joinLedger joins each herdr row to the ledger claim it is: the claim on
// the same work item, or, for an unlabelled pane, the one claim of its
// harness when it is the only unlabelled pane of that harness. Claims no pane
// joined are listed after the panes.
func joinLedger(rows, ledger []Agent) []Agent {
	used := make([]bool, len(ledger))
	unlabelled := map[string]int{}
	for _, r := range rows {
		if r.WorkItem == "" {
			unlabelled[r.Harness]++
		}
	}
	for i := range rows {
		j := matchClaim(rows[i], ledger, used, unlabelled[rows[i].Harness] == 1)
		if j < 0 {
			continue
		}
		used[j] = true
		r, l := &rows[i], ledger[j]
		r.Source, r.Session, r.Since = "herdr+ledger", l.Session, l.Since
		if r.WorkItem == "" {
			r.WorkItem, r.Title = l.WorkItem, l.Title
		}
		if r.LastLine == "" {
			r.LastLine = l.LastLine
		}
	}
	for j, l := range ledger {
		if !used[j] {
			rows = append(rows, l)
		}
	}
	return rows
}

func matchClaim(r Agent, ledger []Agent, used []bool, onlyUnlabelled bool) int {
	sameHarness := func(l Agent) bool { return l.Harness == "" || r.Harness == "" || l.Harness == r.Harness }
	if r.WorkItem != "" {
		for j, l := range ledger {
			if !used[j] && l.WorkItem == r.WorkItem && sameHarness(l) {
				return j
			}
		}
		return -1
	}
	found := -1
	for j, l := range ledger {
		if used[j] || l.Harness != r.Harness {
			continue
		}
		if found >= 0 {
			return -1 // two claims: which one this pane is cannot be told
		}
		found = j
	}
	if !onlyUnlabelled {
		return -1
	}
	return found
}

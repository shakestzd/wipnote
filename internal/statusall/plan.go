package statusall

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// Slice execution states that count as finished.
const (
	sliceDone       = "done"
	sliceSuperseded = "superseded"
)

// planStatusRank orders plan states: a plan being built beats one in review,
// which beats a draft. Completed plans are never active.
var planStatusRank = map[string]int{"active": 3, "finalized": 3, "review": 2, "draft": 1}

type planCandidate struct {
	plan   *planyaml.PlanYAML
	active bool // one of its slices' features is in progress
}

// activePlan picks the project's active plan: an unfinished, not completed
// plan, preferring one whose slice feature is in progress, then the more
// advanced status, then the newest. A plan that cannot be read is an error
// on the row. nil when there is none.
func activePlan(p *Project, dir string, items map[string]*models.Node) *Plan {
	files, _ := filepath.Glob(filepath.Join(dir, "plans", "*.yaml"))
	var cands []planCandidate
	for _, f := range files {
		pl, err := planyaml.Load(f)
		if err != nil {
			p.Errors = append(p.Errors, "plan "+strings.TrimSuffix(filepath.Base(f), ".yaml")+": "+err.Error())
			continue
		}
		if pl.Meta.Status == "completed" || unfinished(pl.Slices) == 0 {
			continue
		}
		cands = append(cands, planCandidate{plan: pl, active: currentSliceIndex(pl.Slices, items, true) >= 0})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.SliceStable(cands, func(i, j int) bool { return betterPlan(cands[i], cands[j]) })
	return planRow(cands[0].plan, items)
}

func betterPlan(a, b planCandidate) bool {
	if a.active != b.active {
		return a.active
	}
	ra, rb := planStatusRank[a.plan.Meta.Status], planStatusRank[b.plan.Meta.Status]
	if ra != rb {
		return ra > rb
	}
	if a.plan.Meta.CreatedAt != b.plan.Meta.CreatedAt {
		return a.plan.Meta.CreatedAt > b.plan.Meta.CreatedAt
	}
	return a.plan.Meta.ID < b.plan.Meta.ID
}

func planRow(pl *planyaml.PlanYAML, items map[string]*models.Node) *Plan {
	row := &Plan{ID: pl.Meta.ID, Title: pl.Meta.Title}
	for _, s := range pl.Slices {
		switch s.ExecutionStatus {
		case sliceSuperseded:
			continue
		case sliceDone:
			row.Done++
		}
		row.Total++
	}
	i := currentSliceIndex(pl.Slices, items, true)
	if i < 0 {
		i = currentSliceIndex(pl.Slices, items, false)
	}
	if i >= 0 {
		s := pl.Slices[i]
		status := s.ExecutionStatus
		if status == "" {
			status = "not_started"
		}
		row.Slice = &Slice{Num: s.Num, Title: s.Title, FeatureID: s.FeatureID, Status: status}
	}
	return row
}

// currentSliceIndex returns the first unfinished slice (by num) whose feature
// is in progress when inProgressOnly; otherwise the first started slice
// (promoted or in progress), else the first unfinished one. -1 when none.
func currentSliceIndex(slices []planyaml.PlanSlice, items map[string]*models.Node, inProgressOnly bool) int {
	order := make([]int, len(slices))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return slices[order[a]].Num < slices[order[b]].Num })
	first := -1
	for _, i := range order {
		s := slices[i]
		if s.ExecutionStatus == sliceDone || s.ExecutionStatus == sliceSuperseded {
			continue
		}
		if n := items[s.FeatureID]; s.FeatureID != "" && n != nil && n.Status == models.StatusInProgress {
			return i
		}
		if inProgressOnly {
			continue
		}
		if s.ExecutionStatus == "promoted" || s.ExecutionStatus == "in_progress" {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

func unfinished(slices []planyaml.PlanSlice) int {
	n := 0
	for _, s := range slices {
		if s.ExecutionStatus != sliceDone && s.ExecutionStatus != sliceSuperseded {
			n++
		}
	}
	return n
}

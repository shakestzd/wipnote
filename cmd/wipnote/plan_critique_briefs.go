package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/plan/planyaml"
)

// critiqueBriefs names the two brief files `wipnote plan critique` writes.
type critiqueBriefs struct {
	Design      string `json:"design"`
	Feasibility string `json:"feasibility"`
}

// readingItem is one file in a brief's reading list.
type readingItem struct {
	Path  string
	IsNew bool // listed with "(new)" in a slice: it does not exist yet
}

// runPlanCritiqueBriefs builds the critique output and, for a YAML plan,
// writes the two critic briefs and the spawn lines. A plan that already has
// MaxCritiqueRounds rounds is refused, naming its open findings
// (feat-0be892a9: the critique loop had no cap).
func runPlanCritiqueBriefs(wipnoteDir, planID string) (*critiqueOutput, error) {
	out, err := extractCritiqueData(wipnoteDir, planID)
	if err != nil {
		return nil, err
	}
	plan, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", planID+".yaml"))
	if err != nil {
		return out, nil // legacy HTML-only plan: no briefs
	}
	if err := planyaml.CheckCritiqueRound(plan.Critique); err != nil {
		return nil, err
	}
	briefs, err := writeCritiqueBriefs(wipnoteDir, planID, plan)
	if err != nil {
		return nil, err
	}
	out.Briefs = briefs
	out.Spawn = critiqueSpawnLines(planID, briefs)
	return out, nil
}

// writeCritiqueBriefs writes both briefs into <plans>/<plan-id>.critique/.
// Each brief holds the plan id, the reading list and the settled risks. It
// holds no revision history, feedback or earlier findings, so the critic
// reads the plan fresh.
func writeCritiqueBriefs(wipnoteDir, planID string, plan *planyaml.PlanYAML) (*critiqueBriefs, error) {
	dir := filepath.Join(wipnoteDir, "plans", planID+".critique")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create brief folder: %w", err)
	}
	var settled []planyaml.AcceptedRisk
	if plan.Critique != nil {
		settled = plan.Critique.AcceptedRisks
	}
	common := critiqueCommon(planID, readingList(plan), settled)
	b := &critiqueBriefs{
		Design:      filepath.Join(dir, "design-brief.md"),
		Feasibility: filepath.Join(dir, "feasibility-brief.md"),
	}
	if err := os.WriteFile(b.Design, []byte(designBriefHead+common+findingsFormat), 0o644); err != nil {
		return nil, fmt.Errorf("write design brief: %w", err)
	}
	if err := os.WriteFile(b.Feasibility, []byte(feasibilityBriefHead+common+feasibilityChecks+findingsFormat), 0o644); err != nil {
		return nil, fmt.Errorf("write feasibility brief: %w", err)
	}
	return b, nil
}

// readingList returns the files the slices name, each once, in slice order.
func readingList(plan *planyaml.PlanYAML) []readingItem {
	seen := map[string]bool{}
	var items []readingItem
	for _, s := range plan.Slices {
		for _, f := range s.Files {
			f = strings.TrimSpace(f)
			isNew := strings.HasSuffix(f, "(new)")
			f = strings.TrimSpace(strings.TrimSuffix(f, "(new)"))
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			items = append(items, readingItem{Path: f, IsNew: isNew})
		}
	}
	return items
}

// critiqueCommon is the part both briefs share: plan id, reading list, round
// rule and settled risks.
func critiqueCommon(planID string, files []readingItem, settled []planyaml.AcceptedRisk) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Plan\n\nPlan id: %s\n\n## Reading list\n\n", planID)
	fmt.Fprintf(&b, "- .wipnote/plans/%s.yaml (the plan: read every slice card in full)\n", planID)
	for _, f := range files {
		if f.IsNew {
			fmt.Fprintf(&b, "- %s (new: the slice creates it, so it is not a missing file)\n", f.Path)
		} else {
			fmt.Fprintf(&b, "- %s\n", f.Path)
		}
	}
	b.WriteString("\nRead only these files and what they point to. Do not edit any file.\n")
	if len(settled) > 0 {
		b.WriteString("\n## Settled risks\n\nThe owner accepted these. Do not raise them again.\n\n")
		for _, r := range settled {
			fmt.Fprintf(&b, "- %s (reason: %s)\n", r.Text, r.Reason)
		}
	}
	return b.String()
}

// critiqueSpawnLines returns one delegate spawn line per brief, on the review
// profile. The command prints them and starts no worker itself.
func critiqueSpawnLines(planID string, b *critiqueBriefs) []string {
	return []string{
		fmt.Sprintf("wipnote delegate spawn %s --profile review --brief-file %s", criticName("design", planID), b.Design),
		fmt.Sprintf("wipnote delegate spawn %s --profile review --brief-file %s", criticName("feasibility", planID), b.Feasibility),
	}
}

// criticName makes a worker name of [a-z0-9-], at most 32 characters.
func criticName(kind, planID string) string {
	name := strings.ToLower(kind + "-critic-" + planID)
	if len(name) > 32 {
		name = strings.TrimRight(name[:32], "-")
	}
	return name
}

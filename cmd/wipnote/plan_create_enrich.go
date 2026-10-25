package main

import (
	"html"
	htmltemplate "html/template"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/plan/plantmpl"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// enrichPageFromYAML loads the YAML plan file (if it exists) and populates
// page.Questions and page.Critique from the YAML data. This bridges the
// planyaml data model to the plantmpl rendering structs.
// If the YAML file does not exist the function is a no-op.
func enrichPageFromYAML(wipnoteDir, planID string, page *plantmpl.PlanPage) {
	yamlPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")
	plan, err := planyaml.Load(yamlPath)
	if err != nil {
		if _, statErr := os.Stat(yamlPath); os.IsNotExist(statErr) {
			log.Printf("enrichPageFromYAML: no YAML found for %s (expected %s)", planID, yamlPath)
		}
		return
	}

	applyPlanMetaToPage(page, plan)
	applyPlanDesignToPage(page, plan)
	applyPlanSlicesToPage(page, plan)
	applyPlanQuestionsToPage(page, plan)
	applyPlanCritiqueToPage(page, plan)
}

// applyPlanMetaToPage maps meta fields when the page was built with
// empty/default values.
func applyPlanMetaToPage(page *plantmpl.PlanPage, plan *planyaml.PlanYAML) {
	if page.Title == "" && plan.Meta.Title != "" {
		page.Title = plan.Meta.Title
	}
	if page.Description == "" && plan.Meta.Description != "" {
		page.Description = plan.Meta.Description
	}
	if plan.Meta.Status != "" {
		page.Status = plan.Meta.Status
	}
	if plan.Meta.Priority != "" {
		page.Priority = plan.Meta.Priority
	}
	if plan.Meta.TrackID != "" && page.FeatureID == "" {
		page.FeatureID = plan.Meta.TrackID
	}
	// Always prefer the original creation date from the YAML over the render-time
	// default set by BuildFromTopic/BuildFromWorkItem. The render-time default was
	// misleading on re-render (header read "Created [today]" even for old plans).
	if plan.Meta.CreatedAt != "" {
		page.Date = plan.Meta.CreatedAt
	}
	// Surface the monotonic version counter from meta.version so the header can
	// show the plan's current revision (v1, v2, v3, ...).
	if plan.Meta.Version > 0 {
		page.Version = plan.Meta.Version
	}
}

// applyPlanDesignToPage maps the design section (problem + goals +
// constraints → HTML).
func applyPlanDesignToPage(page *plantmpl.PlanPage, plan *planyaml.PlanYAML) {
	if plan.Design.Problem != "" || len(plan.Design.Goals) > 0 {
		var b strings.Builder
		if plan.Design.Problem != "" {
			b.WriteString("<h4>Problem</h4>\n<div data-markdown>")
			b.WriteString(html.EscapeString(plan.Design.Problem))
			b.WriteString("</div>\n")
		}
		if len(plan.Design.Goals) > 0 {
			b.WriteString("<h4>Goals</h4>\n<ol>\n")
			for _, g := range plan.Design.Goals {
				b.WriteString("<li data-markdown>")
				b.WriteString(html.EscapeString(g))
				b.WriteString("</li>\n")
			}
			b.WriteString("</ol>\n")
		}
		if len(plan.Design.Constraints) > 0 {
			b.WriteString("<h4>Constraints</h4>\n<ul>\n")
			for _, c := range plan.Design.Constraints {
				b.WriteString("<li data-markdown>")
				b.WriteString(html.EscapeString(c))
				b.WriteString("</li>\n")
			}
			b.WriteString("</ul>\n")
		}
		page.Design = &plantmpl.DesignSection{
			Content: htmltemplate.HTML(b.String()), //nolint:gosec
		}
	}
}

// applyPlanSlicesToPage maps slices from YAML (overrides any existing slices
// from node steps).
func applyPlanSlicesToPage(page *plantmpl.PlanPage, plan *planyaml.PlanYAML) {
	if len(plan.Slices) > 0 {
		page.Slices = nil
		page.Graph = &plantmpl.DependencyGraph{}
		isV2 := false
		for _, s := range plan.Slices {
			sc := plantmpl.SliceCardFromPlanSlice(s)
			page.Slices = append(page.Slices, sc)
			// Detect v2: any slice with questions or critic_revisions marks the plan as v2.
			if len(s.Questions) > 0 || len(s.CriticRevisions) > 0 {
				isV2 = true
			}
			depsStr := sc.Deps
			page.Graph.Nodes = append(page.Graph.Nodes, plantmpl.GraphNode{
				Num:       s.Num,
				Name:      s.Title,
				Deps:      depsStr,
				Files:     len(s.Files),
				Status:    plantmpl.ApprovalStatusToGraphStatus(s.ApprovalStatus),
				Issues:    len(s.CriticRevisions),
				Questions: len(s.Questions),
			})
		}
		page.IsV2 = isV2

		// Build the visual overview zone from all slices' blocks.
		page.VisualOverview = &plantmpl.VisualOverviewZone{Slices: plan.Slices}
	}
}

// applyPlanQuestionsToPage maps questions to DecisionCards.
// Only set Selected when a human has explicitly answered (q.Answer != nil).
// Recommended is highlighted but not pre-selected.
func applyPlanQuestionsToPage(page *plantmpl.PlanPage, plan *planyaml.PlanYAML) {
	if len(plan.Questions) == 0 {
		return
	}
	var cards []plantmpl.DecisionCard
	for _, q := range plan.Questions {
		var opts []string
		for _, o := range q.Options {
			opts = append(opts, o.Label)
		}
		// Map answer key to label for template comparison
		selected := ""
		if q.Answer != nil {
			selected = planOptionLabel(q.Options, *q.Answer)
		}
		// Find the recommended option's label
		recommended := ""
		if q.Recommended != "" {
			recommended = planOptionLabel(q.Options, q.Recommended)
		}
		cards = append(cards, plantmpl.DecisionCard{
			ID:          q.ID,
			Text:        q.Text,
			Options:     opts,
			Selected:    selected,
			Recommended: recommended,
		})
	}
	page.Questions = &plantmpl.QuestionsSection{Cards: cards}
}

// planOptionLabel returns the label of the option whose key is key, or "".
func planOptionLabel(options []planyaml.QuestionOption, key string) string {
	for _, o := range options {
		if o.Key == key {
			return o.Label
		}
	}
	return ""
}

// applyPlanCritiqueToPage maps the critique section.
func applyPlanCritiqueToPage(page *plantmpl.PlanPage, plan *planyaml.PlanYAML) {
	if plan.Critique == nil {
		return
	}
	cz := &plantmpl.CritiqueZone{}

	for _, a := range plan.Critique.Assumptions {
		cz.Assumptions = append(cz.Assumptions, plantmpl.AssumptionResult{
			Text:     a.Text,
			Badge:    a.Status,
			Evidence: a.Evidence,
		})
	}

	for _, r := range plan.Critique.Risks {
		cz.RiskTable = append(cz.RiskTable, plantmpl.RiskRow{
			Risk:       r.Risk,
			Severity:   r.Severity,
			Mitigation: r.Mitigation,
		})
	}

	if plan.Critique.Synthesis != "" {
		cz.Synthesis = htmltemplate.HTML(html.EscapeString(plan.Critique.Synthesis)) //nolint:gosec
	}

	// Positional mapping: Critics[0] → first critic, Critics[1] → second critic.
	// Use reviewer names from YAML as titles, falling back to critic titles.
	if len(plan.Critique.Critics) > 0 {
		cz.GeminiCritique = renderCriticSectionHTML(plan.Critique.Critics[0])
		if len(plan.Critique.Reviewers) > 0 {
			cz.GeminiTitle = plan.Critique.Reviewers[0]
		} else {
			cz.GeminiTitle = plan.Critique.Critics[0].Title
		}
	}
	if len(plan.Critique.Critics) > 1 {
		cz.CopilotCritique = renderCriticSectionHTML(plan.Critique.Critics[1])
		if len(plan.Critique.Reviewers) > 1 {
			cz.CopilotTitle = plan.Critique.Reviewers[1]
		} else {
			cz.CopilotTitle = plan.Critique.Critics[1].Title
		}
	}

	page.Critique = cz
}

// renderCriticSectionHTML converts a planyaml.CriticSection into safe HTML
// for embedding in the CritiqueZone template slots.
func renderCriticSectionHTML(c planyaml.CriticSection) htmltemplate.HTML {
	var b strings.Builder
	for _, sub := range c.Sections {
		b.WriteString("<h5>")
		b.WriteString(html.EscapeString(sub.Heading))
		b.WriteString("</h5>\n<ul>\n")
		for _, item := range sub.Items {
			b.WriteString(`  <li><span class="badge badge-`)
			b.WriteString(html.EscapeString(item.Kind))
			b.WriteString(`">`)
			b.WriteString(html.EscapeString(item.Badge))
			b.WriteString("</span> ")
			b.WriteString(html.EscapeString(item.Text))
			b.WriteString("</li>\n")
		}
		b.WriteString("</ul>\n")
	}
	return htmltemplate.HTML(b.String()) //nolint:gosec
}

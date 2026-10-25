package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

// buildDiffBlob builds the diff text to embed in the compliance prompt.
//
// Commits are Git-derived: canonicalLinkedCommits finds the commits whose
// messages name featureID under wipnote's commit convention — the same source
// the completion gate uses (workitem_provenance_canonical.go). It replaces two
// reads of the derived index that can no longer return anything:
// GetCommitsByFeature (git_commits) and, as a fallback, ListFilesByFeature
// (feature_files). Neither table is in the hydrate set, so both returned empty
// on every call and the compliance prompt silently shipped with no diff
// (feat-fc3cc9e0).
//
// The database parameter is retained because diffBuilderFn is a stubbing seam
// with several test doubles bound to this signature; nothing here reads it.
//
// Returns (diffText, truncated, error). An item whose commits do not name it
// yields an empty diff, which the caller already handles.
func buildDiffBlob(ctx context.Context, database *sql.DB, featureID, gitRoot string, maxChars int) (string, bool, error) {
	_ = database

	// The work item's node is passed so a commit linked by hand via
	// `wipnote <type> link-commit` counts here exactly as it does for the
	// completion gate. Best-effort: a node that cannot be read yields the
	// message-linkage commits alone, which is the common case anyway.
	var node *models.Node
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		if path := workitem.NodePath(wipnoteDir, featureID); path != "" {
			node, _ = htmlparse.ParseFile(path)
		}
	}

	var diffParts []string
	for _, hash := range canonicalLinkedCommits(gitRoot, featureID, node) {
		args := []string{"-C", gitRoot, "show", "--no-color", "--first-parent", hash}
		out, err := exec.CommandContext(ctx, "git", args...).Output()
		if err != nil {
			// Skip commits not present locally (shallow clone safety).
			continue
		}
		diffParts = append(diffParts, string(out))
	}

	if len(diffParts) == 0 {
		return "", false, nil
	}

	combined := strings.Join(diffParts, "\n---\n")
	return truncateDiff(combined, maxChars)
}

// truncateDiff truncates a diff string at the nearest line boundary at or before
// maxChars. Returns (truncated text, wasTruncated). If diff is shorter than
// maxChars, it is returned as-is with wasTruncated=false.
func truncateDiff(diff string, maxChars int) (string, bool, error) {
	if len(diff) <= maxChars {
		return diff, false, nil
	}

	// Find the last newline at or before maxChars.
	cutAt := maxChars
	for cutAt > 0 && diff[cutAt] != '\n' {
		cutAt--
	}
	if cutAt == 0 {
		// No newline found — hard cut at maxChars.
		cutAt = maxChars
	}

	truncated := diff[:cutAt]
	footer := fmt.Sprintf("\n... [truncated %d chars]", len(diff)-cutAt)
	return truncated + footer, true, nil
}

// computeSpecHash returns the hex SHA-256 of spec content (first 8 chars).
func computeSpecHash(spec string) string {
	h := sha256.Sum256([]byte(spec))
	return fmt.Sprintf("%x", h[:4])
}

// extractPrevSpecHash reads the data-spec-hash attribute from an existing
// <section class="compliance-findings"> section, or returns "".
func extractPrevSpecHash(featureHTML string) string {
	const openTag = `<section class="compliance-findings"`
	start := strings.Index(featureHTML, openTag)
	if start == -1 {
		return ""
	}
	// Find the end of the opening tag.
	tagEnd := strings.Index(featureHTML[start:], ">")
	if tagEnd == -1 {
		return ""
	}
	tagContent := featureHTML[start : start+tagEnd]

	// Extract data-spec-hash="..." attribute.
	prefix := `data-spec-hash="`
	hashIdx := strings.Index(tagContent, prefix)
	if hashIdx == -1 {
		return ""
	}
	rest := tagContent[hashIdx+len(prefix):]
	quoteEnd := strings.Index(rest, `"`)
	if quoteEnd == -1 {
		return ""
	}
	return rest[:quoteEnd]
}

// buildComplianceSystemPrompt returns the system prompt for the LLM.
func buildComplianceSystemPrompt() string {
	return `Return ONLY valid JSON. No prose, no markdown fences.
Schema: {"summary": string, "criteria": [{"text": string, "status": "pass"|"fail"|"unclear", "evidence": string}], "score": 0-100, "notes": string}`
}

// buildOutcomeEvidence renders the per-item outcome facts available at
// grading time into a plain-text block for the compliance prompt
// (feat-f9118b9c): this item's own gate pass/fail history, plus — when a
// feat-7ee73444 rollup exists — its measured failure rate, retries, and edit
// churn. Absence is written out explicitly rather than omitted, because an
// LLM grader that sees only "matched spec" text for an unmeasured item has no
// way to distinguish that from "measured and clean" unless told so — that
// conflation is exactly what this evidence block exists to prevent. Cost is
// deliberately never included: it ships degraded
// (workitem.SourceOtelCost) and a grading signal cannot represent that
// caveat, so it is left out rather than silently promoted.
func buildOutcomeEvidence(sig workitem.ItemRollupSignal, gatePass, gateFail int) string {
	var b strings.Builder

	if gatePass+gateFail == 0 {
		b.WriteString("Gate history: no gate runs recorded for this item (unmeasured — do not treat as passing).\n")
	} else {
		b.WriteString(fmt.Sprintf("Gate history: %d passed / %d failed (%d runs recorded).\n",
			gatePass, gateFail, gatePass+gateFail))
	}

	if !sig.Measured {
		b.WriteString("Runtime outcome: unmeasured — no telemetry rollup exists for this item (do not treat as healthy).\n")
		return b.String()
	}

	if sig.HasFailureRate {
		b.WriteString(fmt.Sprintf("Measured failure rate: %.2f%% (%s, source %s).\n",
			sig.FailureRate*100, sig.FailureRateCoverage, sig.FailureRateSource))
	} else {
		b.WriteString("Measured failure rate: unmeasured for this item.\n")
	}
	if sig.HasRetries {
		b.WriteString(fmt.Sprintf("Retries observed: %d (%s).\n", sig.Retries, sig.RetriesCoverage))
	}
	if sig.HasChurnFiles {
		b.WriteString(fmt.Sprintf("Files edited more than once: %d (%s).\n", sig.ChurnFiles, sig.ChurnFilesCoverage))
	}
	if sig.TelemetryUnavailable {
		b.WriteString("Telemetry: unavailable for this item's harness (do not treat as clean).\n")
	}

	return b.String()
}

// buildComplianceUserPrompt builds the user prompt embedding spec, diff, and
// outcome evidence (feat-f9118b9c) — the join that lets a compliance verdict
// reflect whether the result worked, not only whether the diff matched the
// spec.
func buildComplianceUserPrompt(specContent, diffBlob, outcomeEvidence string) string {
	return fmt.Sprintf(`You are a code compliance reviewer. Compare the feature spec against the implementation diff.

## Feature Spec

%s

## Implementation Diff

%s

## Outcome Evidence

%s
Absence of evidence above means the outcome is UNMEASURED, not passing. Do not treat missing runtime or gate data as a clean result. A high measured failure rate or a majority of failed gate runs should weigh against the score even when the diff otherwise looks complete.

## Instructions

For each acceptance criterion in the spec, determine if the diff shows it was implemented (pass), explicitly not implemented (fail), or uncertain (unclear). Weigh the outcome evidence above alongside the diff: a criterion whose code looks right but whose gate or runtime evidence shows failure should not be scored as a clean pass. Return your assessment as JSON matching the schema.`, specContent, diffBlob, outcomeEvidence)
}

// parseComplianceFinding parses the LLM's JSON response into an autoComplianceFinding.
// The LLM may return a JSON object with a "result" wrapper or directly.
func parseComplianceFinding(raw string) (*autoComplianceFinding, error) {
	raw = strings.TrimSpace(raw)

	// Try direct parse first.
	var finding autoComplianceFinding
	if err := json.Unmarshal([]byte(raw), &finding); err == nil {
		return &finding, nil
	}

	// Try wrapped in {"result": ...}.
	var wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil && wrapper.Result != nil {
		if err2 := json.Unmarshal(wrapper.Result, &finding); err2 == nil {
			return &finding, nil
		}
	}

	return nil, fmt.Errorf("cannot parse LLM response as compliance JSON: %q", truncateStr(raw, 200))
}

// renderFindingsHTML renders a finding into the inner HTML for the compliance section.
func renderFindingsHTML(f *autoComplianceFinding) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<h3>Compliance Score: %d/100</h3>\n", f.Score))
	sb.WriteString(fmt.Sprintf("<p><strong>Summary:</strong> %s</p>\n", html.EscapeString(f.Summary)))

	if len(f.Criteria) > 0 {
		sb.WriteString("<ul>\n")
		for _, c := range f.Criteria {
			icon := "?"
			switch c.Status {
			case "pass":
				icon = "&#x2705;"
			case "fail":
				icon = "&#x274C;"
			}
			sb.WriteString(fmt.Sprintf("  <li>%s <strong>%s</strong>: %s",
				icon, html.EscapeString(c.Text), html.EscapeString(c.Status)))
			if c.Evidence != "" {
				sb.WriteString(fmt.Sprintf(" — <em>%s</em>", html.EscapeString(c.Evidence)))
			}
			sb.WriteString("</li>\n")
		}
		sb.WriteString("</ul>\n")
	}

	if f.Notes != "" {
		sb.WriteString(fmt.Sprintf("<p><em>Notes: %s</em></p>\n", html.EscapeString(f.Notes)))
	}

	return sb.String()
}

// writeSkipFinding writes a "compliance skipped: <reason>" finding to the feature HTML.
// It creates the feature file if it doesn't exist (no-op if feature file is missing).
func writeSkipFinding(featurePath, reason string) error {
	if _, err := os.Stat(featurePath); err != nil {
		// Feature file doesn't exist — nothing to write to.
		return nil
	}

	attrs := map[string]string{
		"score":          "0",
		"cost-usd":       "0",
		"model":          "none",
		"spec-hash":      "none",
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"diff-truncated": "false",
	}
	body := fmt.Sprintf("<p><em>%s</em></p>", html.EscapeString(reason))
	return writeComplianceSection(featurePath, attrs, body)
}

// truncateStr truncates a string to at most maxLen chars, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

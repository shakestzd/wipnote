package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/workitem"
)

// checkProvenanceCompleteGate is the type-agnostic provenance gate for
// code-bearing bug/feature/spike completion. It runs as a pre-completion
// gate (before col.Complete and slice-1's transactional commit) so a
// blocked item is never transitioned to "done".
//
// Decision:
//   - Item not code-bearing (no canonical evidence of a source path outside
//     .wipnote/) → exempt, complete normally.
//   - Code-bearing AND >=1 linked source commit → complete normally.
//   - Code-bearing AND zero linked commits AND no advisory → BLOCK with a
//     non-zero error carrying the --accepted-advisory remediation.
//   - Code-bearing AND zero linked commits AND advisory set → complete,
//     persisting the rationale on the .wipnote artifact (Properties +
//     audit note) and emitting a stderr warning.
//
// Both facts come from canonical storage, never from a read index: linked
// commits are parsed out of git history under wipnote's commit convention and
// code-bearing paths from those commits' diffs, falling back to uncommitted
// working-tree source for an item an agent demonstrably implemented. See
// workitem_provenance_canonical.go for the derivation and its known narrowing.
func checkProvenanceCompleteGate(col *workitem.Collection, ev *completionEvidence, typeName, id, advisory string) error {
	commits := ev.linkedCommits()
	codePaths, scope := ev.codeBearingPaths()
	if len(codePaths) == 0 {
		// Pure-.wipnote/doc item — exempt.
		return nil
	}

	if len(commits) > 0 {
		// At least one linked source commit — provenance satisfied.
		return nil
	}

	advisory = strings.TrimSpace(advisory)
	if advisory == "" {
		preview := codePaths
		if len(preview) > 5 {
			preview = preview[:5]
		}
		return fmt.Errorf(
			"refusing to complete %s %s: it is code-bearing (touched %d source path(s) outside .wipnote/, e.g. %s) "+
				"but has zero linked source commits — no durable provenance for the implementation.%s\n"+
				"Commit the implementation and link it, then rerun:\n  wipnote %s complete %s\n"+
				"To intentionally accept completion without a source commit (records an audited rationale on the artifact), rerun with:\n"+
				"  wipnote %s complete %s --accepted-advisory \"<reason>\"",
			typeName, id, len(codePaths), strings.Join(preview, ", "), scannedTreeNote(scope),
			typeName, id, typeName, id)
	}

	// Audited override: persist the rationale on the canonical artifact so
	// `wipnote check accepted-advisory`, `wipnote show`, and snapshot tooling
	// surface it. Persist BEFORE col.Complete so the property survives the
	// completion flush and the transactional commit captures it.
	if err := col.Edit(id).
		AddNote(acceptedAdvisoryMarker + advisory).
		Save(); err != nil {
		return fmt.Errorf("provenance gate: record accepted-advisory on %s: %w", id, err)
	}
	fmt.Fprintf(os.Stderr,
		"accepted-advisory warning: completing code-bearing %s %s with zero linked source commits.\n  reason: %s\n",
		typeName, id, advisory)
	return nil
}

// researchEvidenceMarker prefixes the content note recording the research URL(s)
// / waiver supplied at completion, so audit tooling can surface it.
const researchEvidenceMarker = "research-evidence: "

// wiIsResearchURL reports whether u is a usable http(s) source URL — it must
// parse, carry an http/https scheme, AND have a non-empty host. Mirrors
// plan/planyaml.isResearchURL so the completion gate and the v4 plan gate apply
// the same shape check.
func wiIsResearchURL(u string) bool {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

// checkDependencyResearchCompleteGate enforces the work-item analogue of the v4
// plan research gate (plan/planyaml/validate.go:394-400). When the item's
// code-bearing paths include a dependency manifest (go.mod/package.json/…) — i.e.
// the work changed an external dependency — completion must cite at least one
// http(s) research URL (--research-url) OR record an explicit --research-waiver.
// The cited evidence is persisted on the .wipnote artifact for audit.
//
// Items that touch no dependency manifest are exempt (return nil), so ordinary
// feature/bug completion is unaffected (feat-d1bcbf10 / spk-0a982f70).
func checkDependencyResearchCompleteGate(col *workitem.Collection, ev *completionEvidence, typeName, id string, researchURLs []string, researchWaiver string) error {
	// Shape-check EVERY supplied --research-url up front (mirrors the v4 plan
	// validator, which validates the shape of every research URL regardless of
	// enforcement). A single invalid entry is a hard error rather than being
	// silently dropped when another valid URL or a waiver is present
	// (roborev #580).
	for _, u := range researchURLs {
		if !wiIsResearchURL(u) {
			return fmt.Errorf(
				"refusing to complete %s %s: --research-url %q must be an http(s) URL with a host",
				typeName, id, u)
		}
	}

	codePaths, _ := ev.codeBearingPaths()
	manifests := dependencyManifests(codePaths)
	if len(manifests) == 0 {
		return nil // no dependency change → exempt
	}

	validURLs := researchURLs // all entries are valid (checked above)
	waiver := strings.TrimSpace(researchWaiver)
	if len(validURLs) == 0 && waiver == "" {
		return fmt.Errorf(
			"refusing to complete %s %s: it changes dependency manifest(s) (%s) but cites no web research.\n"+
				"Pass --research-url <https://…> with the docs/changelog you verified, or --research-waiver \"<reason>\".",
			typeName, id, strings.Join(manifests, ", "))
	}

	// Persist the cited evidence / waiver on the canonical artifact for audit,
	// mirroring how --accepted-advisory records its rationale. Done BEFORE
	// col.Complete so the note survives the completion flush.
	note := researchEvidenceNote(validURLs, waiver)
	if err := col.Edit(id).AddNote(note).Save(); err != nil {
		return fmt.Errorf("research gate: record research evidence on %s: %w", id, err)
	}
	// The same evidence also lands on the structured research record.
	if err := recordDependencyResearch(col, id, validURLs, waiver); err != nil {
		return fmt.Errorf("research gate: record research evidence on %s: %w", id, err)
	}
	return nil
}

// dependencyManifests returns the code paths that are dependency manifests
// (go.mod/package.json/…), in order.
func dependencyManifests(codePaths []string) []string {
	var manifests []string
	for _, cp := range codePaths {
		if paths.IsDependencyManifest(cp) {
			manifests = append(manifests, cp)
		}
	}
	return manifests
}

// researchEvidenceNote builds the researchEvidenceMarker note recording the
// cited research URLs and/or waiver.
func researchEvidenceNote(validURLs []string, waiver string) string {
	var note string
	if len(validURLs) > 0 {
		note = researchEvidenceMarker + strings.Join(validURLs, ", ")
	}
	if waiver != "" {
		if note != "" {
			note += " | "
		} else {
			note = researchEvidenceMarker
		}
		note += "waiver: " + waiver
	}
	return note
}

// checkFeatureCompleteSpecGate enforces config.spec_enforcement.feature_complete:
// the feature HTML's <section class="spec"> must exist and contain at least one
// usable criterion (either an OpenSpec ### Requirement: with a non-empty SHALL
// line, or a legacy [ ]/[x]/[F] checkbox line under ## Acceptance Criteria).
//
// Returns nil when the gate is disabled, the feature has a non-empty spec, or
// allowSpecSkip is set. Returns a remediation error otherwise.
func checkFeatureCompleteSpecGate(wipnoteDir, featureID string) error {
	enforcement := hooks.ReadSpecEnforcement(filepath.Dir(wipnoteDir))
	if !enforcement.FeatureComplete {
		return nil
	}

	featurePath := filepath.Join(wipnoteDir, "features", featureID+".html")
	raw, err := os.ReadFile(featurePath)
	if err != nil {
		// Feature file unreadable — let the normal Complete path raise the
		// canonical error; we do not block on missing files.
		return nil
	}
	specContent := extractSpecSection(string(raw))
	if specContent == "" {
		return fmt.Errorf("feature %s has no spec section; run `wipnote spec generate %s --insert` first (or follow phase 7 of /wipnote:plan). Override with --allow-spec-skip if intentional.",
			featureID, featureID)
	}
	criteria := parseCriteria(unwrapPreBlock(specContent))
	if len(criteria) == 0 {
		return fmt.Errorf("feature %s spec section has 0 criteria; populate Requirements or Acceptance Criteria, or override with --allow-spec-skip",
			featureID)
	}
	return nil
}

func checkCompletionGateRecord(database *sql.DB, projectRoot, sessionID, workItemID string) error {
	if database == nil {
		return nil
	}
	codePaths, err := dbpkg.CodeBearingPaths(database, workItemID, projectRoot)
	if err != nil {
		return fmt.Errorf("quality gate: inspect code-bearing paths for %s: %w", workItemID, err)
	}
	if len(codePaths) == 0 {
		return nil
	}
	reportGuardProfileDrift(database, projectRoot, sessionID, os.Stderr)
	return validateCompletionGateRecord(projectRoot, sessionID, workItemID)
}

// unwrapPreBlock strips a leading/trailing <pre>...</pre> wrapper plus HTML
// entity escapes that slice 1's `spec --insert` writer applies. Leaves
// non-wrapped content untouched.
func unwrapPreBlock(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "<pre>") && strings.HasSuffix(t, "</pre>") {
		t = strings.TrimSuffix(strings.TrimPrefix(t, "<pre>"), "</pre>")
	}
	r := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")
	return r.Replace(t)
}

package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/research"
	"github.com/shakestzd/wipnote/core/workitem"
)

// emitResearchStartAdvisory warns when a gated item starts with no research
// record. Starting never fails on it.
func emitResearchStartAdvisory(w io.Writer, projectRoot string, node *models.Node) {
	if msg := research.CheckStart(research.LoadConfig(projectRoot), node); msg != "" {
		fmt.Fprintln(w, msg)
	}
}

// checkResearchCompleteGate applies the research-first gate at completion and
// at `check --gate --work-item`. A non-empty waiver is recorded on the artifact
// and satisfies the gate. In warn mode problems are printed; in block mode an
// incomplete record is an error.
func checkResearchCompleteGate(w io.Writer, projectRoot string, col *workitem.Collection, id, waiver string) error {
	if strings.TrimSpace(waiver) != "" {
		if err := recordResearchWaiver(col, id, waiver); err != nil {
			return fmt.Errorf("research gate: record waiver on %s: %w", id, err)
		}
	}
	node, err := col.Get(id)
	if err != nil {
		return nil // unreadable item: let the caller's normal path report it
	}
	warns, gateErr := research.CheckComplete(research.LoadConfig(projectRoot), node)
	for _, msg := range warns {
		fmt.Fprintln(w, msg)
	}
	return gateErr
}

// checkResearchGateForWorkItem is the `check --gate --work-item` entry point.
func checkResearchGateForWorkItem(w io.Writer, projectRoot, id, waiver string) error {
	p, err := workitem.Open(filepath.Join(projectRoot, ".wipnote"), "claude-code")
	if err != nil {
		return nil // no project: the session gate reports it
	}
	defer p.Close()
	return checkResearchCompleteGate(w, projectRoot, collectionFor(p, typeNameForID(id)), id, waiver)
}

// recordDependencyResearch stores --research-url URLs (as primary sources) and
// a waiver on the research record, so the dependency gate and the research
// gate share one record.
func recordDependencyResearch(col *workitem.Collection, id string, urls []string, waiver string) error {
	rec := &research.Record{Waiver: strings.TrimSpace(waiver)}
	date := nowDate()
	for _, u := range urls {
		rec.Sources = append(rec.Sources, research.Source{URL: u, Type: research.SourcePrimary, Retrieved: date})
	}
	_, err := saveResearchRecord(col, id, rec)
	return err
}

// recordHasDependencyEvidence reports whether an earlier `research add` or
// waiver already satisfies the dependency gate.
func recordHasDependencyEvidence(n *models.Node) bool {
	rec := research.FromNode(n)
	return rec != nil && (len(rec.Sources) > 0 || strings.TrimSpace(rec.Waiver) != "")
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/core/workitem"
	iarch "github.com/shakestzd/wipnote/internal/arch"
)

func runWiSetStatus(typeName, id, status string) error {
	// Shared resolver: the same one `wipnote who` prints and the PreToolUse
	// gate enforces against, so a claim can never bind to a session the hook
	// will not recognise (issue #148).
	sessionID, _ := hooks.ResolveSessionID("")
	agentID := dbpkg.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID"))
	return wiSetStatusWithAgent(typeName, id, status, sessionID, agentID)
}

// legacyActiveFeatureAgents is the closed set of agent identities permitted to
// occupy sessions.active_feature_id, in PRECEDENCE order (root first).
//
// applyActiveFeatureIDFromClaims consumes this same list deliberately. The
// writer and the hydrator disagreed once already: this function accepted
// "codex", while hydration projected only __root__ into the column, so a Codex
// launcher's claim was written at runtime and then silently dropped on every
// projection rebuild. Keeping one definition is what stops them drifting apart
// again.
func legacyActiveFeatureAgents() []string {
	return []string{dbpkg.AgentRootSentinel, "codex"}
}

// wiSetStatusWithAgent is the testable core of runWiSetStatus that accepts
// explicit sessionID and agentID instead of reading them from the environment.
// This allows concurrent tests to call it with distinct agent identities without
// env-var races.
func wiSetStatusWithAgent(typeName, id, status, sessionID, agentID string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	id, err = workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	t := &statusTransition{
		dir: dir, typeName: typeName, id: id, status: status,
		sessionID: sessionID, agentID: agentID,
		p: p, col: collectionFor(p, typeName),
	}
	if err := t.runCompletionGates(); err != nil {
		return err
	}
	t.prepareArtifactCommit()
	if err := t.applyTransition(); err != nil {
		return err
	}
	t.afterTransition()
	if stop, err := t.commitArtifact(); stop || err != nil {
		return err
	}

	// Every abort path above has returned by now, so the completion has stuck.
	sweepClaimsOnCompletion(dir, id, status, t.transitionStartedAt)

	// Update status line cache for subagent visibility.
	if status == "in-progress" {
		WriteStatuslineCache(dir, id)
	} else {
		WriteStatuslineCache(dir, "")
	}

	t.attachLearning()
	t.report()
	return nil
}

// statusTransition carries the state shared by the phases of
// wiSetStatusWithAgent: the resolved item, the gate evidence, the artifact
// commit policy and the node the transition produced.
type statusTransition struct {
	dir, typeName, id, status, sessionID, agentID string

	p   *workitem.Project
	col *workitem.Collection

	dirtyNote string
	// One evidence value for every gate below and the auto-link, so the
	// linked-commit git walk runs once per completion (feat-c30d0792).
	evidence *completionEvidence

	transactionalComplete bool
	deferredComplete      bool
	artifactPreHead       string

	transitionStartedAt time.Time
	node                *models.Node
}

// codeBearingDone reports whether this transition completes a code-bearing
// item whose artifact wipnote autocommits.
func (t *statusTransition) codeBearingDone() bool {
	return t.status == "done" && shouldAutocommitWorkitemArtifact(t.typeName)
}

// runCompletionGates runs every pre-transition gate in order. A blocked item
// never reaches the "done" transition.
func (t *statusTransition) runCompletionGates() error {
	dir, typeName, id, status, col := t.dir, t.typeName, t.id, t.status, t.col

	// CRISPI spec-enforcement gate: when completing a feature with the
	// gate opted in via config, refuse if the feature HTML has no usable
	// spec section. --allow-spec-skip provides an audited bypass.
	if typeName == "feature" && status == "done" && !wiAllowSpecSkip {
		if err := checkFeatureCompleteSpecGate(dir, id); err != nil {
			return err
		}
	}

	if t.codeBearingDone() {
		gateNode, _ := col.Get(id)
		t.evidence = newCompletionEvidence(dir, id, gateNode)
		note, err := checkUncommittedSourceCompleteGate(t.evidence, wiAllowDirtyComplete)
		if err != nil {
			return err
		}
		t.dirtyNote = note
	}
	if err := t.runCodeBearingGates(); err != nil {
		return err
	}

	// Research-first gate (docs/design/research-gate.md): gated change kinds
	// need a research record or --research-waiver; config decides warn vs block.
	if status == "done" {
		if err := checkResearchCompleteGate(os.Stderr, filepath.Dir(dir), col, id, wiResearchWaiver); err != nil {
			return fmt.Errorf("refusing to complete %s %s: %w", typeName, id, err)
		}
	}
	return t.runGateRecordCheck()
}

// runCodeBearingGates runs the provenance, merged-upstream and
// dependency-research completion gates, in that order.
func (t *statusTransition) runCodeBearingGates() error {
	col, evidence, typeName, id := t.col, t.evidence, t.typeName, t.id

	// Provenance completion gate (feat-7b593272). Type-agnostic: covers
	// code-bearing feature/bug/spike items. Runs BEFORE col.Complete and
	// slice-1's transactional commit path so a blocked item never reaches
	// the "done" transition — composing cleanly with the transactional
	// complete (no compensating re-open needed because no transition fired).
	// --accepted-advisory provides an audited override that records the
	// rationale on the .wipnote artifact for compliance/snapshot tooling.
	if t.codeBearingDone() {
		if err := checkProvenanceCompleteGate(col, evidence, typeName, id, wiAcceptedAdvisory); err != nil {
			return err
		}
	}

	// Merged-upstream completion gate (feat-be42685c). A linked commit that
	// only exists on an unmerged branch is provenance but not delivery: at
	// least one must be reachable from the upstream default branch, or the
	// operator records an --allow-orphan rationale. Skipped without a remote.
	if t.codeBearingDone() {
		if err := checkMergedUpstreamCompleteGate(col, evidence, typeName, id, wiAllowOrphan); err != nil {
			return err
		}
	}

	// Dependency-research completion gate (feat-d1bcbf10). When the item's
	// code-bearing paths include a dependency manifest (go.mod/package.json/…),
	// completion must cite a research URL or record an explicit waiver — mirroring
	// the v4 plan research gate. Runs after the provenance gate so it only fires
	// for items that actually carry committed source. --accepted-advisory does
	// NOT bypass it: research evidence and commit provenance are orthogonal.
	if t.codeBearingDone() {
		if err := checkDependencyResearchCompleteGate(col, evidence, typeName, id, wiResearchURL, wiResearchWaiver); err != nil {
			return err
		}
	}
	return nil
}

// runGateRecordCheck enforces the completion gate record for code-bearing
// completions unless --accepted-advisory overrides it.
func (t *statusTransition) runGateRecordCheck() error {
	if t.codeBearingDone() {
		if strings.TrimSpace(wiAcceptedAdvisory) != "" {
			// --accepted-advisory is an audited override that composes past the
			// gate-record guard in the same way it already overrides the
			// provenance gate above. The rationale is already being recorded on
			// the artifact by checkProvenanceCompleteGate; we simply skip guard 4
			// here so the operator is not double-blocked when the gate cannot
			// produce a passing record (e.g. manifest-less or broken runner).
			fmt.Fprintln(os.Stderr, "gate-record check bypassed via --accepted-advisory")
		} else if err := checkCompletionGateRecord(nil, filepath.Dir(t.dir), t.sessionID, t.id); err != nil {
			return err
		}
	}
	return nil
}

// prepareArtifactCommit resolves the artifact commit policy and captures the
// artifact's pre-commit HEAD BEFORE col.Complete flushes the canonical HTML to
// disk. The transactional complete path compares this against the post-commit
// HEAD to assert the commit actually advanced.
func (t *statusTransition) prepareArtifactCommit() {
	t.transactionalComplete = t.codeBearingDone() &&
		workitemArtifactCommitPolicyForEnv() == workitemArtifactCommitPolicySeparate
	t.deferredComplete = t.codeBearingDone() &&
		workitemArtifactCommitPolicyForEnv() == workitemArtifactCommitPolicyDefer
	if t.transactionalComplete {
		repoRoot := filepath.Dir(t.dir)
		absArtifact := filepath.Join(t.dir, t.typeName+"s", t.id+".html")
		t.artifactPreHead = artifactHeadCommit(repoRoot, absArtifact)
	}
}

// applyTransition runs the live-collision gate for starts, then performs the
// status transition itself and records the node it produced.
func (t *statusTransition) applyTransition() error {
	dir, id, status, col := t.dir, t.id, t.status, t.col

	// Live-collision gate (feat-5a9839fb): before acquiring the claim, check
	// whether another live session already holds this item. A live collision
	// (foreign session whose root is still open in the sessions ledger) is a
	// HARD REFUSAL unless --force was supplied. A claim whose session has
	// ended is NOT a refusal — the item is reclaimable. See liveForeignClaims.
	if status == "in-progress" && !wiForceStart {
		if err := checkLiveCollisionGate(dir, id, t.sessionID); err != nil {
			return err
		}
	}
	// The claim sweep is bounded by the transition's own timestamp, read from the
	// node Complete wrote inside the item lock. The wall clock here is only a
	// fallback: a timestamp taken outside the lock cannot be ordered against a
	// concurrent start.
	t.transitionStartedAt = time.Now().UTC()
	var node *models.Node
	var err error
	switch status {
	case "in-progress":
		node, err = col.Start(id, claimEpisodeOnStart(dir, t.sessionID, t.agentID, id))
	case "blocked":
		err = col.Edit(id).SetStatus("blocked").Save()
		if err == nil {
			node, err = col.Get(id)
		}
	default:
		node, err = col.Complete(id)
		if err == nil && node != nil && !node.UpdatedAt.IsZero() {
			t.transitionStartedAt = node.UpdatedAt
		}
	}
	if err != nil {
		return fmt.Errorf("cannot set %s %s to %s: %w\nRun 'wipnote wip' to see active items or 'wipnote %s list' to see valid IDs.", t.typeName, id, status, err, t.typeName)
	}
	t.node = node
	return nil
}

// afterTransition runs the start- and completion-side bookkeeping that
// follows a successful transition.
func (t *statusTransition) afterTransition() {
	if t.status == "in-progress" {
		t.afterStart()
	}
	if t.status == "done" {
		t.afterDone()
	}
}

// afterStart updates per-agent attribution, creates a claim with per-agent
// attribution, and creates an implemented_in edge when starting a work item.
func (t *statusTransition) afterStart() {
	dir, id, sessionID := t.dir, t.id, t.sessionID
	// Non-blocking already-fixed advisory (feat-89d7b057): an item with
	// linked commits, or whose id is cited in source, may have been fixed
	// and never closed. Print before any dispatch happens; never refuse.
	emitAlreadyFixedAdvisory(os.Stderr, filepath.Dir(dir), id, t.node)
	emitResearchStartAdvisory(os.Stderr, filepath.Dir(dir), t.node)
	if sessionID != "" {
		// Durable claim history (feat-21d12cdb) was recorded inside col.Start's
		// item lock by claimEpisodeOnStart, ordered with the transition.
		autoImplementedInEdge(t.col, id, sessionID)
		// Non-fatal advisory: warn when this session now owns >= wipPerSessionSoftLimit
		// in-progress items. Never blocks; yolo/orchestrator may legitimately pre-start.
		if allItems, scanErr := scanInProgress(dir); scanErr == nil {
			bysess := wipGroupBySession(allItems)
			if len(bysess[sessionID]) >= wipPerSessionSoftLimit {
				fmt.Fprintf(os.Stderr,
					"wip advisory: session %s now owns %d in-progress items (soft limit %d) — consider completing before starting more\n",
					slug.Truncate(sessionID, 16), len(bysess[sessionID]), wipPerSessionSoftLimit)
			}
		}
	}
}

// afterDone clears active_work_items and the legacy active_feature_id on any
// session still pointing at the completed item.
func (t *statusTransition) afterDone() {
	dir, id := t.dir, t.id
	// Persist the commits whose messages name this item as committed_in
	// edges, BEFORE the artifact commit below so the durable record carries
	// them (bug-0816b822). Non-fatal.
	if shouldAutocommitWorkitemArtifact(t.typeName) {
		autoLinkMessageDerivedCommits(os.Stderr, t.col, filepath.Dir(dir), id, t.evidence.linkedCommits())
	}
	if t.typeName == "spike" {
		attachSpikeResearch(os.Stderr, t.p, id)
	}
	if t.sessionID != "" {
		// Close the claim episode in place, giving the interval its end.
		recordClaimEpisodeClose(nil, dir, t.sessionID, t.agentID, id, claimledger.OutcomeCompleted)
	}
}

// attachLearning creates the learning arch card after successful completion.
// Learning validation is NON-FATAL: the work item is already done (state changed),
// so an invalid learning must not block completion. Instead, warn and skip attaching.
func (t *statusTransition) attachLearning() {
	if t.status != "done" || strings.TrimSpace(wiLearning) == "" {
		return
	}
	dir, id := t.dir, t.id
	// Validate the learning body and kind (non-fatally, after state change).
	learningBody := strings.TrimSpace(wiLearning)
	validationErr := validateLearningKind(wiLearningKind)
	if validationErr == nil {
		validationErr = validateLearningBody(learningBody)
	}

	if validationErr == nil {
		// Validation passed: create the learning card.
		touchedPaths, _ := resolveWorkItemPaths(id, dir)
		if cerr := createLearningCard(dir, id, learningBody, wiLearningKind, touchedPaths); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to create learning card: %v\n", cerr)
		}
		return
	}
	// Validation failed: emit warning and print remediation command.
	// The correct way to attach a learning post-completion is via
	// "wipnote arch add", not a non-existent "<type> edit --learning".
	// Use a known-valid default kind (decision) in the remediation command.
	// --links accepts work item IDs; --paths accepts file glob patterns.
	slug := "learning-" + id
	fmt.Fprintf(os.Stderr,
		"warning: --learning validation failed (learning NOT attached): %v\n"+
			"learning body was: %q\n"+
			"to attach a valid learning, run:\n  wipnote arch add %s --kind decision --body %s --links %s --created-by wipnote-completion\n",
		validationErr,
		learningBody,
		shellQuote(slug),
		shellQuote(learningBody),
		shellQuote(id))
}

// report prints the transition summary and the best-effort post-completion
// nudges.
func (t *statusTransition) report() {
	dir, typeName, id, status, node := t.dir, t.typeName, t.id, t.status, t.node
	verb := "Started"
	switch status {
	case "done":
		verb = "Completed"
	case "blocked":
		verb = "Blocked"
	}
	fmt.Printf("%s: %s  %s\n", verb, node.ID, node.Title)
	if t.dirtyNote != "" {
		fmt.Println(t.dirtyNote)
	}
	if status == "done" && strings.TrimSpace(wiAcceptedAdvisory) != "" {
		fmt.Printf("  accepted-advisory: %s\n", strings.TrimSpace(wiAcceptedAdvisory))
	}

	// Post-completion drift nudge: emit to stderr (best-effort, never fails completion).
	if status == "done" {
		touchedPaths, _ := resolveWorkItemPaths(id, dir)
		emitDriftNudge(os.Stderr, touchedPaths, dir, iarch.GitDiffNameOnly)
	}

	// Recap nudge: for code-bearing completions, remind the agent to run recap.
	// Non-fatal, stderr only, consistent with the emitDriftNudge pattern above.
	if t.codeBearingDone() {
		fmt.Fprintf(os.Stderr, "  ! recap: wipnote recap %s   (grounded diff recap of this work)\n", id)
	}

	// Auto plan-rollup recap: when the last feature of a plan completes, generate
	// the plan-rollup recap automatically. Best-effort, non-fatal.
	if status == "done" && typeName == "feature" {
		maybeAutoGeneratePlanRollupRecap(dir, node, t.p)
	}

	// On start, print a session-label hint tailored to the active harness,
	// and name the caller's herdr pane after the item. A subagent's start
	// leaves the pane alone: the pane belongs to the session that runs it.
	if status == "in-progress" {
		printStartTip(typeName, node.Title)
		if slices.Contains(legacyActiveFeatureAgents(), dbpkg.NormaliseAgentID(t.agentID)) {
			labelHerdrPane(node.ID, node.Title)
		}
	}
}

package hooks

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// lineageInputs holds pre-resolved data needed to insert lineage traces inside
// the transaction. All fields are computed from read-only DB queries before
// the transaction begins.
type lineageInputs struct {
	featureID       string
	rootSessionID   string
	parentSessionID string
	depth           int
	path            []string
	parentAgent     string
	myAgent         string
	needsRootSeed   bool
}

// resolveParentLineage reads the parent's lineage record and builds the inputs
// for the child trace. Pure reads — must be called before the transaction.
func resolveParentLineage(event *CloudEvent, database *sql.DB, parentSessionID, featureID string) *lineageInputs {
	parent, _ := db.GetLineageBySession(database, parentSessionID)
	inp := &lineageInputs{
		myAgent:         resolveEventAgentID(event),
		featureID:       featureID,
		parentSessionID: parentSessionID,
	}

	if parent != nil {
		inp.rootSessionID = parent.RootSessionID
		inp.depth = parent.Depth + 1
		inp.path = make([]string, len(parent.Path)+1)
		copy(inp.path, parent.Path)
		inp.path[len(parent.Path)] = inp.myAgent
	} else {
		// No parent trace: treat parent as root and seed its entry.
		inp.rootSessionID = parentSessionID
		inp.depth = 1
		inp.parentAgent = "claude-code"
		inp.path = []string{inp.parentAgent, inp.myAgent}
		inp.needsRootSeed = true
	}
	return inp
}

// routeSessionUpsert enqueues the session-row upsert through the writer daemon
// (RouteHookWrite, enqueue-only). The statement is the exact INSERT OR IGNORE the
// pre-daemon upsertSessionTx ran, so a replay/resume is idempotent (the writer
// IGNOREs the conflicting row). NO direct writable Exec is performed when the
// daemon is reachable; a held lock degrades to the bounded direct fallback inside
// RouteHookWrite (<1s) and ultimately to canonical NDJSON + reindex.
func routeSessionUpsert(projectDir, sessionID string, s *models.Session) {
	RouteHookWrite("session-start", projectDir, sessionID, `
		INSERT OR IGNORE INTO sessions
			(session_id, agent_assigned, parent_session_id, parent_event_id,
			 created_at, status, start_commit, is_subagent, model, active_feature_id,
			 git_remote_url, project_dir, exec_worktree_path, branch, harness, continued_from)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.SessionID,
		s.AgentAssigned,
		nullableStr(s.ParentSessionID),
		nullableStr(s.ParentEventID),
		s.CreatedAt.UTC().Format(time.RFC3339),
		s.Status,
		nullableStr(s.StartCommit),
		s.IsSubagent,
		nullableStr(s.Model),
		nullableStr(s.ActiveFeatureID),
		nullableStr(s.GitRemoteURL),
		nullableStr(s.ProjectDir),
		nullableStr(s.ExecWorktreePath),
		nullableStr(s.Branch),
		nullableStr(s.Harness),
		nullableStr(s.ContinuedFrom),
	)
}

// routeLineageTraces enqueues the subagent lineage trace inserts through the
// writer daemon, in the SAME order the transactional path wrote them (root seed
// first, then the child trace). FIFO ordering on the single writer guarantees
// they apply after routeSessionUpsert's session row. Each insert is its own
// enqueue-only op (RouteHookWrite) — no direct writable Exec.
func routeLineageTraces(projectDir, sessionID string, inp *lineageInputs) {
	now := time.Now().UTC()
	if inp.needsRootSeed {
		routeLineageTrace(projectDir, sessionID, &models.LineageTrace{
			TraceID:       inp.parentSessionID,
			RootSessionID: inp.parentSessionID,
			SessionID:     inp.parentSessionID,
			AgentName:     inp.parentAgent,
			Depth:         0,
			Path:          []string{inp.parentAgent},
			FeatureID:     inp.featureID,
			StartedAt:     now,
			Status:        "active",
		})
	}
	routeLineageTrace(projectDir, sessionID, &models.LineageTrace{
		TraceID:       sessionID,
		RootSessionID: inp.rootSessionID,
		SessionID:     sessionID,
		AgentName:     inp.myAgent,
		Depth:         inp.depth,
		Path:          inp.path,
		FeatureID:     inp.featureID,
		StartedAt:     now,
		Status:        "active",
	})
}

// routeLineageTrace enqueues a single agent_lineage_trace INSERT, mirroring
// db.insertLineageTrace's statement and arg marshalling (path → JSON).
func routeLineageTrace(projectDir, sessionID string, trace *models.LineageTrace) {
	pathJSON, err := json.Marshal(trace.Path)
	if err != nil {
		debugLog(projectDir, "[session-start] marshal lineage path (trace=%s): %v", trace.TraceID, err)
		return
	}
	RouteHookWrite("session-start", projectDir, sessionID, `
		INSERT INTO agent_lineage_trace
			(trace_id, root_session_id, session_id, agent_name, depth, path,
			 feature_id, started_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.TraceID,
		trace.RootSessionID,
		nullableStr(trace.SessionID),
		nullableStr(trace.AgentName),
		trace.Depth,
		string(pathJSON),
		nullableStr(trace.FeatureID),
		trace.StartedAt.UTC().Format(time.RFC3339),
		trace.Status,
	)
}

// routeInsertEvent enqueues the session-start agent_events row through the writer
// daemon, mirroring db.InsertEvent's statement. The session-start event has no
// ParentEventID, so InsertEvent's parent-agent lookup is skipped here (it would
// be a no-op). No direct writable Exec.
func routeInsertEvent(projectDir, sessionID string, e *models.AgentEvent) {
	RouteHookWrite("session-start", projectDir, sessionID, `
		INSERT INTO agent_events (
			event_id, agent_id, event_type, timestamp, tool_name,
			input_summary, tool_input, output_summary, session_id, feature_id,
			parent_agent_id, parent_event_id, subagent_type,
			cost_tokens, execution_duration_seconds, status,
			model, claude_task_id, source, step_id,
			created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?)`,
		e.EventID, e.AgentID, string(e.EventType),
		e.Timestamp.UTC().Format(time.RFC3339), nullableStr(e.ToolName),
		nullableStr(e.InputSummary), nullableStr(e.ToolInput), nullableStr(e.OutputSummary),
		e.SessionID, nullableStr(e.FeatureID),
		nullableStr(e.ParentAgentID), nullableStr(e.ParentEventID),
		nullableStr(e.SubagentType),
		e.CostTokens, e.ExecDuration, e.Status,
		nullableStr(e.Model), nullableStr(e.ClaudeTaskID),
		e.Source, nullableStr(e.StepID),
		e.CreatedAt.UTC().Format(time.RFC3339),
		e.UpdatedAt.UTC().Format(time.RFC3339),
	)
}

// routeFamilyAttribution is the daemon-routed analogue of
// db.PropagateFamilyAttribution. It performs the read-modify-write reconcile with
// reads against the read handle and routes each recipient UPDATE through the
// writer daemon (enqueue-only) instead of writing directly. It returns the number
// of recipients it ROUTED an update for (the would-update count) so the caller can
// gate best-effort HTML persistence; because enqueue-only acks carry no
// RowsAffected, this is not guaranteed to equal the applied-row count, but the
// reconcile and HTML persist are both idempotent.
func routeFamilyAttribution(projectDir string, database *sql.DB, familyID string) int {
	if database == nil || strings.TrimSpace(familyID) == "" {
		return 0
	}
	members, err := db.GetSessionsByFamily(database, familyID)
	if err != nil {
		debugLog(projectDir, "[session-start] propagate family attribution (members): %v", err)
		return 0
	}
	if len(members) < 2 {
		return 0 // a family of one has no sibling to donate to or from
	}
	donor := ""
	for _, sid := range members {
		if wi := db.GetActiveWorkItemWithFallback(database, sid, db.AgentRootSentinel); wi != "" {
			donor = wi
			break
		}
	}
	if donor == "" {
		return 0
	}
	now := time.Now().UTC().Format(time.RFC3339)
	routed := 0
	for _, sid := range members {
		if db.GetActiveWorkItemWithFallback(database, sid, db.AgentRootSentinel) != "" {
			continue // already attributed — never clobber
		}
		// Same TOCTOU-guarded predicate as db.PropagateFamilyAttribution: only
		// write when the recipient is still unattributed in BOTH stores. The
		// daemon Execs this as one parameterized statement; the guard runs at
		// apply time so a claim landing between our read and the apply is honored.
		RouteHookWrite("session-start", projectDir, sid,
			`UPDATE sessions SET active_feature_id = ?, updated_at = ?
			 WHERE session_id = ?
			   AND (active_feature_id IS NULL OR active_feature_id = '')
			   AND NOT EXISTS (
			       SELECT 1 FROM active_work_items awi
			       WHERE awi.session_id = sessions.session_id AND awi.agent_id = ?
			   )`,
			donor, now, sid, db.AgentRootSentinel,
		)
		routed++
	}
	return routed
}

func persistFamilyAttributionHTML(projectDir string, database *sql.DB, familyID string) {
	members, err := db.GetSessionsByFamily(database, familyID)
	if err != nil {
		return
	}
	for _, sid := range members {
		featureID := db.GetActiveWorkItemWithFallback(database, sid, db.AgentRootSentinel)
		if featureID == "" {
			continue
		}
		if err := SetSessionHTMLActiveFeature(projectDir, sid, featureID); err != nil {
			debugLog(projectDir, "[session-start] persist family attribution html %s: %v", sid, err)
		}
	}
}

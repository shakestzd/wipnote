package hooks

import (
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/provenance"
	"github.com/shakestzd/wipnote/core/worktree"
)

// bareLaunchNudge returns an extra startup hint when Claude was started without
// `wipnote claude` (i.e. .launch-mode is missing or older than 30 seconds).
// Returns an empty string when the orchestrator system prompt is already active.
func bareLaunchNudge(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	path := filepath.Join(projectDir, ".wipnote", ".launch-mode")
	info, err := os.Stat(path)
	if err == nil {
		// File exists — check if it was written within the freshness window
		// (shared with resolveOwningPID so both agree on "this session").
		if time.Since(info.ModTime()) <= launchMarkerFreshWindow {
			return ""
		}
	}
	return "Start sessions with `wipnote claude` for automatic orchestrator mode."
}

// SessionStart handles the SessionStart Claude Code hook event.
// It upserts a session row in SQLite and writes environment variables for
// downstream hooks via CLAUDE_ENV_FILE.
func SessionStart(event *CloudEvent, database *sql.DB, projectDir string) (*HookResult, error) {
	handlerStart := time.Now()

	// Install .wipnote/.gitignore on every session start (harness-neutral, idempotent).
	// This ensures adopter projects receive the runtime-artifact gitignore policy on
	// their first launch after wipnote is installed or upgraded, without requiring any
	// manual step. The call is a no-op when the file already exists.
	worktree.EnsureWipnoteGitignore(projectDir)

	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	now := time.Now().UTC()
	shortID := sessionID[:minSessionLen(sessionID)]

	startCommit := bootstrapSessionStart(event, projectDir, sessionID)
	s := newStartedSession(event, projectDir, sessionID, now, startCommit)
	persistStartedSession(event, database, projectDir, sessionID, shortID, s)

	LogTimed(projectDir, "session-start", map[string]string{
		"session": shortID,
	}, handlerStart, "handler complete")

	recordSessionStartEvent(event, projectDir, sessionID, s, now)
	wireSessionFamily(projectDir, database, sessionID, shortID, s.AgentAssigned)
	return sessionStartResult(projectDir), nil
}

// bootstrapSessionStart repairs the worktree gitdir, starts the background
// sweeps, propagates the session ID to downstream hooks and returns the HEAD
// commit the session starts at.
func bootstrapSessionStart(event *CloudEvent, projectDir, sessionID string) string {
	// Repair stale worktree .git gitdir pointer before any git operations.
	// When a worktree is created on one machine (e.g. macOS) and opened on
	// another (e.g. Linux devcontainer), the absolute path in the .git file
	// becomes stale. Repair it now so all downstream git commands succeed.
	if event.CWD != "" && event.CWD != projectDir {
		if err := worktree.RepairGitdirFromRepoRoot(event.CWD, projectDir); err != nil {
			debugLog(projectDir, "[session-start] worktree gitdir repair failed (cwd=%s): %v", event.CWD, err)
		}
	}

	// Launch headCommit in a goroutine — I/O-bound, no data dependency with writeEnvVars.
	commitCh := make(chan string, 1)
	go func() {
		commitCh <- headCommit(projectDir)
	}()

	// Fire-and-forget disk-retention sweep: finalize telemetry for sessions
	// that never got a clean SessionEnd (bounded catch-up), rotate oversized
	// logs, and archive+prune raw events.ndjson for OLD, inactive,
	// fully-ingested sessions. The writer daemon runs the same sweep every
	// 24h; this is the session-start cleanup point. The current session is
	// passed as active so its own ndjson is never touched; the sweep is
	// idempotent and fail-safe (no-op on any error). Backgrounded so it never
	// blocks the hot hook path.
	if RetentionSweepFn != nil {
		go RetentionSweepFn(projectDir, sessionID)
	}

	// Propagate session ID to downstream hooks while git is running. The
	// .active-session entry is tagged with THIS event's harness so a later
	// reader under another harness never adopts it (issue #148).
	writeEnvVars(sessionID, projectDir, activeSessionHarness(event))

	// Emit the Rosetta correlation event: maps launcher-minted OTel session ID
	// to Claude Code's own session_id so the dashboard can follow --resume flows.
	// WIPNOTE_OTEL_SESSION_ID carries the OTel collector's 28-char hex ID minted
	// by the launcher; WIPNOTE_SESSION_ID is now reserved for the real Claude Code
	// session identity (set by writeEnvVars after this point). See bug-b262d303.
	emitRosettaEvent(projectDir, os.Getenv("WIPNOTE_OTEL_SESSION_ID"), event.SessionID)

	// Wait for git result — upsertSession needs the commit hash.
	return <-commitCh
}

// newStartedSession builds the session row for this SessionStart, including
// its model and creation provenance.
func newStartedSession(event *CloudEvent, projectDir, sessionID string, now time.Time, startCommit string) *models.Session {
	s := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: resolveEventAgentID(event),
		Status:        "active",
		CreatedAt:     now,
		StartCommit:   startCommit,
		IsSubagent:    isSubagentEvent(event) || isSubagent(),
		Model:         os.Getenv("CLAUDE_MODEL"),
		// TODO(bug-cb4918d8): remove after lineage wiring verified end-to-end.
		// These env vars are NEVER set in subagent hook contexts (confirmed via
		// /tmp/wipnote-hook-trace.jsonl); lineage now flows through the
		// subagent-start hook writing sessions+agent_lineage_trace directly.
		ParentSessionID: os.Getenv("WIPNOTE_PARENT_SESSION"),
		ParentEventID:   os.Getenv("WIPNOTE_PARENT_EVENT"),
		GitRemoteURL:    paths.GetGitRemoteURL(projectDir),
		// Normalize to repo-relative so session records remain stable across
		// worktrees and machines. Local sessions get a relative path (e.g. ".");
		// sessions ingested from foreign machines (where the canonical root
		// differs from the local repo) are stored with an "unresolved:" prefix
		// so they are queryable without silently mangling the original path.
		ProjectDir:       paths.NormalizeProjectDir(projectDir),
		ExecWorktreePath: execWorktreeRelPath(event.CWD, projectDir),
		Branch:           gitBranch(execDirOrDefault(event.CWD, projectDir)),
		Harness:          resolveHarness(),
		ContinuedFrom:    strings.TrimSpace(os.Getenv("WIPNOTE_CONTINUED_FROM")),
	}

	// Prefer CloudEvent fields over env vars (more reliable).
	if event.Model != "" {
		s.Model = event.Model
	}

	// Provenance — capture which harness/model/role/CLI started this session
	// so downstream consumers can attribute it across handoffs (feat-40ef1333).
	prov := provenance.Detect()
	if prov.Agent == "" {
		prov.Agent = s.AgentAssigned
	}
	if event.Model != "" {
		prov.Model = event.Model
	}
	s.CreatedByAgent = prov.Agent
	s.CreatedByModel = prov.Model
	s.CreatedByRole = prov.Role
	s.CreatedByCLIVersion = prov.CLIVersion
	return s
}

// persistStartedSession persists the session row and its lineage, the liveness
// anchor, the canonical session HTML and ledger entry, finishes sessions whose
// end hook was cancelled, and runs the bounded orphan sweep.
func persistStartedSession(event *CloudEvent, database *sql.DB, projectDir, sessionID, shortID string, s *models.Session) {
	routeStartedSession(event, database, projectDir, sessionID, shortID, s)

	// Record the session process-liveness anchor (feat-0a7db952, slice-1).
	// Writing the owning pid (+ /proc start-time) lets the reaper distinguish a
	// crashed session from a long-idle LIVE one. Best-effort: a write failure
	// must NEVER block or fail the hook — when the owner is unresolvable we omit
	// .session-pid entirely so the session safe-degrades to LIVE (never reaped).
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	updateSessionPIDAnchor(projectDir, sessDir)

	// Finish sessions whose SessionEnd hook was cancelled after it wrote its
	// end note: close their ledger rows and claim episodes from the note.
	// Sessions that never reported an end are left alone (feat-174dc958).
	_ = CatchUpEndedSessions(database, projectDir, sessionID, false)

	// Write canonical session HTML file (non-critical, errors silently logged).
	CreateSessionHTML(projectDir, s)

	// Record the session in the canonical, GIT-TRACKED sessions ledger. The HTML
	// written just above lives under .wipnote/sessions/, which is gitignored
	// runtime telemetry and is pruned on a retention timer — so it cannot be
	// what an implemented_in edge resolves against. The ledger can.
	recordSessionLedgerOpen(projectDir, s)

	// Record tool calls that never completed in recently active sessions as
	// aborted, from the canonical tool log (feat-174dc958). Bounded: at most
	// SessionStartSweepCap per launch; the rest wait for the next one.
	if n := SweepOrphanedToolCalls(projectDir, sessionID, time.Now().UTC(), SessionStartSweepCap); n > 0 {
		debugLog(projectDir, "[session-start] recorded %d orphaned tool call(s) as aborted", n)
	}
}

// routeStartedSession resolves the parent lineage and routes the session row
// and (for subagents) its lineage traces.
func routeStartedSession(event *CloudEvent, database *sql.DB, projectDir, sessionID, shortID string, s *models.Session) {
	// Resolve lineage inputs before opening the transaction (read-only queries).
	var inp *lineageInputs
	if s.IsSubagent && s.ParentSessionID != "" {
		featureID := GetActiveFeatureID(database, s.SessionID)
		inp = resolveParentLineage(event, database, s.ParentSessionID, featureID)
	}

	// Persist the session row and (for subagents) its lineage traces through the
	// writer daemon (enqueue-only) instead of a direct writable transaction.
	//
	// TX-atomicity note (plan-2390966a slice-3): the old code wrapped the session
	// upsert + lineage inserts in ONE SQLite transaction. The daemon apply loop
	// does NOT auto-wrap a transaction, and a multi-statement batch cannot be a
	// single OpTypeSQL op, so we route each statement as its OWN enqueue-only op.
	// Atomicity-of-effect is preserved differently: every op lands on the SINGLE
	// writer in FIFO order, so the session insert always applies before the
	// lineage traces that reference it — a reader never sees a lineage row whose
	// session is missing. The all-or-nothing guarantee of a SQL transaction is
	// relaxed to "FIFO-ordered single-writer + canonical NDJSON/reindex backstop",
	// which is the established hot-hook contract (RouteHookWrite, slice-2).
	txStart := time.Now()
	routeSessionUpsert(projectDir, sessionID, s)
	if inp != nil {
		routeLineageTraces(projectDir, sessionID, inp)
	}
	LogTimed(projectDir, "session-start", map[string]string{
		"phase":   "db-tx",
		"session": shortID,
	}, txStart, "session+lineage routed")
}

// recordSessionStartEvent stores the transcript path and persists the
// session-start event to agent_events.
func recordSessionStartEvent(event *CloudEvent, projectDir, sessionID string, s *models.Session, now time.Time) {
	// Store transcript path if provided by CloudEvent. Routed enqueue-only so a
	// held write lock never stalls the post-selection critical path.
	if event.TranscriptPath != "" {
		RouteHookWrite("session-start", projectDir, sessionID,
			`UPDATE sessions SET transcript_path = ? WHERE session_id = ?`,
			event.TranscriptPath, sessionID)
	}

	// Persist the session-start event to agent_events for dashboard activity feed.
	// Routed enqueue-only through the daemon (no direct writable Exec). The event
	// has no ParentEventID, so db.InsertEvent's parent-agent lookup is a no-op and
	// the plain parameterized INSERT below is equivalent.
	ev := &models.AgentEvent{
		EventID:      uuid.New().String(),
		AgentID:      s.AgentAssigned,
		EventType:    models.EventStart,
		Timestamp:    now,
		ToolName:     "SessionStart",
		InputSummary: "Session started",
		SessionID:    sessionID,
		Status:       "recorded",
		Source:       "hook",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	routeInsertEvent(projectDir, sessionID, ev)
}

// wireSessionFamily implements session-family continuity (slice-4,
// feat-a225ce7c): it wires the harness-neutral session_family_id for Claude, Codex, and Gemini.
// Launchers inject WIPNOTE_SESSION_FAMILY_ID; the hook is the authoritative
// DB write path for all harnesses. Per-session state + the family index are
// also written here as a durability layer (they survive hook restarts).
func wireSessionFamily(projectDir string, database *sql.DB, sessionID, shortID, agentID string) {
	familyID := os.Getenv("WIPNOTE_SESSION_FAMILY_ID")
	if familyID == "" {
		// No family env — treat this session as its own family.
		familyID = sessionID
	}
	setSessionFamilyApplied(projectDir, sessionID, familyID)
	// File writes: family index + per-session state (harness-neutral).
	_ = agent.RegisterSessionFamily(projectDir, sessionID, familyID)
	_ = agent.WriteSessionState(projectDir, sessionID, agentID, familyID)

	// Forward-propagate work-item attribution across the family. Claude Code
	// splits a session: this hook fires on the short parent stub (which holds
	// active_feature_id), but the real long-running child session IDs never get
	// a SessionStart the hook observes. Reconciling here on every launch copies
	// the family's work item onto any sibling that still lacks one, so the
	// chooser, dashboard, and analytics all attribute the split children.
	//
	// The reconcile reads members + donor directly (reads stay direct) and
	// routes each recipient UPDATE through the daemon enqueue-only path. We use
	// the count of routed recipients to gate the (best-effort, idempotent) HTML
	// persistence; enqueue-only acks carry no RowsAffected, so this is the
	// would-update count rather than the applied count.
	if n := routeFamilyAttribution(projectDir, database, familyID); n > 0 {
		persistFamilyAttributionHTML(projectDir, database, familyID)
		debugLog(projectDir, "[session-start] propagated work item to %d family sibling(s) of %s", n, shortID)
	}
}

// setSessionFamilyApplied sets session_family_id on this session row with an
// applied acknowledgement from the writer daemon (apply.RouteSQL, bounded by
// CLISubmitBudget). On a daemon miss the write is dropped (feat-174dc958).
func setSessionFamilyApplied(projectDir, sessionID, familyID string) {
	const setFamilySQL = `UPDATE sessions SET session_family_id = ? WHERE session_id = ?`
	if !routeSQLApplied(projectDir, setFamilySQL, familyID, sessionID) {
		RecordFallback("session-start", sessionID, FallbackWriterUnavailable, "daemon miss: write dropped")
	}
}

// sessionStartResult builds the SessionStart additional context: reconcile
// warnings, continue handoff, and either the version-mismatch warning or the
// startup attribution guidance.
func sessionStartResult(projectDir string) *HookResult {
	// Surface (and consume) any durable reconcile warnings persisted by a
	// prior Gemini/Codex session exit (slice-5, feat-f93fe770). This is the
	// non-blocking counterpart to the Claude exit-2 path: the user-never-
	// returns case is recorded at session exit and rendered here on return.
	reconcilePrefix := DrainReconcileWarnings(projectDir)
	continuePrefix := continueHandoffContextFromEnv()

	// Warn the user when the CLI and plugin versions have drifted.
	warning := versionMismatchWarning()
	if warning != "" {
		debugLog(projectDir, "[session-start] version mismatch detected: %s", warning)
		return &HookResult{AdditionalContext: joinSessionStartContext(continuePrefix, reconcilePrefix, warning)}
	}

	// Emit concise startup guidance at session start. Open-item rosters stay
	// on-demand via the CLI to keep startup context quiet.
	attribution := buildSessionStartAttribution()
	if nudge := bareLaunchNudge(projectDir); nudge != "" {
		attribution = joinSessionStartContext(attribution, nudge)
	}
	return &HookResult{AdditionalContext: joinSessionStartContext(continuePrefix, reconcilePrefix, attribution)}
}

func continueHandoffContextFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("WIPNOTE_CONTINUE_HANDOFF_B64"))
	if raw == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(decoded))
}

func joinSessionStartContext(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return strings.Join(filtered, "\n\n")
}

// buildSessionStartAttribution returns concise session-start guidance. It keeps
// startup context quiet by directing agents to fetch work context on demand
// rather than eagerly listing open items and titles.
func buildSessionStartAttribution() string {
	lines := []string{
		"wipnote plugin is active in this project.",
		"",
		"Retrieve context on demand:",
		"- `wipnote wip` for open and in-progress items",
		"- `wipnote relevant <topic>` before creating new work so you search open and completed lineage first",
		"- `wipnote help --compact` for a command overview",
	}
	return joinLines(lines)
}

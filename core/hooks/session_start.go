package hooks

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
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
// It records the session in canonical files and writes environment variables
// for downstream hooks via CLAUDE_ENV_FILE.
func SessionStart(event *CloudEvent, projectDir string) (*HookResult, error) {
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

	s := newStartedSession(event, projectDir, sessionID, now, "")
	s.StartCommit = bootstrapSessionStart(event, projectDir, s)
	persistStartedSession(projectDir, sessionID, s)

	LogTimed(projectDir, "session-start", map[string]string{
		"session": shortID,
	}, handlerStart, "handler complete")

	wireSessionFamily(projectDir, sessionID, s.AgentAssigned)
	return sessionStartResult(projectDir), nil
}

// bootstrapSessionStart repairs the worktree gitdir, starts the background
// sweeps, records the session's ledger row, propagates the session ID to
// downstream hooks and returns the HEAD commit the session starts at.
func bootstrapSessionStart(event *CloudEvent, projectDir string, s *models.Session) string {
	sessionID := s.SessionID
	repairWorktreeGitdir(event, projectDir)

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

	// Record the session in the canonical, GIT-TRACKED sessions ledger BEFORE
	// .active-session names it: from that moment any wipnote CLI command
	// resolves this session id and may write its row with a weaker guess of
	// the harness (bug-5333ea49). Writing first closes that window; Open also
	// corrects a guessed harness, so the order is defence, not the guarantee.
	// The session HTML lives under .wipnote/sessions/, gitignored and pruned on
	// a retention timer, so it cannot be what an implemented_in edge resolves
	// against. The ledger can.
	recordSessionLedgerOpen(projectDir, s)

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

// repairWorktreeGitdir repairs a stale worktree .git gitdir pointer before any
// git operations. When a worktree is created on one machine (e.g. macOS) and
// opened on another (e.g. Linux devcontainer), the absolute path in the .git
// file becomes stale. Repair it now so all downstream git commands succeed.
func repairWorktreeGitdir(event *CloudEvent, projectDir string) {
	if event.CWD == "" || event.CWD == projectDir {
		return
	}
	if err := worktree.RepairGitdirFromRepoRoot(event.CWD, projectDir); err != nil {
		debugLog(projectDir, "[session-start] worktree gitdir repair failed (cwd=%s): %v", event.CWD, err)
	}
}

// newStartedSession builds the session record SessionStart persists: the
// fields the canonical session HTML and the sessions ledger carry, and no
// others (feat-174dc958).
func newStartedSession(event *CloudEvent, projectDir, sessionID string, now time.Time, startCommit string) *models.Session {
	return &models.Session{
		SessionID:     sessionID,
		AgentAssigned: resolveEventAgentID(event),
		Status:        "active",
		CreatedAt:     now,
		StartCommit:   startCommit,
		IsSubagent:    isSubagentEvent(event) || isSubagent(),
		// Repo-relative, so canonical records stay stable across worktrees and
		// machines; a foreign-machine path gets an "unresolved:" prefix.
		ProjectDir: paths.NormalizeProjectDir(projectDir),
		Harness:    resolveHarness(),
		// Lineage only: at depth 0 an inherited parent does not make this a
		// subagent, but which session it came from is still recorded.
		ParentSessionID: inheritedParentSession(sessionID),
	}
}

// persistStartedSession records the liveness anchor and the canonical session HTML, finishes sessions whose
// end hook was cancelled, and runs the bounded orphan sweep.
func persistStartedSession(projectDir, sessionID string, s *models.Session) {
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
	_ = CatchUpEndedSessions(projectDir, sessionID, false)

	// Write canonical session HTML file (non-critical, errors silently logged).
	CreateSessionHTML(projectDir, s)

	// Record tool calls that never completed in recently active sessions as
	// aborted, from the canonical tool log (feat-174dc958). Bounded: at most
	// SessionStartSweepCap per launch; the rest wait for the next one.
	if n := SweepOrphanedToolCalls(projectDir, sessionID, time.Now().UTC(), SessionStartSweepCap); n > 0 {
		debugLog(projectDir, "[session-start] recorded %d orphaned tool call(s) as aborted", n)
	}
}

// wireSessionFamily records session-family continuity (slice-4,
// feat-a225ce7c) in the canonical files: the family index and the
// per-session state. Launchers inject WIPNOTE_SESSION_FAMILY_ID; without it a
// session is its own family.
func wireSessionFamily(projectDir, sessionID, agentID string) {
	familyID := os.Getenv("WIPNOTE_SESSION_FAMILY_ID")
	if familyID == "" {
		familyID = sessionID
	}
	_ = agent.RegisterSessionFamily(projectDir, sessionID, familyID)
	_ = agent.WriteSessionState(projectDir, sessionID, agentID, familyID)
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

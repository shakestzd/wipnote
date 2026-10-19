package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/shakestzd/wipnote/core/agent"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/internal/launcher"
)

type chooserEligibility struct {
	TTY              bool
	CI               bool
	ResumeID         string
	WorkItem         string
	Targeted         bool
	InPlace          bool
	ExplicitContinue bool
	Yolo             bool
	ExtraArgs        []string
}

type claudeIntentResult struct {
	mode     string
	resumeID string
	workItem string
	intent   launcher.LaunchIntent
	// browse opens Claude's own session picker: `claude --resume` with no id.
	browse bool
}

func shouldOfferLaunchIntentChooser(opts chooserEligibility) bool {
	if !opts.TTY || opts.CI {
		return false
	}
	if opts.ResumeID != "" || opts.WorkItem != "" || opts.Targeted || opts.InPlace || opts.ExplicitContinue || opts.Yolo {
		return false
	}
	return len(opts.ExtraArgs) == 0
}

func isInteractiveTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

var chooseLaunchIntentFn = chooseLaunchIntent

func chooseLaunchIntent(projectRoot, canonicalRoot, harness string, in io.Reader, out io.Writer) (launcher.LaunchIntent, error) {
	grouped, err := listGroupedResumableSessionsForRoot(projectRoot, canonicalRoot, harness)
	if err != nil {
		return launcher.NewWorkIntent(), err
	}
	// Skip the chooser only when there is genuinely nothing to resume. The
	// current-session slot counts: it is the whole point of this path for a
	// split-child session with no (or a completed) work item, which never
	// appears in the same/cross-harness groups.
	// A harness with a session browser always has something to offer: the
	// browser can find sessions the shortlist could not read.
	if !hasResumableOptions(grouped) && !offersSessionBrowser(harness) {
		return launcher.NewWorkIntent(), nil
	}
	return promptLaunchIntent(in, out, harness, grouped)
}

func resolveLaunchIntentForDefaultLaunch(projectRoot, canonicalRoot, harness string, opts chooserEligibility, in io.Reader, out io.Writer) (launcher.LaunchIntent, error) {
	if !shouldOfferLaunchIntentChooser(opts) {
		return launcher.NewWorkIntent(), nil
	}
	return chooseLaunchIntentFn(projectRoot, canonicalRoot, harness, in, out)
}

func listGroupedResumableSessionsForRoot(projectRoot, canonicalRoot, harness string) (dbpkg.HarnessGroupedResumableSessions, error) {
	root := canonicalRoot
	if root == "" {
		root = projectRoot
	}
	if root == "" {
		return dbpkg.HarnessGroupedResumableSessions{}, nil
	}
	wipnoteDir := root + string(os.PathSeparator) + ".wipnote"
	db, err := openReadOnlyDB(wipnoteDir)
	if err != nil {
		return dbpkg.HarnessGroupedResumableSessions{}, err
	}
	defer db.Close()
	threshold := dbpkg.LivenessStalenessThreshold(root)
	grouped, err := dbpkg.ListHarnessGroupedResumableSessions(db, threshold, harness)
	if err != nil {
		return grouped, err
	}
	// Surface the session the user is launching from as a first-class slot. This
	// bypasses the work_item_id <> '' gate the grouped listing applies, which
	// otherwise hides session-split children that never got work-item attribution.
	// Without one, Claude's slot is the project's newest session read from Claude's
	// own transcripts (what /resume lists), so it carries the prompt the user
	// typed. Other harnesses keep the newest open ledger session. Degrades cleanly:
	// on any error the grouped listing is returned without the slot.
	if current, cerr := dbpkg.GetCurrentSessionResumable(db, threshold, resolveCurrentSessionIDs(root)); cerr == nil && current != nil && isActionableCurrentSession(*current, harness) {
		grouped = withCurrentSession(grouped, current)
	} else if strings.EqualFold(harness, harnessClaude) {
		if last := latestClaudeTranscriptFn(root); last != nil {
			grouped = withCurrentSession(grouped, transcriptResumable(*last, root))
			grouped.CurrentIsLast = true
		}
	} else if current, cerr := dbpkg.GetLatestHarnessSessionResumable(db, threshold, harness); cerr == nil && current != nil && isActionableCurrentSession(*current, harness) && recentlyActive(*current, time.Now()) {
		grouped = withCurrentSession(grouped, current)
		grouped.CurrentIsLast = true
	}
	return shortlistWorkItemRows(grouped, time.Now()), nil
}

// recentlyActive reports whether a row was active within workItemRowMaxAge. A
// ledger session that was never closed stays "active" for good, so the
// fallback slot needs this age limit to avoid offering a months-old session.
func recentlyActive(row dbpkg.ResumableSession, now time.Time) bool {
	at, ok := parseActivityTime(row.LastActivity)
	return ok && now.Sub(at) <= workItemRowMaxAge
}

// latestClaudeTranscriptFn is the seam for tests: it returns the project's newest
// Claude session from Claude's transcript folder.
var latestClaudeTranscriptFn = func(root string) *claudeTranscriptSession {
	return latestClaudeTranscriptSession(claudeProjectsDir(), root)
}

// transcriptLiveWindow treats a transcript written this recently as a session
// that may still be open in another terminal.
const transcriptLiveWindow = 2 * time.Minute

// transcriptResumable turns a transcript session into the picker's row shape.
// A session that ran in a worktree keeps that directory: `claude --resume <id>`
// finds a transcript only from the directory the session ran in.
func transcriptResumable(t claudeTranscriptSession, root string) *dbpkg.ResumableSession {
	row := &dbpkg.ResumableSession{
		Harness:       harnessClaude,
		LastSessionID: t.ID,
		PromptLabel:   t.Label,
		LastActivity:  t.ModTime.UTC().Format(time.RFC3339),
		Live:          time.Since(t.ModTime) < transcriptLiveWindow,
	}
	if cwd := strings.TrimSpace(t.Cwd); cwd != "" && filepath.Clean(cwd) != filepath.Clean(root) {
		row.ExecWorktreePath = cwd
	}
	return row
}

// The picker offers at most maxWorkItemRows work items, each in progress and
// active within workItemRowMaxAge. Everything else is one step away through
// "Browse all sessions" (Claude's /resume picker) or `wipnote feature start`.
const (
	maxWorkItemRows   = 3
	workItemRowMaxAge = 7 * 24 * time.Hour
)

// shortlistWorkItemRows keeps the in-progress, recently active work items, the
// newest maxWorkItemRows across both harness groups, each group keeping its
// order.
func shortlistWorkItemRows(grouped dbpkg.HarnessGroupedResumableSessions, now time.Time) dbpkg.HarnessGroupedResumableSessions {
	type pick struct {
		at    time.Time
		same  bool
		index int
	}
	var picks []pick
	consider := func(rows []dbpkg.ResumableSession, same bool) {
		for i, r := range rows {
			if !strings.EqualFold(strings.TrimSpace(r.Status), "in-progress") {
				continue
			}
			at, ok := parseActivityTime(r.LastActivity)
			if !ok || now.Sub(at) > workItemRowMaxAge {
				continue
			}
			picks = append(picks, pick{at: at, same: same, index: i})
		}
	}
	consider(grouped.SameHarness, true)
	consider(grouped.CrossHarness, false)
	sort.SliceStable(picks, func(a, b int) bool { return picks[a].at.After(picks[b].at) })
	if len(picks) > maxWorkItemRows {
		picks = picks[:maxWorkItemRows]
	}
	keepSame, keepCross := map[int]bool{}, map[int]bool{}
	for _, p := range picks {
		if p.same {
			keepSame[p.index] = true
		} else {
			keepCross[p.index] = true
		}
	}
	filter := func(rows []dbpkg.ResumableSession, keep map[int]bool) []dbpkg.ResumableSession {
		var out []dbpkg.ResumableSession
		for i, r := range rows {
			if keep[i] {
				out = append(out, r)
			}
		}
		return out
	}
	grouped.SameHarness = filter(grouped.SameHarness, keepSame)
	grouped.CrossHarness = filter(grouped.CrossHarness, keepCross)
	return grouped
}

// harnessesWithNativeSessionResume lists the harnesses that can resume a prior
// session directly from its stored wipnote session ID (e.g. `--resume <id>`).
// Gemini resumes by a numeric index and Antigravity has no session-ID resume, so
// a resume-ID-only current slot is not actionable for them — it would bail in the
// continuation-context path and launch fresh despite offering "Resume this
// session".
var harnessesWithNativeSessionResume = map[string]struct{}{
	"claude": {},
	"codex":  {},
}

// isActionableCurrentSession reports whether the current-session slot would
// resolve to a meaningful launch intent for the target harness.
//
//   - Same harness, resume-ID-only: actionable only when the harness resumes
//     natively by session ID (claude/codex). Otherwise the resume ID is unusable
//     and, with no work item, the continuation context bails — so require a work
//     item for Gemini/Antigravity.
//   - Cross harness: never actionable for the special current-session slot.
//     Cross-harness continuation is represented by the grouped "Continue from"
//     work-item rows; the current-session slot is reserved for resuming the
//     target harness's own runtime session.
func isActionableCurrentSession(row dbpkg.ResumableSession, harness string) bool {
	sameHarness := strings.EqualFold(strings.TrimSpace(row.Harness), strings.TrimSpace(harness))
	if !sameHarness {
		return false
	}
	if strings.TrimSpace(row.WorkItemID) != "" {
		return true
	}
	// No work item: only a same-harness, natively-resumable session is actionable.
	if strings.TrimSpace(row.LastSessionID) == "" {
		return false
	}
	_, ok := harnessesWithNativeSessionResume[strings.ToLower(strings.TrimSpace(harness))]
	return ok
}

// resolveCurrentSessionIDs returns the candidate session IDs for "the session the
// user is launching from": the harness/launcher session env vars plus every
// member of that session's family. Including the family expands the short parent
// stub to the long-running split-child IDs, so whichever one carries the live
// transcript can be surfaced. Returns nil when nothing is resolvable.
func resolveCurrentSessionIDs(projectRoot string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(s string) bool {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			ids = append(ids, s)
			return true
		}
		return false
	}
	nativeIDFound := false

	// Prefer the native session/thread ID for the harness we are currently
	// running inside. WIPNOTE_SESSION_ID can be inherited from a parent harness
	// during nested launches (for example Claude -> `wipnote codex --dev`), so
	// it is a fallback, not the primary identity, when a launcher stamps a
	// more specific harness marker.
	//
	// agent.HarnessNativeEnvSessionID() is the canonical shared implementation
	// (WIPNOTE_HARNESS=codex→CODEX_THREAD_ID, then CODEX_SESSION_ID; gemini→GEMINI_SESSION_ID,
	// antigravity→ANTIGRAVITY_SESSION_ID). Chooser-specific secondary IDs
	// (WIPNOTE_OTEL_SESSION, CLAUDE_CODE_SESSION_ID, CLAUDE_SESSION_ID) are
	// handled in the residual switch below.
	currentHarness := strings.ToLower(strings.TrimSpace(os.Getenv(harnessEnvKey)))
	if primaryID := agent.HarnessNativeEnvSessionID(); primaryID != "" {
		nativeIDFound = add(primaryID) || nativeIDFound
	}
	switch currentHarness {
	case harnessCodex, harnessGemini, harnessAntigravity:
		nativeIDFound = add(os.Getenv("WIPNOTE_OTEL_SESSION")) || nativeIDFound
	case harnessClaude:
		nativeIDFound = add(os.Getenv("CLAUDE_CODE_SESSION_ID")) || nativeIDFound
		nativeIDFound = add(os.Getenv("CLAUDE_SESSION_ID")) || nativeIDFound
	}

	for _, env := range []string{
		"WIPNOTE_SESSION_ID",
		"CLAUDE_CODE_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"CODEX_THREAD_ID",
		"CODEX_SESSION_ID",
		"GEMINI_SESSION_ID",
		"ANTIGRAVITY_SESSION_ID",
		"WIPNOTE_OTEL_SESSION",
		"WIPNOTE_PARENT_SESSION",
	} {
		if nativeIDFound && env == "WIPNOTE_SESSION_ID" && currentHarness != "" {
			continue
		}
		// CODEX_SESSION_ID is the root thread. It is a fallback only: with a
		// thread id present it would offer the parent session for resume,
		// because the resumable lookup orders candidates by activity, not by
		// this list's precedence.
		if env == "CODEX_SESSION_ID" && strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" {
			continue
		}
		add(os.Getenv(env))
	}

	famID := strings.TrimSpace(os.Getenv("WIPNOTE_SESSION_FAMILY_ID"))
	if famID == "" {
		for _, id := range ids {
			if f := agent.SessionFamilyFor(projectRoot, id); f != "" {
				famID = f
				break
			}
		}
	}
	if famID != "" {
		add(famID)
		if idx, err := agent.ReadSessionFamilyIndex(projectRoot); err == nil {
			for sid, fid := range idx {
				if fid == famID {
					add(sid)
				}
			}
		}
	}
	return ids
}

// withCurrentSession sets the current-session slot and removes any duplicate of
// it from the same/cross-harness groups so it is never listed twice.
func withCurrentSession(grouped dbpkg.HarnessGroupedResumableSessions, current *dbpkg.ResumableSession) dbpkg.HarnessGroupedResumableSessions {
	grouped.Current = current
	grouped.SameHarness = dropSessionByID(grouped.SameHarness, current.LastSessionID)
	grouped.CrossHarness = dropSessionByID(grouped.CrossHarness, current.LastSessionID)
	return grouped
}

// dropSessionByID returns rows with any entry whose LastSessionID equals
// sessionID removed. Returns rows unchanged when sessionID is empty.
func dropSessionByID(rows []dbpkg.ResumableSession, sessionID string) []dbpkg.ResumableSession {
	if sessionID == "" {
		return rows
	}
	out := make([]dbpkg.ResumableSession, 0, len(rows))
	for _, r := range rows {
		if r.LastSessionID == sessionID {
			continue
		}
		out = append(out, r)
	}
	return out
}

// runSelectTUIFn is the seam for tests: replace it to drive selection without a live TTY.
// It receives the writer, harness name, and pre-built options; returns the chosen int index.
var runSelectTUIFn = runSelectTUI

// runSelectTUI runs a huh Select form and returns the chosen option index.
// Returns an error when the TUI cannot run (non-TTY writer, accessible mode fail, etc.).
func runSelectTUI(in io.Reader, out io.Writer, harness string, opts []huh.Option[int]) (int, error) {
	selected := 0
	sel := huh.NewSelect[int]().
		Title(chooserTitle(harness)).
		Options(opts...).
		Value(&selected)

	accessible := os.Getenv("ACCESSIBLE") != ""
	form := huh.NewForm(huh.NewGroup(sel)).
		WithTheme(launchtui.WipnoteTheme()).
		WithInput(in).
		WithOutput(out).
		WithAccessible(accessible)

	if err := form.Run(); err != nil {
		return 0, err
	}
	return selected, nil
}

func chooserTitle(harness string) string {
	return fmt.Sprintf("Start or continue in %s:", formatHarnessName(harness))
}

// browseSessionsLabel is the last row: it opens the harness's own session picker.
const browseSessionsLabel = "Browse all sessions…"

// harnessesWithSessionBrowser lists the harnesses whose launcher can open the
// native session picker (`claude --resume` with no id).
var harnessesWithSessionBrowser = map[string]struct{}{
	"claude": {},
}

func offersSessionBrowser(harness string) bool {
	_, ok := harnessesWithSessionBrowser[strings.ToLower(strings.TrimSpace(harness))]
	return ok
}

// chooserRowLabels returns the label of every row after "Start something new",
// in index order: the current slot, the work items, then "Browse all sessions"
// when the harness has a session browser.
func chooserRowLabels(harness string, grouped dbpkg.HarnessGroupedResumableSessions) []string {
	var labels []string
	if grouped.Current != nil {
		labels = append(labels, currentSessionActionLabel(grouped)+"  "+describeCurrentSession(*grouped.Current))
	}
	for _, row := range grouped.SameHarness {
		labels = append(labels, "Continue "+describeResumableSession(row, true))
	}
	for _, row := range grouped.CrossHarness {
		labels = append(labels, "Continue "+describeResumableSession(row, false))
	}
	if offersSessionBrowser(harness) {
		labels = append(labels, browseSessionsLabel)
	}
	return labels
}

// buildSelectOptions constructs the huh option list for the chooser.
// Index 0 = NewWork; index i in 1..len(orderedRows) = orderedRows[i-1], where
// orderedRows is [Current?] + SameHarness + CrossHarness (orderedRowsFor); the
// next index, when offered, is "Browse all sessions".
func buildSelectOptions(harness string, grouped dbpkg.HarnessGroupedResumableSessions) []huh.Option[int] {
	st := launchtui.NewStyles()
	opts := []huh.Option[int]{
		huh.NewOption(st.AccentText.Render("Start something new"), 0),
	}
	for i, label := range chooserRowLabels(harness, grouped) {
		opts = append(opts, huh.NewOption(label, i+1))
	}
	return opts
}

// hasResumableOptions reports whether a grouped listing has anything the chooser
// can offer. The current-session slot counts even when both harness groups are
// empty — it lives outside them, so the gate must not be derived from group
// emptiness alone (that would drop the slot in exactly the split-child case).
func hasResumableOptions(grouped dbpkg.HarnessGroupedResumableSessions) bool {
	return grouped.Current != nil || len(grouped.SameHarness) > 0 || len(grouped.CrossHarness) > 0
}

// orderedRowsFor flattens a grouped listing into the index order the chooser
// uses: the current-session slot first (when present), then same-harness, then
// cross-harness. mapIndexToIntent resolves option index i>=1 to orderedRows[i-1].
func orderedRowsFor(grouped dbpkg.HarnessGroupedResumableSessions) []dbpkg.ResumableSession {
	ordered := make([]dbpkg.ResumableSession, 0, len(grouped.SameHarness)+len(grouped.CrossHarness)+1)
	if grouped.Current != nil {
		ordered = append(ordered, *grouped.Current)
	}
	ordered = append(ordered, grouped.SameHarness...)
	ordered = append(ordered, grouped.CrossHarness...)
	return ordered
}

// mapIndexToIntent maps a 0-based option index to a LaunchIntent.
// Index 0 => NewWorkIntent; index i in 1..len(orderedRows) =>
// continueIntentForHarness(orderedRows[i-1]); the next index => browse, when
// the harness offers it.
func mapIndexToIntent(idx int, orderedRows []dbpkg.ResumableSession, harness string) launcher.LaunchIntent {
	if idx == len(orderedRows)+1 && offersSessionBrowser(harness) {
		return launcher.BrowseSessionsIntent()
	}
	if idx <= 0 || idx > len(orderedRows) {
		return launcher.NewWorkIntent()
	}
	return continueIntentForHarness(orderedRows[idx-1], harness)
}

// isTTYWriterFn is the seam for tests: replace it to force the TUI path on a
// non-terminal writer (e.g. bytes.Buffer) without a live char-device.
var isTTYWriterFn = isTTYWriter

// isTTYWriter returns true when w is a *os.File backed by a char-device terminal.
// Used to guard TUI rendering: huh's bubbletea backend hangs on non-TTY writers.
func isTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isInteractiveTerminalFile(f)
}

// promptLaunchIntent shows the chooser and returns the resolved LaunchIntent.
// It tries the huh TUI first when both in and out are real TTYs; if the TUI
// errors (or the writer is not a TTY) it falls through to the numeric reader.
// The upstream non-TTY gate (shouldOfferLaunchIntentChooser + isInteractiveTerminalFile)
// already ensures this function is only reached on interactive char-device stdin.
func promptLaunchIntent(in io.Reader, out io.Writer, harness string, grouped dbpkg.HarnessGroupedResumableSessions) (launcher.LaunchIntent, error) {
	orderedRows := orderedRowsFor(grouped)
	if len(orderedRows) == 0 && !offersSessionBrowser(harness) {
		return launcher.NewWorkIntent(), nil
	}

	// Try the huh TUI only when out is a real terminal (bubbletea hangs otherwise).
	if isTTYWriterFn(out) {
		opts := buildSelectOptions(harness, grouped)
		idx, tuiErr := runSelectTUIFn(in, out, harness, opts)
		if tuiErr == nil {
			return mapIndexToIntent(idx, orderedRows, harness), nil
		}
		// An explicit user abort (Ctrl-C / Esc) cancels the launch: propagate the
		// error so the caller aborts, rather than silently starting new work.
		if errors.Is(tuiErr, huh.ErrUserAborted) {
			return launcher.NewWorkIntent(), tuiErr
		}
		// Other TUI errors (render / TTY failures) fall through to the numeric reader.
	}

	// Numeric text fallback: used for non-TTY out, accessible mode, or any TUI error.
	return promptLaunchIntentNumeric(in, out, harness, grouped, orderedRows)
}

// promptLaunchIntentNumeric is the numbered-menu fallback used when the huh TUI
// cannot render (e.g. non-TTY writer, ACCESSIBLE mode, or any Run error). It
// lists the same rows as the TUI, numbered from 1.
func promptLaunchIntentNumeric(in io.Reader, out io.Writer, harness string, grouped dbpkg.HarnessGroupedResumableSessions, orderedRows []dbpkg.ResumableSession) (launcher.LaunchIntent, error) {
	fmt.Fprintln(out, chooserTitle(harness))
	fmt.Fprintln(out, "  1. Start something new")
	labels := chooserRowLabels(harness, grouped)
	for i, label := range labels {
		fmt.Fprintf(out, "  %d. %s\n", i+2, label)
	}
	last := len(labels) + 1
	fmt.Fprint(out, "Select [1-", last, "] (default 1): ")

	reader := bufio.NewReader(in)
	for attempts := 0; attempts < 3; attempts++ {
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return launcher.NewWorkIntent(), nil
		}
		line = strings.TrimSpace(line)
		if line == "" || line == "1" {
			return launcher.NewWorkIntent(), nil
		}
		n, convErr := strconv.Atoi(line)
		if convErr == nil && n >= 2 && n <= last {
			return mapIndexToIntent(n-1, orderedRows, harness), nil
		}
		if attempts == 2 {
			return launcher.NewWorkIntent(), fmt.Errorf("invalid selection %q", line)
		}
		fmt.Fprint(out, "Enter a number from 1 to ", last, ": ")
	}
	return launcher.NewWorkIntent(), nil
}

func continueIntentForHarness(row dbpkg.ResumableSession, harness string) launcher.LaunchIntent {
	return launcher.ContinueWorkIntent(
		row.WorkItemID,
		row.Harness,
		resumeSessionIDForHarness(row, harness),
		row.ExecWorktreePath,
		true,
	)
}

func resumeSessionIDForHarness(row dbpkg.ResumableSession, harness string) string {
	if strings.EqualFold(strings.TrimSpace(row.Harness), strings.TrimSpace(harness)) {
		return row.LastSessionID
	}
	return ""
}

// maxChooserTitleLen bounds the work-item title shown in a chooser row so long
// titles don't wrap and break the single-line scannability of the list.
const maxChooserTitleLen = 44

// maxChooserPromptLen gives transcript-derived labels more room than work-item
// titles while keeping chooser rows compact.
const maxChooserPromptLen = 76

// shortSessionID returns the leading segment of a session ID (e.g. the first
// UUID/ULID group) so a row is identifiable without printing the full ID.
func shortSessionID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.IndexByte(id, '-'); i >= 4 && i <= 12 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncateTitle shortens s to at most n runes, appending an ellipsis when cut.
func truncateTitle(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}

// relativeTime renders an RFC3339 timestamp as a short, human delta
// ("just now", "3m ago", "2h ago", "5d ago"), falling back to the calendar date
// for older or unparseable values. Empty input yields an empty string.
func relativeTime(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return ""
	}
	t, parsed := parseActivityTime(iso)
	if !parsed {
		if len(iso) >= 10 {
			return iso[:10]
		}
		return iso
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// parseActivityTime parses a last_activity value.
func parseActivityTime(iso string) (time.Time, bool) {
	iso = strings.TrimSpace(iso)
	// last_activity is a MAX() across several columns, so the layout varies:
	// RFC3339 (with/without fractional seconds) from Go-written rows, and the
	// SQLite CURRENT_TIMESTAMP / datetime('now') form ("2006-01-02 15:04:05",
	// UTC, space-separated) from columns with DB-side defaults.
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for i, layout := range layouts {
		var t time.Time
		var err error
		// The SQLite layouts have no zone; treat them as UTC (how SQLite writes them).
		if i >= 2 {
			t, err = time.ParseInLocation(layout, iso, time.UTC)
		} else {
			t, err = time.Parse(layout, iso)
		}
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// joinChooserSegments joins non-empty segments with a middot separator.
func joinChooserSegments(segs ...string) string {
	out := segs[:0]
	for _, s := range segs {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " · ")
}

func currentSessionActionLabel(grouped dbpkg.HarnessGroupedResumableSessions) string {
	if grouped.CurrentIsLast {
		return "Continue last session"
	}
	return "Resume this session"
}

// describeCurrentSession renders the body of the current-session slot: what the
// session was about (its prompt in quotes, else its short id), the worktree it
// ran in when that is not the project root, and when it was last active. It
// intentionally does not render work-item attribution: claims and
// active_work_items are mutable, and a stale attribution row should not make
// the session look like a different bug or feature.
func describeCurrentSession(row dbpkg.ResumableSession) string {
	about := shortSessionID(row.LastSessionID)
	if prompt := truncateTitle(row.PromptLabel, maxChooserPromptLen); prompt != "" {
		about = `"` + prompt + `"`
	}
	worktree := ""
	if wt := strings.TrimSpace(row.ExecWorktreePath); wt != "" {
		worktree = "in " + filepath.Base(wt)
	}
	return joinChooserSegments(about, worktree, activityLabel(row))
}

// describeResumableSession renders a work-item row: the id, then its title (or
// the session's prompt when the item has no title), the harness it last ran in
// when that is not the one being launched, and when it was last active.
func describeResumableSession(row dbpkg.ResumableSession, sameHarness bool) string {
	about := truncateTitle(row.Title, maxChooserTitleLen)
	if about == "" {
		about = truncateTitle(row.PromptLabel, maxChooserTitleLen)
	}
	from := ""
	if !sameHarness {
		from = "from " + formatHarnessName(row.Harness)
	}
	return row.WorkItemID + "  " + joinChooserSegments(about, from, activityLabel(row))
}

// activityLabel is a row's relative last-activity time, marked "live" when the
// session may still be running.
func activityLabel(row dbpkg.ResumableSession) string {
	when := relativeTime(row.LastActivity)
	if row.Live {
		when = joinChooserSegments("live", when)
	}
	return when
}

func formatHarnessName(harness string) string {
	switch strings.ToLower(strings.TrimSpace(harness)) {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini"
	case "antigravity":
		return "Antigravity"
	default:
		h := strings.TrimSpace(harness)
		if h == "" {
			return "Harness"
		}
		return strings.ToUpper(h[:1]) + h[1:]
	}
}

func applyClaudeLaunchIntent(resumeID, workItem string, intent launcher.LaunchIntent) claudeIntentResult {
	result := claudeIntentResult{
		mode:     "default",
		resumeID: resumeID,
		workItem: workItem,
		intent:   intent,
	}
	if intent.WantsBrowse() {
		result.mode = "browse"
		result.browse = true
		return result
	}
	if !intent.WantsContinue() {
		return result
	}
	result.mode = "continue"
	if result.workItem == "" && intent.WorkItemID != "" {
		result.workItem = intent.WorkItemID
	}
	// Only a real Claude session id goes straight to --resume. Older ledger rows
	// recorded as "claude-code" can hold a wipnote or OTel id; leaving resumeID
	// empty lets resolveContinueLaunchContext validate and rehydrate instead.
	if id := intent.ResumeForHarness("claude"); result.resumeID == "" && isClaudeCodeSessionID(id) {
		result.resumeID = id
	}
	return result
}

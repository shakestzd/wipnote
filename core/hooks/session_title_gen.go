package hooks

// Generated session titles (feat-00e8112d). A session launched with no work
// item keeps its launch name ("<project>-<date-time>") as a prefix and gets
// " · <title>" appended, where the title is a few words a small model writes
// from the first prompt and, later, from the session's commits and changed
// files. Hooks never wait on the model: they start a detached `wipnote`
// child (spawnTitleGenerator) that writes the ready title to a pending file,
// and the next UserPromptSubmit applies it through sessionTitle. An active
// work item's title always wins (session_title.go).

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/shakestzd/wipnote/core/paths"
)

// LaunchNameEnv carries the launch name the launcher passed as --name. It is
// set only for an item-less launch's default name; every other launch sets it
// empty, so a nested launch never inherits an outer one.
const LaunchNameEnv = "WIPNOTE_LAUNCH_NAME"

// Title sources a generator run reads.
const (
	TitleFromPrompt = "prompt"
	TitleFromWork   = "work"
)

const (
	launchNameFile     = ".launch-name"
	titlePromptFile    = ".title-prompt"
	pendingGenTitle    = ".pending-generated-title"
	titleSeededFile    = ".title-seeded"
	titleSeparator     = " · "
	titlePromptMaxRune = 2000
)

// TitleJob is one background title run.
type TitleJob struct {
	ProjectDir string // the project whose .wipnote/sessions holds the state
	RepoDir    string // where git reads commits and changes (the event cwd)
	SessionID  string
	Source     string // TitleFromPrompt or TitleFromWork
}

// spawnTitleGenerator starts the detached generator. A package var so tests
// record jobs instead of starting processes.
var spawnTitleGenerator = startTitleGenerator

func sessionStateDir(projectDir, sessionID string) string {
	return filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
}

// SessionTitleGenerationEnabled reports .wipnote/config.json
// session_titles.generate; generation is on unless it is false.
func SessionTitleGenerationEnabled(projectDir string) bool {
	var cfg struct {
		SessionTitles struct {
			Generate *bool `json:"generate"`
		} `json:"session_titles"`
	}
	if projectDir != "" && paths.ReadProjectConfig(projectDir, &cfg) && cfg.SessionTitles.Generate != nil {
		return *cfg.SessionTitles.Generate
	}
	return true
}

// titleGenerationApplies reports whether this event's session may get a
// generated title: a top-level Claude session that is not a delegate worker,
// with generation on in the project config.
func titleGenerationApplies(event *CloudEvent, projectDir string) bool {
	if isSubagentEvent(event) || os.Getenv(DelegateRunEnv) != "" {
		return false
	}
	if currentHarness() != HarnessClaude {
		return false
	}
	return SessionTitleGenerationEnabled(projectDir)
}

// recordLaunchName stores the launch name for the session the first time a
// hook sees it, and returns the stored name ("" when there is none). A
// resumed session carries no launch name and keeps the one stored at launch.
func recordLaunchName(projectDir, sessionID string) string {
	dir := sessionStateDir(projectDir, sessionID)
	if data, err := os.ReadFile(filepath.Join(dir, launchNameFile)); err == nil {
		return strings.TrimSpace(string(data))
	}
	name := strings.TrimSpace(os.Getenv(LaunchNameEnv))
	if name == "" || os.MkdirAll(dir, 0o755) != nil {
		return ""
	}
	if os.WriteFile(filepath.Join(dir, launchNameFile), []byte(name+"\n"), 0o644) != nil {
		return ""
	}
	return name
}

// seedTitleFromPrompt starts the first-prompt title run once per session.
func seedTitleFromPrompt(event *CloudEvent, projectDir, sessionID, prompt string) {
	dir := sessionStateDir(projectDir, sessionID)
	seeded, err := os.OpenFile(filepath.Join(dir, titleSeededFile), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = seeded.Close()
	r := []rune(prompt)
	if len(r) > titlePromptMaxRune {
		r = r[:titlePromptMaxRune]
	}
	if os.WriteFile(filepath.Join(dir, titlePromptFile), []byte(string(r)), 0o600) != nil {
		return
	}
	spawnTitleGenerator(TitleJob{ProjectDir: projectDir, RepoDir: event.CWD, SessionID: sessionID, Source: TitleFromPrompt})
}

// generatedTitleOnPrompt runs in UserPromptSubmit when no item title is due.
// With an active item it drops any ready title (the item name stands);
// otherwise it applies a ready title and, on the session's first prompt,
// starts the prompt-based run.
func generatedTitleOnPrompt(event *CloudEvent, projectDir, sessionID, activeItem, prompt string) string {
	if !titleGenerationApplies(event, projectDir) {
		return ""
	}
	launch := recordLaunchName(projectDir, sessionID)
	if launch == "" {
		return ""
	}
	title := takePendingGeneratedTitle(projectDir, sessionID)
	if activeItem != "" {
		return ""
	}
	seedTitleFromPrompt(event, projectDir, sessionID, prompt)
	return ComposeSessionName(launch, title, sessionTitleWidth)
}

// refreshTitleOnStop starts the work-based run at the end of an item-less
// turn. The child compares the session's commits and changed files with the
// last naming and calls the model only when they differ.
func refreshTitleOnStop(event *CloudEvent, projectDir, sessionID string) {
	if !titleGenerationApplies(event, projectDir) {
		return
	}
	if recordLaunchName(projectDir, sessionID) == "" {
		return
	}
	if canonicalFamilyActiveWorkItem(projectDir, sessionID) != "" {
		return
	}
	spawnTitleGenerator(TitleJob{ProjectDir: projectDir, RepoDir: event.CWD, SessionID: sessionID, Source: TitleFromWork})
}

// WritePendingGeneratedTitle stores a ready title for the next prompt. The
// generator writes it to a temporary file and renames it, so a prompt never
// reads half a title.
func WritePendingGeneratedTitle(projectDir, sessionID, title string) error {
	title = SanitizeGeneratedTitle(title, sessionTitleWidth)
	if title == "" {
		return nil
	}
	dir := sessionStateDir(projectDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, pendingGenTitle+".tmp")
	if err := os.WriteFile(tmp, []byte(title+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, pendingGenTitle))
}

// takePendingGeneratedTitle returns the ready title and removes it, so one
// title is applied once.
func takePendingGeneratedTitle(projectDir, sessionID string) string {
	path := filepath.Join(sessionStateDir(projectDir, sessionID), pendingGenTitle)
	data, err := os.ReadFile(path)
	if err != nil || os.Remove(path) != nil {
		return ""
	}
	return SanitizeGeneratedTitle(string(data), sessionTitleWidth)
}

// ComposeSessionName is "<launch> · <title>". The launch prefix is kept
// whole; only the title is cut, to width runes on a word boundary. With no
// launch name or no title it returns "".
func ComposeSessionName(launch, title string, width int) string {
	launch = strings.TrimSpace(launch)
	title = SanitizeGeneratedTitle(title, width)
	if launch == "" || title == "" {
		return ""
	}
	return launch + titleSeparator + title
}

// SanitizeGeneratedTitle makes model output a title: the first non-empty
// line, without quotes, markdown marks, control characters or a trailing
// period, spaces collapsed, cut to width runes on a word boundary.
func SanitizeGeneratedTitle(s string, width int) string {
	line := ""
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	line = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == ' ', r == ' ':
			return ' '
		case strings.ContainsRune("\"`“”*#<>", r):
			return -1
		}
		return r
	}, line)
	line = strings.Trim(strings.TrimPrefix(strings.TrimSpace(line), "Title:"), " '‘’")
	line = strings.TrimRight(strings.Join(strings.Fields(line), " "), ".:;,!- ")
	return cutWords(line, width)
}

// cutWords cuts s to at most width runes, at the last space when there is one.
func cutWords(s string, width int) string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return s
	}
	cut := string(r[:width])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, ".:;,!- ")
}

package hooks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testLaunchName = "wipnote-20261116-22.59.16"

// recordTitleJobs stubs the generator spawn and returns the jobs it got.
func recordTitleJobs(t *testing.T) *[]TitleJob {
	t.Helper()
	var jobs []TitleJob
	prev := spawnTitleGenerator
	spawnTitleGenerator = func(j TitleJob) { jobs = append(jobs, j) }
	t.Cleanup(func() { spawnTitleGenerator = prev })
	return &jobs
}

func stateFile(projectDir, sid, name string) string {
	return filepath.Join(sessionStateDir(projectDir, sid), name)
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func writeProjectConfig(t *testing.T, projectDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectDir, ".wipnote", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestComposeSessionName_KeepsLaunchPrefix: the launch name is kept whole on
// every rename; only the title part is cut, on a word boundary.
func TestComposeSessionName_KeepsLaunchPrefix(t *testing.T) {
	got := ComposeSessionName(testLaunchName, "fix delegate stop drafts", sessionTitleWidth)
	if got != testLaunchName+" · fix delegate stop drafts" {
		t.Fatalf("compose = %q", got)
	}
	long := strings.Repeat("word ", 30)
	got = ComposeSessionName(testLaunchName, long, 12)
	if !strings.HasPrefix(got, testLaunchName+" · ") {
		t.Fatalf("prefix lost: %q", got)
	}
	if title := strings.TrimPrefix(got, testLaunchName+" · "); title != "word word" {
		t.Fatalf("title part = %q, want cut to whole words within 12 runes", title)
	}
	longPrefix := strings.Repeat("p", 60)
	if got := ComposeSessionName(longPrefix, "short title", 10); !strings.HasPrefix(got, longPrefix+" · ") {
		t.Fatalf("a long prefix was cut: %q", got)
	}
	if ComposeSessionName("", "title", 48) != "" || ComposeSessionName(testLaunchName, " \n", 48) != "" {
		t.Fatal("no launch name or no title must compose nothing")
	}
}

// TestSanitizeGeneratedTitle: model output becomes one plain bounded line.
func TestSanitizeGeneratedTitle(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Fix delegate stop drafts", "Fix delegate stop drafts"},
		{"\n\n\"Fix delegate stop drafts.\"\nsecond line", "Fix delegate stop drafts"},
		{"Title: **name sessions from work**", "name sessions from work"},
		{"`add\tconfig\x07 switch`", "add config switch"},
		{"'don't break hooks'", "don't break hooks"},
		{"“curly quoted title”", "curly quoted title"},
		{"  \n  ", ""},
		{strings.Repeat("abcdefghij ", 10), "abcdefghij abcdefghij abcdefghij abcdefghij"},
	} {
		if got := SanitizeGeneratedTitle(tc.in, sessionTitleWidth); got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := SanitizeGeneratedTitle(tc.in, sessionTitleWidth); len([]rune(got)) > sessionTitleWidth {
			t.Errorf("sanitize(%q) is %d runes", tc.in, len([]rune(got)))
		}
	}
}

// TestUserPrompt_GeneratedTitleHandoff: the first prompt records the launch
// name and starts one prompt run; a title the generator writes is applied
// by the next prompt as "<launch> · <title>", once.
func TestUserPrompt_GeneratedTitleHandoff(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a001"
	projectDir := taskClaimProject(t, sid, false)
	t.Setenv(LaunchNameEnv, testLaunchName)
	jobs := recordTitleJobs(t)

	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("first prompt renamed before any title was ready: %q", got)
	}
	if len(*jobs) != 1 || (*jobs)[0].Source != TitleFromPrompt || (*jobs)[0].SessionID != sid {
		t.Fatalf("jobs = %+v, want one prompt run", *jobs)
	}
	if data, _ := os.ReadFile(stateFile(projectDir, sid, titlePromptFile)); string(data) != "carry on with the work" {
		t.Fatalf("saved prompt = %q", data)
	}
	if err := WritePendingGeneratedTitle(projectDir, sid, "\"Carry on the work.\""); err != nil {
		t.Fatal(err)
	}
	t.Setenv(LaunchNameEnv, "other-launch") // a later env never replaces the recorded prefix
	if got := promptTitle(t, projectDir, sid); got != testLaunchName+" · Carry on the work" {
		t.Fatalf("sessionTitle = %q", got)
	}
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("a title was applied twice: %q", got)
	}
	if len(*jobs) != 1 {
		t.Fatalf("later prompts started more runs: %+v", *jobs)
	}
}

// TestUserPrompt_ItemTitleBeatsGenerated: with an active item a ready
// generated title is dropped and never applied; an item title due on the
// same prompt is the one applied.
func TestUserPrompt_ItemTitleBeatsGenerated(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a002"
	projectDir := sessionTitleProject(t, sid)
	t.Setenv(LaunchNameEnv, testLaunchName)
	jobs := recordTitleJobs(t)

	_ = WritePendingGeneratedTitle(projectDir, sid, "generated title")
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("generated title applied over an active item: %q", got)
	}
	if fileExists(stateFile(projectDir, sid, pendingGenTitle)) {
		t.Fatal("generated title kept while an item is active")
	}
	_ = WritePendingGeneratedTitle(projectDir, sid, "generated title")
	if err := MarkSessionTitlePending(projectDir, sid, "spk-0a0b0c0d"); err != nil {
		t.Fatal(err)
	}
	if got := promptTitle(t, projectDir, sid); got != "spk-0a0b0c0d Research caching" {
		t.Fatalf("sessionTitle = %q, want the item title", got)
	}
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("generated title applied after the item title: %q", got)
	}
	if len(*jobs) != 0 {
		t.Fatalf("runs started with an active item: %+v", *jobs)
	}
}

// TestGeneratedTitle_OffSwitch: session_titles.generate false starts no run,
// applies no ready title and makes a run write nothing.
func TestGeneratedTitle_OffSwitch(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a003"
	projectDir := taskClaimProject(t, sid, false)
	writeProjectConfig(t, projectDir, `{"session_titles": {"generate": false}}`)
	t.Setenv(LaunchNameEnv, testLaunchName)
	jobs := recordTitleJobs(t)

	_ = WritePendingGeneratedTitle(projectDir, sid, "generated title")
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("title applied with generation off: %q", got)
	}
	refreshTitleOnStop(&CloudEvent{SessionID: sid, CWD: projectDir}, projectDir, sid)
	if len(*jobs) != 0 {
		t.Fatalf("runs started with generation off: %+v", *jobs)
	}
	_ = os.WriteFile(stateFile(projectDir, sid, titlePromptFile), []byte("a prompt"), 0o644)
	called := false
	model := func(context.Context, string, string) (string, error) { called = true; return "x", nil }
	job := TitleJob{ProjectDir: projectDir, SessionID: sid, Source: TitleFromPrompt}
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil || called {
		t.Fatalf("run with generation off: err=%v model called=%v", err, called)
	}
}

// TestGeneratedTitle_WorkerAndSubagentGuards: a delegate worker, a
// subagent and a session with no launch name are never renamed or queued.
func TestGeneratedTitle_WorkerAndSubagentGuards(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a004"
	projectDir := taskClaimProject(t, sid, false)
	jobs := recordTitleJobs(t)
	_ = WritePendingGeneratedTitle(projectDir, sid, "generated title")

	t.Setenv(LaunchNameEnv, "") // not launched with a default name
	if got := promptTitle(t, projectDir, sid); got != "" || len(*jobs) != 0 {
		t.Fatalf("no launch name: title %q jobs %+v", got, *jobs)
	}
	t.Setenv(LaunchNameEnv, testLaunchName)
	t.Setenv(DelegateRunEnv, "dlg-000000000000")
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("worker renamed: %q", got)
	}
	refreshTitleOnStop(&CloudEvent{SessionID: sid, CWD: projectDir}, projectDir, sid)
	t.Setenv(DelegateRunEnv, "")
	sub := &CloudEvent{SessionID: sid, CWD: projectDir, AgentID: "a1b2c3", Prompt: "x"}
	if got := generatedTitleOnPrompt(sub, projectDir, sid, "", "x"); got != "" {
		t.Fatalf("subagent renamed: %q", got)
	}
	refreshTitleOnStop(sub, projectDir, sid)
	if len(*jobs) != 0 {
		t.Fatalf("guarded sessions started runs: %+v", *jobs)
	}
}

// TestRefreshTitleOnStop_OnlyWithoutItem: an item-less turn end starts a
// work run; one with an active item does not.
func TestRefreshTitleOnStop_OnlyWithoutItem(t *testing.T) {
	t.Setenv(LaunchNameEnv, testLaunchName)
	jobs := recordTitleJobs(t)
	const free = "aaaa4444-bbbb-cccc-dddd-eeee0000a005"
	projectDir := taskClaimProject(t, free, false)
	refreshTitleOnStop(&CloudEvent{SessionID: free, CWD: projectDir}, projectDir, free)
	if len(*jobs) != 1 || (*jobs)[0].Source != TitleFromWork || (*jobs)[0].RepoDir != projectDir {
		t.Fatalf("jobs = %+v, want one work run", *jobs)
	}
	const busy = "aaaa4444-bbbb-cccc-dddd-eeee0000a006"
	itemProject := sessionTitleProject(t, busy)
	refreshTitleOnStop(&CloudEvent{SessionID: busy, CWD: itemProject}, itemProject, busy)
	if len(*jobs) != 1 {
		t.Fatalf("a run started with an active item: %+v", *jobs)
	}
}

// TestGenerateSessionTitle_FromPrompt: a prompt run hands the saved prompt
// to the model once and writes the sanitized title; a failing model leaves
// nothing.
func TestGenerateSessionTitle_FromPrompt(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a007"
	projectDir := taskClaimProject(t, sid, false)
	dir := sessionStateDir(projectDir, sid)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, titlePromptFile), []byte("please fix the stop hook drafts"), 0o644)
	var input string
	model := func(_ context.Context, system, in string) (string, error) {
		if system != TitleSystemPrompt {
			t.Errorf("system prompt = %q", system)
		}
		input = in
		return "\"Fix stop hook drafts.\"\n", nil
	}
	job := TitleJob{ProjectDir: projectDir, SessionID: sid, Source: TitleFromPrompt}
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil {
		t.Fatal(err)
	}
	if input != "please fix the stop hook drafts" {
		t.Fatalf("model input = %q", input)
	}
	if got := takePendingGeneratedTitle(projectDir, sid); got != "Fix stop hook drafts" {
		t.Fatalf("pending title = %q", got)
	}
	if fileExists(filepath.Join(dir, titlePromptFile)) || fileExists(filepath.Join(dir, titleLockFile)) {
		t.Fatal("prompt file or lock left behind")
	}
	_ = os.WriteFile(filepath.Join(dir, titlePromptFile), []byte("again"), 0o644)
	fail := func(context.Context, string, string) (string, error) { return "", errors.New("timeout") }
	if err := GenerateSessionTitle(context.Background(), job, fail); err == nil {
		t.Fatal("model failure not reported")
	}
	if fileExists(filepath.Join(dir, pendingGenTitle)) {
		t.Fatal("a failed run wrote a title")
	}
}

// TestGenerateSessionTitle_FromWork: a work run names the session from its
// commits and changed files, and calls the model again only when they
// change.
func TestGenerateSessionTitle_FromWork(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000a008"
	projectDir := taskClaimProject(t, sid, false)
	repo := t.TempDir()
	gitT(t, repo, "init", "-q")
	dir := sessionStateDir(projectDir, sid)
	_ = os.MkdirAll(dir, 0o755)
	launch := filepath.Join(dir, launchNameFile)
	_ = os.WriteFile(launch, []byte(testLaunchName+"\n"), 0o644)
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(launch, past, past)

	calls := 0
	var input string
	model := func(_ context.Context, _, in string) (string, error) {
		calls++
		input = in
		return "stop hook drafts", nil
	}
	job := TitleJob{ProjectDir: projectDir, RepoDir: repo, SessionID: sid, Source: TitleFromWork}
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil || calls != 0 {
		t.Fatalf("no work yet: err=%v calls=%d", err, calls)
	}
	_ = os.WriteFile(filepath.Join(repo, "stop.go"), []byte("package x\n"), 0o644)
	gitT(t, repo, "add", "stop.go")
	gitT(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "fix stop drafts")
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil || calls != 1 {
		t.Fatalf("after a commit: err=%v calls=%d", err, calls)
	}
	if !strings.Contains(input, "fix stop drafts") || !strings.Contains(input, "stop.go") {
		t.Fatalf("model input = %q, want commit subject and file", input)
	}
	if got := takePendingGeneratedTitle(projectDir, sid); got != "stop hook drafts" {
		t.Fatalf("pending title = %q", got)
	}
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil || calls != 1 {
		t.Fatalf("unchanged work called the model again: err=%v calls=%d", err, calls)
	}
	_ = os.WriteFile(filepath.Join(repo, "new.go"), []byte("package x\n"), 0o644)
	if err := GenerateSessionTitle(context.Background(), job, model); err != nil || calls != 2 {
		t.Fatalf("a new changed file: err=%v calls=%d", err, calls)
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

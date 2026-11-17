package hooks

// The background half of generated session titles (feat-00e8112d): the
// detached `wipnote` child a hook starts. It reads its input (the first
// prompt, or the session's commits and changed files), asks the model for a
// title and leaves it in the pending file. Any failure leaves the name as it
// is.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	titleBasisFile  = ".title-basis"
	titleLockFile   = ".title-lock"
	titleLockMaxAge = 2 * time.Minute
)

// TitleModel asks a model for a title. system is the instruction, input
// the material to name.
type TitleModel func(ctx context.Context, system, input string) (string, error)

// TitleSystemPrompt is the instruction the model gets.
const TitleSystemPrompt = "You name a coding session. Reply with one plain title of 3 to 6 words " +
	"that says what the work is about, such as: fix delegate stop drafts. " +
	"Lowercase, no quotes, no punctuation at the end, nothing else."

// errTitleBusy reports that another run for the session holds the lock.
var errTitleBusy = errors.New("session title: another run is in progress")

// startTitleGenerator starts `wipnote session-title` detached, with its
// standard streams on the null device, and does not wait for it. A test
// binary never starts it: its executable would rerun the test suite.
func startTitleGenerator(job TitleJob) {
	exe, err := os.Executable()
	if err != nil || strings.HasSuffix(filepath.Base(exe), ".test") {
		return
	}
	cmd := exec.Command(exe, "session-title", "--project", job.ProjectDir, "--repo", job.RepoDir,
		"--session", job.SessionID, "--from", job.Source)
	cmd.SysProcAttr = detachAttr()
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// GenerateSessionTitle is one generator run: it writes a pending title for
// job's session, or nothing when generation is off, the input is empty or
// unchanged since the last naming, or the model fails.
func GenerateSessionTitle(ctx context.Context, job TitleJob, model TitleModel) error {
	if job.ProjectDir == "" || job.SessionID == "" || !SessionTitleGenerationEnabled(job.ProjectDir) {
		return nil
	}
	dir := sessionStateDir(job.ProjectDir, job.SessionID)
	unlock, err := lockTitleRun(dir)
	if err != nil {
		return err
	}
	defer unlock()
	input, basis := titleInput(job, dir)
	if input == "" {
		return nil
	}
	out, err := model(ctx, TitleSystemPrompt, input)
	if err != nil {
		return err
	}
	if err := WritePendingGeneratedTitle(job.ProjectDir, job.SessionID, out); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, titleBasisFile), []byte(basis+"\n"), 0o644)
}

// titleInput is the material to name and the work fingerprint to store. A
// prompt run takes the saved prompt (once) and records the work as it
// stands, so later turns re-name only on new work. A work run returns ""
// when there is no work or it is unchanged.
func titleInput(job TitleJob, dir string) (input, basis string) {
	work := sessionWork(job.RepoDir, launchTime(dir))
	basis = work.fingerprint()
	if job.Source == TitleFromPrompt {
		path := filepath.Join(dir, titlePromptFile)
		data, err := os.ReadFile(path)
		_ = os.Remove(path)
		if err != nil {
			return "", ""
		}
		return strings.TrimSpace(string(data)), basis
	}
	if work.empty() {
		return "", ""
	}
	if prev, err := os.ReadFile(filepath.Join(dir, titleBasisFile)); err == nil && strings.TrimSpace(string(prev)) == basis {
		return "", ""
	}
	return work.describe(), basis
}

// lockTitleRun holds the session's run lock, so two runs never race on one
// title. A lock older than titleLockMaxAge is from a run that died and is
// taken over.
func lockTitleRun(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, titleLockFile)
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		info, statErr := os.Stat(path)
		if statErr != nil || time.Since(info.ModTime()) < titleLockMaxAge {
			return nil, errTitleBusy
		}
		_ = os.Remove(path)
	}
	return nil, errTitleBusy
}

// launchTime is when the session's launch name was recorded: work before it
// is not the session's.
func launchTime(dir string) time.Time {
	if info, err := os.Stat(filepath.Join(dir, launchNameFile)); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// sessionWorkSummary is the session's work as git sees it.
type sessionWorkSummary struct {
	Subjects []string // commit subjects since launch, newest first
	Files    []string // files those commits touched plus uncommitted changes
}

func (w sessionWorkSummary) empty() bool { return len(w.Subjects) == 0 && len(w.Files) == 0 }

func (w sessionWorkSummary) fingerprint() string {
	h := sha256.Sum256([]byte(strings.Join(w.Subjects, "\n") + "\x00" + strings.Join(w.Files, "\n")))
	return hex.EncodeToString(h[:8])
}

func (w sessionWorkSummary) describe() string {
	var b strings.Builder
	if len(w.Subjects) > 0 {
		b.WriteString("Commits:\n" + strings.Join(w.Subjects, "\n") + "\n")
	}
	if len(w.Files) > 0 {
		b.WriteString("Changed files:\n" + strings.Join(w.Files, "\n") + "\n")
	}
	return strings.TrimSpace(b.String())
}

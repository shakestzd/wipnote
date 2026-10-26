package gitwatch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testRepo is a throwaway repository with a bare "origin" beside it. Every
// test builds its own under t.TempDir; none touches the real project.
type testRepo struct {
	t      *testing.T
	root   string // parent of work and origin
	work   string
	origin string
}

// isolateEnv keeps a test away from the developer's config and project.
func isolateEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// newTestRepo makes work (branch main, one commit) pushed to origin.
func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateEnv(t)
	root := t.TempDir()
	r := &testRepo{t: t, root: root, work: filepath.Join(root, "work"), origin: filepath.Join(root, "origin.git")}
	r.gitIn(root, nil, "init", "-q", "--bare", "-b", "main", r.origin)
	r.gitIn(root, nil, "init", "-q", "-b", "main", r.work)
	r.git("config", "user.email", "test@example.test")
	r.git("config", "user.name", "Test")
	r.git("remote", "add", "origin", r.origin)
	r.commit("base.txt", "", 0)
	r.git("push", "-q", "-u", "origin", "main")
	return r
}

// git runs git in the work tree and returns trimmed stdout.
func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	return r.gitIn(r.work, nil, args...)
}

func (r *testRepo) gitIn(dir string, env []string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes name (content defaults to name plus the time) and commits
// it in dir (work when empty), dated ago before now.
func (r *testRepo) commitIn(dir, name, content string, ago time.Duration) {
	r.t.Helper()
	if dir == "" {
		dir = r.work
	}
	if content == "" {
		content = name + time.Now().String()
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
	when := time.Now().Add(-ago).Format(time.RFC3339)
	env := []string{"GIT_COMMITTER_DATE=" + when, "GIT_AUTHOR_DATE=" + when}
	r.gitIn(dir, nil, "add", name)
	r.gitIn(dir, env, "commit", "-q", "-m", "add "+name)
}

func (r *testRepo) commit(name, content string, ago time.Duration) {
	r.t.Helper()
	r.commitIn("", name, content, ago)
}

// splitRunner sends git to Git and gh to GH, so a test can pair a real temp
// repository with recorded gh output.
type splitRunner struct {
	Git Runner
	GH  Runner
}

func (s splitRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	if name == "gh" {
		return s.GH.Run(ctx, name, args...)
	}
	return s.Git.Run(ctx, name, args...)
}

// oldGitRunner behaves like a git older than 2.41: a for-each-ref format
// with %(ahead-behind:...) fails as that git does. Calls records argv.
type oldGitRunner struct {
	Calls *[]string
}

func (o oldGitRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	joined := strings.Join(args, " ")
	*o.Calls = append(*o.Calls, joined)
	if strings.Contains(joined, "for-each-ref") && strings.Contains(joined, "%(ahead-behind:") {
		return nil, []byte("fatal: unknown field name: ahead-behind:origin/main\n"), 128, nil
	}
	return ExecRunner{}.Run(ctx, name, args...)
}

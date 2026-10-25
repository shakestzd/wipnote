package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// liveEnv holds the HERDR_* variables the test process started with. TestMain
// clears them so no test can reach a real herdr by accident; only the live
// test (HERDR_ENV=1 at start) puts them back.
var liveEnv map[string]string

// TestMain doubles as the fake herdr: the harness links the test binary into
// a temp dir as "herdr" and puts that dir first on PATH, so every herdr the
// client runs is this binary replaying a script.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "herdr" {
		os.Exit(FakeMain(os.Args[1:], os.Stdout, os.Stderr))
	}
	liveEnv = map[string]string{}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "HERDR_") {
			liveEnv[name] = value
			_ = os.Unsetenv(name)
		}
	}
	_ = os.Unsetenv("WIPNOTE_PROJECT_DIR")
	_ = os.Unsetenv("CLAUDE_PROJECT_DIR")
	os.Exit(m.Run())
}

// fakeRun is one installed fake: its script copy and its call log.
type fakeRun struct {
	t      *testing.T
	script string
	log    string
	// allowUnscripted is set by the one test that makes an unscripted call
	// on purpose.
	allowUnscripted bool
}

// installFake copies testdata/scripts/<name>.json into a temp dir, links the
// test binary there as "herdr", puts that dir alone in front of PATH and sets
// HERDR_ENV=1. At cleanup an unscripted call fails the test.
func installFake(t *testing.T, name string) *fakeRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "scripts", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return installFakeScript(t, string(data))
}

func installFakeScript(t *testing.T, script string) *fakeRun {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "herdr")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "script.json")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(FakeScriptEnv, path)
	t.Setenv("HERDR_ENV", "1")
	f := &fakeRun{t: t, script: path, log: path + ".log.ndjson"}
	t.Cleanup(f.failOnUnscripted)
	return f
}

func (f *fakeRun) calls() []FakeCall {
	f.t.Helper()
	calls, err := ReadFakeCalls(f.log)
	if err != nil {
		f.t.Fatal(err)
	}
	return calls
}

// argvs returns every logged call as one space-joined line.
func (f *fakeRun) argvs() []string {
	var out []string
	for _, c := range f.calls() {
		out = append(out, strings.Join(c.Argv, " "))
	}
	return out
}

func (f *fakeRun) failOnUnscripted() {
	if f.allowUnscripted {
		return
	}
	for _, c := range f.calls() {
		if c.Unscripted {
			f.t.Errorf("unscripted herdr call: %s", strings.Join(c.Argv, " "))
		}
	}
}

// newClient builds a client from the test's current environment.
func newClient(t *testing.T) *CLI {
	t.Helper()
	return New(EnvFrom(os.Getenv))
}

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func stubCodexHelp(t *testing.T, help string, err error) {
	t.Helper()
	prev := codexHelpFn
	codexHelpFn = func(string) (string, error) { return help, err }
	t.Cleanup(func() { codexHelpFn = prev })
}

// isolateCodexHome points the launcher at a Codex home that holds only the
// given files, and returns it.
func isolateCodexHome(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	writeFiles(t, dir, files)
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const helpWithNoDaemon = "Options:\n      --no-alt-screen\n          Disable alternate screen\n      --no-daemon\n          Run without the shared background server\n"

// herdr-core's manual requires --no-daemon in an hc terminal. A Codex that
// predates the flag exits on it, so the launcher adds it only when the
// installed Codex lists it, and says why when it leaves it out (bug-fc843d71).
func TestApplyCodexNoDaemon(t *testing.T) {
	for _, tt := range []struct {
		name, terminal, help string
		helpErr              error
		args                 []string
		want                 bool
		wantNote             string
	}{
		{"hc terminal, flag listed", "t7", helpWithNoDaemon, nil, nil, true, ""},
		{"hc terminal, old codex", "t7", "Options:\n      --no-alt-screen\n", nil, nil, false, "does not list --no-daemon"},
		{"hc terminal, help failed", "t7", "", errors.New("signal: killed"), nil, false, "could not read `codex --help` (signal: killed)"},
		{"hc terminal, remote server", "t7", helpWithNoDaemon, nil, []string{"--remote", "ws://x"}, false, "remote or shared server"},
		{"hc terminal, queue subcommand", "t7", helpWithNoDaemon, nil, []string{"-m", "o3", "queue", "list"}, false, "remote or shared server"},
		{"hc terminal, a prompt named queue is fine", "t7", helpWithNoDaemon, nil, []string{"-m", "queue", "fix the queue"}, true, ""},
		{"outside hc", "", helpWithNoDaemon, nil, nil, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HC_TERMINAL_ID", tt.terminal)
			stubCodexHelp(t, tt.help, tt.helpErr)
			opts := codexLaunchOpts{ExtraArgs: tt.args}
			note := applyCodexNoDaemon(func() (string, error) { return codexHelpFn("codex") }, &opts)
			if got := slices.Contains(buildCodexArgs(opts, 0, nil), "--no-daemon"); got != tt.want {
				t.Errorf("--no-daemon passed = %v, want %v", got, tt.want)
			}
			if (tt.wantNote == "") != (note == "") || !strings.Contains(note, tt.wantNote) {
				t.Errorf("note = %q, want it to contain %q", note, tt.wantNote)
			}
		})
	}
}

// resolveCodexWritableRoots names a sandbox only when nothing else did.
func TestResolveCodexWritableRoots(t *testing.T) {
	roots := []string{"/store"}
	for _, tt := range []struct {
		name      string
		opts      codexLaunchOpts
		choice    codexSandboxChoice
		wantMode  string
		wantRoots bool
		wantNote  string
	}{
		{"no choice anywhere", codexLaunchOpts{WritableRoots: roots}, codexSandboxChoice{}, codexWorkspaceWrite, true, "--sandbox workspace-write"},
		{"no writable root", codexLaunchOpts{}, codexSandboxChoice{}, "", false, ""},
		{"yolo", codexLaunchOpts{WritableRoots: roots, Yolo: true}, codexSandboxChoice{}, "", true, ""},
		{"launcher already set a sandbox", codexLaunchOpts{WritableRoots: roots, SandboxMode: "danger-full-access"}, codexSandboxChoice{}, "", true, ""},
		{"user bypass", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--dangerously-bypass-approvals-and-sandbox"}}, codexSandboxChoice{ReadOnly: true, Set: true}, "", true, ""},
		{"a writable choice is kept", codexLaunchOpts{WritableRoots: roots}, codexSandboxChoice{Set: true}, "", true, ""},
		{"a read-only choice drops the root", codexLaunchOpts{WritableRoots: roots}, codexSandboxChoice{Set: true, ReadOnly: true, Source: "your Codex config"}, "", false, "read-only here (your Codex config)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mode, got, notice := resolveCodexWritableRoots(tt.opts, tt.choice)
			if mode != tt.wantMode || (len(got) > 0) != tt.wantRoots {
				t.Errorf("mode = %q roots = %q, want mode %q roots kept %v", mode, got, tt.wantMode, tt.wantRoots)
			}
			if (tt.wantNote == "") != (notice == "") || !strings.Contains(notice, tt.wantNote) {
				t.Errorf("notice = %q, want it to contain %q", notice, tt.wantNote)
			}
		})
	}
}

// Every way the user can choose a sandbox is read, so wipnote never widens
// one. Each case is a launch in a project with no trust entry.
func TestReadCodexSandboxChoice(t *testing.T) {
	for _, tt := range []struct {
		name         string
		args         []string
		home         map[string]string
		project      map[string]string
		set, readOnl bool
	}{
		{name: "nothing chosen"},
		{name: "flag", args: []string{"--sandbox", "danger-full-access"}, set: true},
		{name: "short flag read-only", args: []string{"-s", "read-only"}, set: true, readOnl: true},
		{name: "approve-for-me", args: []string{"--approve-for-me"}, set: true},
		{name: "-c K=V", args: []string{"-c", `sandbox_mode="workspace-write"`}, set: true},
		{name: "-c with spaces", args: []string{"-c", `sandbox_mode = "read-only"`}, set: true, readOnl: true},
		{name: "-cK=V attached", args: []string{"-csandbox_mode=read-only"}, set: true, readOnl: true},
		{name: "--config=K=V", args: []string{"--config=sandbox_mode=read-only"}, set: true, readOnl: true},
		{name: "the last -c wins", args: []string{"-c", "sandbox_mode=workspace-write", "-c", "sandbox_mode=read-only"}, set: true, readOnl: true},
		{name: "-c default_permissions", args: []string{"-c", `default_permissions=":read-only"`}, set: true, readOnl: true},
		{name: "a flag beats the config", args: []string{"--sandbox=workspace-write"}, home: map[string]string{"config.toml": `sandbox_mode = "read-only"`}, set: true},
		{name: "config sandbox_mode", home: map[string]string{"config.toml": `sandbox_mode = "danger-full-access"`}, set: true},
		{name: "config read-only", home: map[string]string{"config.toml": "model = \"x\"\nsandbox_mode = \"read-only\"\n"}, set: true, readOnl: true},
		{name: "config default_permissions", home: map[string]string{"config.toml": `default_permissions = ":read-only"`}, set: true, readOnl: true},
		{name: "project config", project: map[string]string{".codex/config.toml": `sandbox_mode = "read-only"`}, set: true, readOnl: true},
		{name: "profile file with a sandbox", args: []string{"-p", "work"}, home: map[string]string{"work.config.toml": `sandbox_mode = "read-only"`}, set: true, readOnl: true},
		{name: "legacy profile table", home: map[string]string{"config.toml": "profile = \"work\"\n[profiles.work]\nsandbox_mode = \"workspace-write\"\n"}, set: true},
		{name: "-c profile", args: []string{"-c", "profile=work"}, home: map[string]string{"work.config.toml": `sandbox_mode = "read-only"`}, set: true, readOnl: true},
		{name: "a profile with no sandbox decides nothing", args: []string{"--profile", "work"}, home: map[string]string{"work.config.toml": `model = "x"`}},
		{name: "a profile beats the base config", args: []string{"-p", "work"}, home: map[string]string{"work.config.toml": `sandbox_mode = "workspace-write"`, "config.toml": `sandbox_mode = "read-only"`}, set: true},
		{name: "words after -- are prompt", args: []string{"--", "--sandbox", "read-only"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateCodexHome(t, tt.home)
			project := t.TempDir()
			writeFiles(t, project, tt.project)
			got := readCodexSandboxChoice(codexLaunchOpts{ProjectRoot: project, ExtraArgs: tt.args})
			if got.Set != tt.set || got.ReadOnly != tt.readOnl {
				t.Errorf("choice = %+v, want set %v read-only %v", got, tt.set, tt.readOnl)
			}
			if got.Set && got.Source == "" {
				t.Error("a set choice names no source")
			}
		})
	}
}

// A trust entry of either kind settles the sandbox: Codex accepts --add-dir
// there with no sandbox flag. Only a project with no entry is left open.
func TestReadCodexSandboxChoice_TrustEntry(t *testing.T) {
	project := t.TempDir()
	sub := filepath.Join(project, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, level, launchDir string
		set, readOnly          bool
	}{
		{"trusted", "trusted", project, true, false},
		{"trusted parent", "trusted", sub, true, false},
		{"untrusted", "untrusted", project, true, false},
		{"entry for another project", "trusted", t.TempDir(), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateCodexHome(t, map[string]string{"config.toml": "[projects.\"" + project + "\"]\ntrust_level = \"" + tt.level + "\"\n"})
			got := readCodexSandboxChoice(codexLaunchOpts{ProjectRoot: tt.launchDir})
			if got.Set != tt.set || got.ReadOnly != tt.readOnly {
				t.Errorf("choice = %+v, want set %v read-only %v", got, tt.set, tt.readOnly)
			}
		})
	}
}

// End to end through the argv, for a fresh launch and both resume shapes.
func TestApplyCodexHostFlags_WritableRootArgv(t *testing.T) {
	t.Setenv("HC_TERMINAL_ID", "")
	stubCodexHelp(t, "an older Codex with no approval preset", nil)
	project := t.TempDir()
	for _, tt := range []struct {
		name string
		opts codexLaunchOpts
		home map[string]string
		want string
		not  []string
	}{
		{"no trust entry", codexLaunchOpts{}, nil, "--sandbox workspace-write --add-dir /store", nil},
		{"resume last", codexLaunchOpts{ResumeLast: true}, nil, "--sandbox workspace-write --add-dir /store resume --last", nil},
		{"resume id", codexLaunchOpts{ResumeID: "abc"}, nil, "--sandbox workspace-write --add-dir /store resume abc", nil},
		{"trusted project", codexLaunchOpts{}, map[string]string{"config.toml": "[projects.\"" + project + "\"]\ntrust_level = \"trusted\"\n"}, "--add-dir /store", []string{"--sandbox"}},
		{"configured read-only", codexLaunchOpts{}, map[string]string{"config.toml": `sandbox_mode = "read-only"`}, "", []string{"--sandbox", "--add-dir"}},
		{"default_permissions override", codexLaunchOpts{ExtraArgs: []string{"-c", "default_permissions=:workspace"}}, nil, "--add-dir /store", []string{"--sandbox"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateCodexHome(t, tt.home)
			opts := tt.opts
			opts.ProjectRoot, opts.WritableRoots = project, []string{"/store"}
			applyCodexHostFlags("codex", &opts)
			argv := buildCodexArgs(opts, 0, nil)
			if got := strings.Join(argv, " "); !strings.Contains(got, tt.want) {
				t.Errorf("argv = %q, want it to contain %q", got, tt.want)
			}
			for _, word := range tt.not {
				if slices.Contains(argv, word) {
					t.Errorf("argv = %q, want no %s", argv, word)
				}
			}
		})
	}
}

// A plain launch gets --approve-for-me. Any sandbox or approval choice the
// user made, and any launch that opens no session, leaves it out.
func TestApplyCodexHostFlags_ApproveForMeDefault(t *testing.T) {
	t.Setenv("HC_TERMINAL_ID", "")
	project := t.TempDir()
	trusted := map[string]string{"config.toml": "[projects.\"" + project + "\"]\ntrust_level = \"trusted\"\n"}
	listed := "      --approve-for-me\n          Route approval requests"
	for _, tt := range []struct {
		name string
		opts codexLaunchOpts
		home map[string]string
		help string
		err  error
		want bool
	}{
		{"plain launch", codexLaunchOpts{}, nil, listed, nil, true},
		{"trusted project", codexLaunchOpts{}, trusted, listed, nil, true},
		{"prompt", codexLaunchOpts{ExtraArgs: []string{"-m", "o3", "fix it"}}, nil, listed, nil, true},
		{"resume", codexLaunchOpts{ResumeLast: true}, nil, listed, nil, true},
		{"resume subcommand", codexLaunchOpts{ExtraArgs: []string{"resume", "--last"}}, nil, listed, nil, true},
		{"older Codex", codexLaunchOpts{}, nil, "no such flag", nil, false},
		{"help failed", codexLaunchOpts{}, nil, "", errors.New("timeout"), false},
		{"yolo", codexLaunchOpts{Yolo: true}, nil, listed, nil, false},
		{"wipnote picked a sandbox", codexLaunchOpts{SandboxMode: "danger-full-access"}, nil, listed, nil, false},
		{"--sandbox", codexLaunchOpts{ExtraArgs: []string{"--sandbox", "read-only"}}, nil, listed, nil, false},
		{"-a", codexLaunchOpts{ExtraArgs: []string{"-a", "on-request"}}, nil, listed, nil, false},
		{"--ask-for-approval=", codexLaunchOpts{ExtraArgs: []string{"--ask-for-approval=never"}}, nil, listed, nil, false},
		{"-c approval_policy", codexLaunchOpts{ExtraArgs: []string{"-c", "approval_policy=never"}}, nil, listed, nil, false},
		{"--full-auto", codexLaunchOpts{ExtraArgs: []string{"--full-auto"}}, nil, listed, nil, false},
		{"configured sandbox", codexLaunchOpts{}, map[string]string{"config.toml": `sandbox_mode = "workspace-write"`}, listed, nil, false},
		{"configured approval policy", codexLaunchOpts{}, map[string]string{"config.toml": `approval_policy = "untrusted"`}, listed, nil, false},
		{"configured reviewer", codexLaunchOpts{}, map[string]string{"config.toml": `approvals_reviewer = "user"`}, listed, nil, false},
		{"exec subcommand", codexLaunchOpts{ExtraArgs: []string{"exec", "do it"}}, nil, listed, nil, false},
		{"login subcommand", codexLaunchOpts{ExtraArgs: []string{"login"}}, nil, listed, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateCodexHome(t, tt.home)
			stubCodexHelp(t, tt.help, tt.err)
			opts := tt.opts
			opts.ProjectRoot, opts.WritableRoots = project, []string{"/store"}
			applyCodexHostFlags("codex", &opts)
			argv := buildCodexArgs(opts, 0, nil)
			if got := slices.Contains(argv, codexApproveForMe); got != tt.want {
				t.Errorf("--approve-for-me passed = %v, want %v (argv %q)", got, tt.want, argv)
			}
			// The preset names the sandbox, so wipnote adds no second one.
			if tt.want && (slices.Contains(argv, "--sandbox") || !slices.Contains(argv, "--add-dir")) {
				t.Errorf("argv = %q, want --add-dir and no --sandbox beside the preset", argv)
			}
		})
	}
}

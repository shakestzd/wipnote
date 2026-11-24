package main

import (
	"slices"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// The second review of feat-c1c93fd2 found launches where the approval default
// overrode a choice the user had made. Each case here is one of its inputs.
func TestApproveForMe_KeepsUserChoices(t *testing.T) {
	t.Setenv("HC_TERMINAL_ID", "")
	t.Setenv(hooks.DelegateRunEnv, "")
	project := t.TempDir()
	const granular = "approval_policy = { granular = { sandbox_approval = true } }\n"
	for _, tt := range []struct {
		name        string
		args        []string
		home        map[string]string
		wantFlag    bool
		wantSandbox bool // wipnote adds --sandbox workspace-write
		wantRoots   bool // --add-dir survives
	}{
		{"table approval policy before a read-only sandbox", nil,
			map[string]string{"config.toml": granular + "sandbox_mode = \"read-only\"\n"}, false, false, false},
		{"table approval policy alone", nil, map[string]string{"config.toml": granular}, false, true, true},
		{"table approval policy before a profile", nil,
			map[string]string{"config.toml": "profile = \"ro\"\n" + granular + "[profiles.ro]\nsandbox_mode = \"read-only\"\n"}, false, false, false},
		{"profile with a table approval policy", []string{"-p", "ro"},
			map[string]string{"config.toml": "[profiles.ro]\n" + granular + "sandbox_mode = \"read-only\"\n"}, false, false, false},
		{"config that is not valid TOML", nil, map[string]string{"config.toml": "sandbox_mode = \n[[["}, false, false, true},
		{"untrusted project", nil,
			map[string]string{"config.toml": "[projects.\"" + project + "\"]\ntrust_level = \"untrusted\"\n"}, false, false, true},
		{"attached profile flag", []string{"-pfoo"},
			map[string]string{"config.toml": "[profiles.foo]\napproval_policy = \"never\"\n"}, false, true, true},
		{"profile override on the line", []string{"-c", "profiles.foo.approval_policy=never", "-p", "foo"}, nil, false, true, true},
		{"profile sandbox override on the line", []string{"-c", "profiles.foo.sandbox_mode=read-only", "-p", "foo"}, nil, false, false, false},
		{"exec alias", []string{"e", "do it"}, nil, false, true, true},
		{"flag-like words after the separator", []string{"fix it", "--", "--sandbox", "read-only"}, nil, false, true, true},
		{"plain prompt after the separator", []string{"-m", "o3", "--", "fix it"}, nil, true, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateCodexHome(t, tt.home)
			stubCodexHelp(t, "      --approve-for-me\n", nil)
			opts := codexLaunchOpts{ExtraArgs: tt.args, ProjectRoot: project, WritableRoots: []string{"/store"}}
			applyCodexHostFlags("codex", &opts)
			argv := buildCodexArgs(opts, 0, nil)
			if got := slices.Contains(argv, codexApproveForMe); got != tt.wantFlag {
				t.Errorf("--approve-for-me passed = %v, want %v (argv %q)", got, tt.wantFlag, argv)
			}
			if got := slices.Contains(argv, codexWorkspaceWrite); got != tt.wantSandbox {
				t.Errorf("--sandbox workspace-write passed = %v, want %v (argv %q)", got, tt.wantSandbox, argv)
			}
			if got := slices.Contains(argv, "--add-dir"); got != tt.wantRoots {
				t.Errorf("--add-dir passed = %v, want %v (argv %q)", got, tt.wantRoots, argv)
			}
		})
	}
}

// A delegated worker keeps the mode its recipe gives it. wipnote's own resume
// puts the flag after the subcommand and the id, where Codex 0.172.0 lists it
// (`codex resume --help`).
func TestApproveForMe_DelegateWorkerAndResumePlacement(t *testing.T) {
	t.Setenv("HC_TERMINAL_ID", "")
	isolateCodexHome(t, nil)
	stubCodexHelp(t, "      --approve-for-me\n", nil)
	project := t.TempDir()

	t.Setenv(hooks.DelegateRunEnv, "dlg-1")
	worker := codexLaunchOpts{ExtraArgs: []string{"the brief", "--model", "o3"}, ProjectRoot: project, WritableRoots: []string{"/store"}}
	applyCodexHostFlags("codex", &worker)
	if argv := buildCodexArgs(worker, 0, nil); slices.Contains(argv, codexApproveForMe) {
		t.Errorf("a delegated worker got --approve-for-me: %q", argv)
	}

	t.Setenv(hooks.DelegateRunEnv, "")
	resume := codexLaunchOpts{ResumeID: "abc", ProjectRoot: project, WritableRoots: []string{"/store"}}
	applyCodexHostFlags("codex", &resume)
	argv := buildCodexArgs(resume, 0, nil)
	at, flag := slices.Index(argv, "resume"), slices.Index(argv, codexApproveForMe)
	if at < 0 || flag != at+2 || argv[at+1] != "abc" {
		t.Errorf("argv = %q, want resume abc --approve-for-me", argv)
	}
}

func TestCodexInfoOnly_ReviewCases(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"help"}, true},
		{[]string{"help", "review"}, true},
		{[]string{"-m", "o3", "--version"}, true},
		{[]string{"-m", "help"}, false},
		{[]string{"fix the help page"}, false},
		{[]string{"fix", "--version"}, false},
		{[]string{"--", "help"}, false},
	} {
		if got := codexInfoOnly(tt.args); got != tt.want {
			t.Errorf("codexInfoOnly(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

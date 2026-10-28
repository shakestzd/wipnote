package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

// doctorProjectThreadLong is `wipnote doctor --project-thread`'s help.
const doctorProjectThreadLong = `A Claude project cloud thread loads CLAUDE.md, .claude/CLAUDE.md and
.claude/rules only. Repo hooks, plugins, skills, agents, commands and .mcp.json
do not load, so wipnote reaches a thread through four parts:

  binary  wipnote on PATH, installed by the cloud environment's setup script
          (scripts/cloud-env-setup.sh)
  plugin  the wipnote plugin, added in Project settings > Plugins
  hooks   the plugin's hooks having run in this session
  rules   CLAUDE.md, .claude/CLAUDE.md or a .claude/rules/*.md that names wipnote

--project-thread checks each part and names the fix for each missing one.
Exits non-zero when a part is missing. It only reads; it changes nothing.
See docs/guides/claude-projects.md.

Other doctors: wipnote launcher doctor (worktrees, git state) and
wipnote delegate doctor (codex, bwrap, devcontainer).`

// doctorCmd is `wipnote doctor` (plan-28b4a7f6 slice 23). Only
// --project-thread exists today; the flag is the caller saying "this is a
// project thread", so the doctor never guesses where it runs (safest
// default: no detection heuristics).
func doctorCmd() *cobra.Command {
	var projectThread bool
	cmd := &cobra.Command{
		Use:   "doctor --project-thread",
		Short: "Check what wipnote needs inside a Claude project thread",
		Long:  doctorProjectThreadLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !projectThread {
				return errors.New("pass --project-thread (see also: wipnote launcher doctor, wipnote delegate doctor)")
			}
			checks := projectThreadChecks(liveProjectThreadProbe())
			if n := codex.WriteChecks(cmd.OutOrStdout(), checks); n > 0 {
				return fmt.Errorf("doctor --project-thread: %d part(s) missing", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&projectThread, "project-thread", false, "check the parts a Claude project thread needs (binary, plugin, hooks, rules)")
	return cmd
}

// projectThreadProbe is what the project-thread checks read; tests fill it.
type projectThreadProbe struct {
	LookPath      func(string) (string, error)
	Getenv        func(string) string
	Home          string
	ProjectDir    string
	NativeSession string // the harness's own session id, "" when unknown
}

// liveProjectThreadProbe reads this process's PATH, environment, HOME and
// project root.
func liveProjectThreadProbe() projectThreadProbe {
	home, _ := os.UserHomeDir()
	return projectThreadProbe{
		LookPath: exec.LookPath, Getenv: os.Getenv, Home: home,
		ProjectDir: projectThreadRoot(), NativeSession: agent.HarnessNativeEnvSessionID(),
	}
}

// projectThreadRoot is the wipnote project root, else the git top level,
// else the working directory: the rules check needs a directory even in a
// repo that has no .wipnote/ yet.
func projectThreadRoot() string {
	if dir, err := findWipnoteDir(); err == nil {
		return filepath.Dir(dir)
	}
	if top, err := paths.GitTopLevel("."); err == nil {
		return top
	}
	cwd, _ := os.Getwd()
	return cwd
}

// projectThreadChecks runs the four checks in a fixed order.
func projectThreadChecks(p projectThreadProbe) []codex.Check {
	hooksRan := threadHooksRan(p)
	return []codex.Check{binaryCheck(p), pluginCheck(p, hooksRan), hooksCheck(hooksRan), rulesCheck(p.ProjectDir)}
}

func binaryCheck(p projectThreadProbe) codex.Check {
	path, err := p.LookPath("wipnote")
	if err != nil {
		return codex.Check{Name: "binary", Status: codex.CheckFail, Detail: "wipnote is not on PATH\n" +
			"fix: add scripts/cloud-env-setup.sh to the cloud environment's setup script " +
			"(it installs a pinned release into ~/.local/bin; set WIPNOTE_BIN_DIR to a directory on PATH if that one is not)"}
	}
	return codex.Check{Name: "binary", Status: codex.CheckPass, Detail: path}
}

// pluginCheck passes when Claude Code loaded the plugin (CLAUDE_PLUGIN_ROOT),
// installed and enabled it, or synced it from claude.ai and left it on, or
// when its hooks ran (in a thread they come only from the plugin).
func pluginCheck(p projectThreadProbe, hooksRan bool) codex.Check {
	c := codex.Check{Name: "plugin", Status: codex.CheckPass}
	if root := p.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" && isWipnotePluginDir(root) {
		c.Detail = root
		return c
	}
	if dir := enabledWipnotePlugin(p); dir != "" {
		c.Detail = dir
		return c
	}
	if hooksRan {
		c.Detail = "not found on disk, but its hooks ran in this session"
		return c
	}
	c.Status = codex.CheckFail
	c.Detail = "the wipnote plugin is not installed and enabled\nfix: add the wipnote plugin in Project settings > Plugins " +
		"(a repo's .claude/ plugin settings do not load in a project thread)"
	return c
}

func hooksCheck(ran bool) codex.Check {
	if ran {
		return codex.Check{Name: "hooks", Status: codex.CheckPass, Detail: "the SessionStart hook ran in this session"}
	}
	return codex.Check{Name: "hooks", Status: codex.CheckFail, Detail: "no wipnote hook has run in this session " +
		"(WIPNOTE_SESSION_ID is not set)\nfix: add the wipnote plugin in Project settings > Plugins and start a new thread; " +
		"repo hooks in .claude/settings.json do not load in a project thread. " +
		"delegate run, wait and result still work without hooks"}
}

// threadHooksRan reports whether wipnote's SessionStart hook ran for this
// session. Safest default: only SessionStart counts (every other hook
// relies on it), proven by the WIPNOTE_SESSION_ID it exports through
// CLAUDE_ENV_FILE, or by .wipnote/.active-session naming this harness
// session (a file left by another session does not count).
func threadHooksRan(p projectThreadProbe) bool {
	if p.Getenv("WIPNOTE_SESSION_ID") != "" {
		return true
	}
	if p.NativeSession == "" {
		return false
	}
	as := hooks.ReadActiveSession(p.ProjectDir)
	return as != nil && as.SessionID == p.NativeSession
}

// pluginSearchDepth bounds the walk under ~/.claude/plugins: an installed
// plugin sits a few levels down (cache/<marketplace>/<name>/<version>).
const pluginSearchDepth = 6

// findWipnotePlugin returns the first directory under root holding a
// .claude-plugin/plugin.json named wipnote, or "".
func findWipnotePlugin(root string) string {
	found := ""
	base := strings.Count(filepath.Clean(root), string(filepath.Separator))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.Count(path, string(filepath.Separator))-base > pluginSearchDepth {
			return fs.SkipDir
		}
		if isWipnotePluginDir(path) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func isWipnotePluginDir(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return false
	}
	var m struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(b, &m) == nil && m.Name == "wipnote"
}

// rulesCheck passes when a file a thread loads names wipnote. @imports
// are not followed (safest default: only what the thread reads directly).
func rulesCheck(projectDir string) codex.Check {
	for _, f := range threadInstructionFiles(projectDir) {
		if b, err := os.ReadFile(f); err == nil && strings.Contains(strings.ToLower(string(b)), "wipnote") {
			rel, _ := filepath.Rel(projectDir, f)
			return codex.Check{Name: "rules", Status: codex.CheckPass, Detail: rel + " names wipnote"}
		}
	}
	return codex.Check{Name: "rules", Status: codex.CheckFail, Detail: "no CLAUDE.md, .claude/CLAUDE.md or .claude/rules/**/*.md names wipnote\n" +
		"fix: add a rule such as .claude/rules/wipnote.md (text in docs/guides/claude-projects.md) and commit it; " +
		"for plan context also paste `wipnote plan project-instructions <plan>` into Project settings > Memory > Project instructions"}
}

// threadInstructionFiles lists the files a project thread loads, in order.
// Claude Code discovers .claude/rules/**/*.md recursively
// (code.claude.com/docs/en/memory, checked 2026-10-28); symlinked
// directories are not followed here.
func threadInstructionFiles(projectDir string) []string {
	files := []string{filepath.Join(projectDir, "CLAUDE.md"), filepath.Join(projectDir, ".claude", "CLAUDE.md")}
	_ = filepath.WalkDir(filepath.Join(projectDir, ".claude", "rules"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

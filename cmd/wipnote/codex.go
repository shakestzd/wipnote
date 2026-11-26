package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/spf13/cobra"
)

// codexHomeDir returns Codex's user directory: $CODEX_HOME when set, else
// ~/.codex. Codex reads its config, hooks, agents, plugin cache and sessions
// from here, so wipnote must write and read the same place.
func codexHomeDir() string {
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// codexConfigPath returns the user-level config.toml in codexHomeDir.
func codexConfigPath() string {
	return filepath.Join(codexHomeDir(), "config.toml")
}

// codexHooksPath returns the path Codex currently reads for user-level hooks.
func codexHooksPath() string {
	return filepath.Join(codexHomeDir(), "hooks.json")
}

// codexAgentsPath returns the documented user-level custom agent directory.
func codexAgentsPath() string {
	return filepath.Join(codexHomeDir(), "agents")
}

func codexProjectAgentsPath(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".codex", "agents")
}

const codexPluginID = "wipnote@wipnote"
const codexLocalPluginCacheVersion = "local"

// renderCodexWarningBanner renders a visually distinct warning notice.
// It is kept separate because sandbox degradation is discovered inside execCodex,
// after the main launch banner has already been printed. Using the same
// RenderLaunchBanner helper keeps the border style consistent.
func renderCodexWarningBanner(warning string) string {
	return launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        "Codex launch notice",
		Warning:         warning,
		WarningSeverity: "amber",
	})
}

// promptYesNo asks the user a yes/no question and returns true if they answer y/Y/yes.
// If yes is true (--yes flag), the function returns true without prompting.
func promptYesNo(question string, yes bool) bool {
	if yes {
		return true
	}
	fmt.Print(question + " [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes"
}

// codexCmd returns the cobra command for `wipnote codex`.
func codexCmd() *cobra.Command {
	var f codexFlags

	cmd := &cobra.Command{
		Use:   "codex",
		Short: "Launch Codex CLI with wipnote context",
		Long: `Launch Codex CLI with wipnote observability context.

Modes:
  wipnote codex                   Launch Codex interactively with wipnote env.
  wipnote codex --init            Install the wipnote Codex marketplace (idempotent).
  wipnote codex --continue        Resume the last Codex session (codex resume --last).
  wipnote codex --resume <id>     Resume a specific Codex session by ID.
  wipnote codex --dev             Register local packages/codex-marketplace/ and launch.
  wipnote codex --feature <id>    Launch in the feature's git worktree.
  wipnote codex --track <id>      Launch in the track's git worktree.
  wipnote codex --yolo            Launch without Codex approvals/sandbox prompts.

Session IDs come from ~/.codex/session_index.jsonl.

Flags that wipnote does not define go to Codex as written, for example
"wipnote codex --sandbox read-only". A flag below keeps wipnote's meaning
when it comes before a Codex subcommand. Every word from a Codex subcommand
on goes to Codex: "wipnote codex review --base main".

A plain launch adds --approve-for-me, Codex's automatic approval review in
the workspace-write sandbox. wipnote leaves it out when you chose a sandbox or
an approval policy yourself, on the line or in your Codex config.

A "--" ends wipnote's flags. After Codex flags it also reaches Codex, so a
prompt may start with a dash: "wipnote codex --search -- '-v fails'". With
no Codex flag before it, write it twice: "wipnote codex -- -- '-v fails'".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodexCmd(&f, args)
		},
	}

	registerCodexFlags(cmd, &f)

	return cmd
}

// codexFlags holds the `wipnote codex` flag values.
type codexFlags struct {
	init_, continue_, dev, cleanup, dryRun, yes, noWorktree, inPlace, yolo, allowNonInteractive bool
	resumeID, trackID, featureID, worktreePath, workItem, baseBranch                            string
}

// runCodexCmd dispatches `wipnote codex` to the selected mode.
func runCodexCmd(f *codexFlags, args []string) error {
	// --init only installs the marketplace and --dry-run only prints;
	// every other mode execs an interactive Codex TUI, so refuse a
	// non-TTY stdin BEFORE any launch marker, serve/collector spawn,
	// worktree or session write happens (issue #148).
	// `codex --version` and a subcommand's --help only print. Run them as
	// they are, with no launch marker, daemon or collector (bug-fc843d71).
	if codexInfoOnly(args) && !f.init_ {
		if f.dryRun {
			fmt.Println("would run: codex " + strings.Join(args, " "))
			return nil
		}
		return runCodexPlainFn(args)
	}
	if !f.init_ && !f.dryRun {
		if err := requireInteractiveLaunch(harnessCodex, f.allowNonInteractive, args); err != nil {
			return err
		}
	}
	switch {
	case f.init_:
		return runCodexInit(f.yes, f.dryRun)
	case f.dev:
		effectiveInPlace := f.inPlace || f.noWorktree
		_ = f.baseBranch // reserved for slice-3+
		return launchCodexDev(f.resumeID, f.cleanup, f.dryRun, f.yolo, args, f.trackID, f.featureID, f.worktreePath, f.workItem, effectiveInPlace)
	case f.continue_:
		return launchCodexContinue(f.resumeID, f.yolo, args)
	default:
		effectiveInPlace := f.inPlace || f.noWorktree
		_ = f.baseBranch // reserved for slice-3+
		return launchCodexDefault(f.resumeID, f.trackID, f.featureID, f.worktreePath, f.workItem, effectiveInPlace, f.yolo, args)
	}
}

// registerCodexFlags registers the `wipnote codex` flags on cmd.
func registerCodexFlags(cmd *cobra.Command, f *codexFlags) {
	cmd.Flags().BoolVar(&f.init_, "init", false, "Install the wipnote Codex marketplace plugin (idempotent)")
	cmd.Flags().BoolVar(&f.continue_, "continue", false, "Resume the last Codex session")
	cmd.Flags().BoolVar(&f.dev, "dev", false, "Register local packages/codex-marketplace/ and launch Codex")
	cmd.Flags().BoolVar(&f.cleanup, "cleanup", false, "With --dev: unregister the local marketplace on exit")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Print what would happen without executing")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "Answer yes to all prompts (non-interactive)")
	cmd.Flags().BoolVar(&f.noWorktree, "no-worktree", false, "Skip worktree creation; run in project root (alias for --in-place)")
	cmd.Flags().BoolVar(&f.inPlace, "in-place", false, "Intentional in-place mutation; records opt-out of isolation")
	cmd.Flags().BoolVar(&f.yolo, "yolo", false, "Pass Codex --dangerously-bypass-approvals-and-sandbox")
	cmd.Flags().StringVar(&f.resumeID, "resume", "", "Resume a specific Codex session by ID")
	cmd.Flags().StringVar(&f.trackID, "track", "", "Track ID to work on (e.g., trk-3719d8f3)")
	cmd.Flags().StringVar(&f.featureID, "feature", "", "Feature ID to work on (e.g., feat-15c458aa)")
	cmd.Flags().StringVar(&f.worktreePath, "worktree", "", "Explicit worktree path (overrides --track/--feature resolution)")
	cmd.Flags().StringVar(&f.workItem, "work-item", "", "Work item ID for attribution prefix (e.g., feat-15c458aa)")
	cmd.Flags().StringVar(&f.baseBranch, "base", "", "Base branch for managed worktree (advanced; default: current HEAD)")
	cmd.Flags().BoolVar(&f.allowNonInteractive, allowNonInteractiveFlag, false,
		"Launch even when stdin is not a terminal (default: refuse; env "+allowNonTTYEnv+"=1 is equivalent)")
}

// codexInfoOnly reports whether the words forwarded to Codex only ask it to
// print its version or a help page.
//
// The version flag counts before the first subcommand or prompt word, and the
// help subcommand counts as the first such word. A flag's value is skipped.
func codexInfoOnly(args []string) bool {
	codex := passthroughLaunchers[harnessCodex]
	words := 0
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			return false
		case a == "--help" || a == "-h":
			return true
		case words == 0 && (a == "--version" || a == "-V"):
			return true
		case codex.valueFlags[a]:
			i++
		case !strings.HasPrefix(a, "-"):
			if words == 0 && a == "help" {
				return true
			}
			words++
		}
	}
	return false
}

// runCodexPlainFn runs codex with args and this process's terminal, and
// nothing else. It is a package-level var so tests can record the call.
var runCodexPlainFn = func(args []string) error {
	path, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex not found in PATH: %w", err)
	}
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

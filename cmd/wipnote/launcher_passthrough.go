package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// harnessWords describes a harness's own command line, as far as the rewrite
// needs it.
type harnessWords struct {
	// subcommands end wipnote's part of the line: every word from the first
	// one on belongs to the harness.
	subcommands map[string]bool
	// valueFlags take the next word as their value, so that word is never read
	// as a wipnote flag.
	valueFlags map[string]bool
}

// passthroughLaunchers are the launcher commands that forward every flag they
// do not define to the harness they start. The Codex words come from
// `codex --help` of codex-cli 0.172.0. A flag or subcommand missing here is
// still forwarded: only the two refinements above do not apply to it.
var passthroughLaunchers = map[string]harnessWords{
	"codex": {
		subcommands: wordSet("agents", "exec", "review", "login", "logout", "mcp", "plugin", "app-server",
			"remote-control", "app", "completion", "update", "doctor", "sandbox", "debug", "apply", "resume",
			"queue", "archive", "delete", "migrate-rollouts", "migrate-nfs-db", "unarchive", "fork", "cloud",
			"exec-server", "features", "help",
			// Codex's own aliases: e for exec, a for apply.
			"e", "a"),
		valueFlags: wordSet("-c", "--config", "--enable", "--disable", "--remote", "--remote-auth-token-env",
			"-i", "--image", "-m", "--model", "--local-provider", "-p", "--profile", "-s", "--sandbox",
			"-C", "--cd", "--add-dir", "-a", "--ask-for-approval"),
	},
}

// positionals returns the words in args that are neither flags nor the value
// of a flag: a prompt, or a subcommand and its arguments. It stops at "--",
// after which every word is positional.
func (h harnessWords) positionals(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			return append(out, args[i+1:]...)
		case h.valueFlags[a]:
			i++
		case !strings.HasPrefix(a, "-") || a == "-":
			out = append(out, a)
		}
	}
	return out
}

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

// forwardHarnessFlags rewrites a launcher command line so the flags wipnote
// does not define reach the harness. Cobra stops on an unknown flag, so
// `wipnote codex --sandbox read-only` used to fail before Codex started
// (bug-fc843d71). The rewrite keeps wipnote's own flags in front and moves
// every other word, in its original order, behind a "--":
//
//	wipnote codex --sandbox read-only --yolo  ->  wipnote codex --yolo -- --sandbox read-only
//
// A flag both sides define keeps wipnote's meaning, up to the first harness
// subcommand: `wipnote codex review --base main` gives --base to Codex. Words
// after a "--" the user typed always go to the harness. Other commands are
// returned unchanged.
func forwardHarnessFlags(root *cobra.Command, argv []string) []string {
	at := launcherIndex(argv)
	if at < 0 {
		return argv
	}
	cmd, _, err := root.Find(argv[at : at+1])
	if err != nil || cmd == root {
		return argv
	}
	own, fwd := splitLauncherWords(cmd, root, passthroughLaunchers[argv[at]], argv[at+1:])
	out := append(append([]string{}, argv[:at+1]...), own...)
	if len(fwd) > 0 {
		out = append(append(out, "--"), fwd...)
	}
	return out
}

// launcherIndex is the position of a pass-through launcher name when it is
// the command argv runs, or -1. Only --project-dir may come before it.
func launcherIndex(argv []string) int {
	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; {
		case a == "--project-dir":
			i++
		case strings.HasPrefix(a, "--project-dir="):
		case passthroughLaunchers[a].subcommands != nil:
			return i
		default:
			return -1
		}
	}
	return -1
}

// splitLauncherWords separates the words after the launcher name into the
// flags wipnote defines (with their values) and everything else.
func splitLauncherWords(cmd, root *cobra.Command, harness harnessWords, words []string) (own, fwd []string) {
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			// Harness words before the "--" make it the harness's own
			// separator: what follows is a prompt, even with a leading dash.
			// With none before it, it only ends wipnote's flags, as it
			// always did (`wipnote codex --in-place -- <codex words>`).
			if len(fwd) > 0 {
				fwd = append(fwd, w)
			}
			return own, append(fwd, words[i+1:]...)
		}
		if harness.subcommands[w] {
			return own, append(fwd, words[i:]...)
		}
		flag, inline := launcherFlag(cmd, root, w)
		switch {
		case w == "--help" || w == "-h":
			own = append(own, w)
		case flag != nil:
			own = append(own, w)
			// A flag with no default value takes the next word as its value.
			if !inline && flag.NoOptDefVal == "" && i+1 < len(words) {
				i++
				own = append(own, words[i])
			}
		default:
			fwd = append(fwd, w)
			if harness.valueFlags[w] && i+1 < len(words) {
				i++
				fwd = append(fwd, words[i])
			}
		}
	}
	return own, fwd
}

// launcherFlag returns the wipnote flag that word names, and whether the word
// carries its value inline (--flag=value). It returns nil for a word wipnote
// does not define.
func launcherFlag(cmd, root *cobra.Command, word string) (flag *pflag.Flag, inline bool) {
	if !strings.HasPrefix(word, "--") || len(word) < 3 {
		return nil, false
	}
	name, _, inline := strings.Cut(strings.TrimPrefix(word, "--"), "=")
	if f := cmd.Flags().Lookup(name); f != nil {
		return f, inline
	}
	return root.PersistentFlags().Lookup(name), inline
}

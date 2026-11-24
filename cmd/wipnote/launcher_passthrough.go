package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// passthroughLaunchers are the launcher commands that forward every flag they
// do not define to the harness they start.
var passthroughLaunchers = map[string]bool{"codex": true}

// forwardHarnessFlags rewrites a launcher command line so the flags wipnote
// does not define reach the harness. Cobra stops on an unknown flag, so
// `wipnote codex --sandbox read-only` used to fail before Codex started
// (bug-fc843d71). The rewrite keeps wipnote's own flags in front and moves
// every other word, in its original order, behind a "--":
//
//	wipnote codex --sandbox read-only --yolo  ->  wipnote codex --yolo -- --sandbox read-only
//
// A flag both sides define keeps wipnote's meaning. Words after a "--" the
// user typed always go to the harness. Other commands are returned unchanged.
func forwardHarnessFlags(root *cobra.Command, argv []string) []string {
	at := launcherIndex(argv)
	if at < 0 {
		return argv
	}
	cmd, _, err := root.Find(argv[at : at+1])
	if err != nil || cmd == root {
		return argv
	}
	own, fwd := splitLauncherWords(cmd, root, argv[at+1:])
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
		case passthroughLaunchers[a]:
			return i
		default:
			return -1
		}
	}
	return -1
}

// splitLauncherWords separates the words after the launcher name into the
// flags wipnote defines (with their values) and everything else.
func splitLauncherWords(cmd, root *cobra.Command, words []string) (own, fwd []string) {
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			return own, append(fwd, words[i+1:]...)
		}
		flag, inline := launcherFlag(cmd, root, w)
		switch {
		case w == "--help" || w == "-h":
			own = append(own, w)
		case flag == nil:
			fwd = append(fwd, w)
		default:
			own = append(own, w)
			// A flag with no default value takes the next word as its value.
			if !inline && flag.NoOptDefVal == "" && i+1 < len(words) {
				i++
				own = append(own, words[i])
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

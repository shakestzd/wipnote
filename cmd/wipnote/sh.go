package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// shOpts holds the parsed CLI flags for wipnote sh.
type shOpts struct {
	maxLines int
	noDedup  bool
	raw      bool
}

// shCmd returns the cobra command for `wipnote sh`.
func shCmd() *cobra.Command {
	var opts shOpts

	cmd := &cobra.Command{
		Use:   "sh [flags] <command>",
		Short: "Run a shell command with compression for verbose output",
		Long: `Run a shell command via bash -c, capture stdout+stderr (merged),
and apply compression: strip ANSI, drop progress bars, dedup consecutive
identical lines, and cap to max-lines. Exits with the child's exit code.

Designed for AI agents to reduce output verbosity without losing information.

Examples:
  wipnote sh "echo foo; echo foo; echo bar"
  wipnote sh --max-lines 10 "seq 1 100"
  wipnote sh --raw "some verbose command"`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSh(args[0], opts)
		},
	}

	cmd.Flags().IntVar(&opts.maxLines, "max-lines", 200, "Cap lines after compression (0 = unlimited)")
	cmd.Flags().BoolVar(&opts.noDedup, "no-dedup", false, "Skip consecutive-duplicate-line dedup")
	cmd.Flags().BoolVar(&opts.raw, "raw", false, "Bypass all compression")

	return cmd
}

// runSh executes the command via bash -c, captures output, applies compression,
// and exits with the child's exit code.
func runSh(command string, opts shOpts) error {
	cmd := exec.Command("bash", "-c", command)

	// Merge stderr into stdout
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output = output + stderr.String()
	}

	// Apply compression unless --raw
	if !opts.raw {
		output = compressOutput(output, opts.maxLines, opts.noDedup)
	}

	fmt.Print(output)

	// Exit with the child's exit code
	if exitErr, ok := err.(*exec.ExitError); ok {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "wipnote sh: %v\n", err)
		os.Exit(1)
	}
	return nil
}

var ansiEscapeRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// dropProgressBars removes progress bar lines that contain a carriage-return
// mid-string (i.e., \r is not the line terminator). Splits on \r and keeps
// only the final segment of each line.
func dropProgressBars(lines []string) []string {
	var result []string
	for _, line := range lines {
		// Check if line contains \r (carriage return mid-string)
		if strings.Contains(line, "\r") {
			// Split on \r and take the last segment
			parts := strings.Split(line, "\r")
			line = parts[len(parts)-1]
		}
		// Only add non-empty lines
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// dedupConsecutive collapses consecutive identical lines to a single line.
func dedupConsecutive(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	var result []string
	prev := ""
	for _, line := range lines {
		if line != prev {
			result = append(result, line)
			prev = line
		}
	}
	return result
}

// capLines truncates the lines to maxLines and appends a trailer if needed.
func capLines(lines []string, maxLines int) []string {
	if maxLines <= 0 {
		return lines
	}
	if len(lines) <= maxLines {
		return lines
	}
	truncated := len(lines) - maxLines
	result := lines[:maxLines]
	trailer := fmt.Sprintf("... %d lines truncated (run with --max-lines 0 or --raw to see all)", truncated)
	result = append(result, trailer)
	return result
}

// compressOutput applies the full compression pipeline to the output.
func compressOutput(output string, maxLines int, noDedup bool) string {
	// Split into lines (preserving trailing newline handling)
	lines := strings.Split(output, "\n")
	// Remove the last empty element if output ended with \n
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// 1. Strip ANSI from each line
	for i, line := range lines {
		lines[i] = stripANSI(line)
	}

	// 2. Drop progress bar lines (those with \r mid-string)
	lines = dropProgressBars(lines)

	// 3. Dedup consecutive identical lines (unless --no-dedup)
	if !noDedup {
		lines = dedupConsecutive(lines)
	}

	// 4. Cap to maxLines
	lines = capLines(lines, maxLines)

	// Reconstruct with newlines
	result := strings.Join(lines, "\n")
	if result != "" {
		result += "\n"
	}
	return result
}

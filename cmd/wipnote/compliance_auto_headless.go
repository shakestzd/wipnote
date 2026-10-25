package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// buildHeadlessArgs constructs the `claude` CLI argv for a headless
// compliance-grading invocation (see realHeadlessInvoker). It is split out
// from realHeadlessInvoker so the exact flag set and order can be asserted by
// tests without spawning a process — see compliance_auto_test.go's
// TestBuildHeadlessArgs_GoldenFlags (always-on) and
// TestBuildHeadlessArgs_ValidatedAgainstInstalledCLI (live, skipped when
// `claude` isn't on PATH).
//
// req.userPrompt is intentionally NOT included here: it is piped via stdin
// by the caller, not passed as an argument.
//
// NOTE: this used to pass "--system" (not a recognized flag — `claude -p
// --system ...` fails immediately with "error: unknown option '--system'")
// until bug-33348600's predecessor fix; every real (non-stubbed) invocation
// of this code path was failing before that correction. No existing test
// caught it because all compliance_auto_test.go coverage stubbed
// headlessInvoker rather than exercising realHeadlessInvoker's constructed
// argv against the actual claude binary. The tests added for bug-33348600
// close that hole.
func buildHeadlessArgs(req headlessRequest) []string {
	return []string{
		"-p",
		"--output-format", "json",
		"--model", req.model,
		"--effort", req.effort,
		"--max-budget-usd", fmt.Sprintf("%.2f", req.maxBudgetUSD),
		"--max-turns", strconv.Itoa(req.maxTurns),
		"--permission-mode", "dontAsk",
		"--allowedTools", "Read",
		"--system-prompt", req.systemPrompt,
	}
}

// claudeLeanFlags make a headless run lean: no CLAUDE.md, skills, plugins,
// hooks or MCP, and no saved session. --safe-mode keeps the person's login,
// unlike --bare, which accepts only an API key or apiKeyHelper.
var claudeLeanFlags = []string{"--safe-mode", "--no-session-persistence"}

// claudeHelpProbeTimeout bounds the `claude --help` capability probe, which
// runs before the grading call and its wall-clock timer exist.
const claudeHelpProbeTimeout = 10 * time.Second

// claudeHelpText returns `claude --help`, bounded by ctx and
// claudeHelpProbeTimeout; "" on failure or timeout. A package var so tests can
// stub it.
var claudeHelpText = func(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, claudeHelpProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--help")
	// A wrapper script's children can hold the output pipe open after the
	// kill; stop waiting for them shortly after.
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

var (
	supportedLeanFlagsOnce sync.Once
	supportedLeanFlags     []string
)

// supportedClaudeLeanFlags returns the claudeLeanFlags this claude install
// lists in --help. --safe-mode is new in Claude Code 2.1.302 and an older CLI
// rejects an unknown flag, so each one is added only when it is supported:
// an older install still grades, just without the lean flags. A probe that
// fails or times out fails open to the base flags.
func supportedClaudeLeanFlags(ctx context.Context) []string {
	supportedLeanFlagsOnce.Do(func() {
		help := claudeHelpText(ctx)
		for _, f := range claudeLeanFlags {
			if strings.Contains(help, f) {
				supportedLeanFlags = append(supportedLeanFlags, f)
			}
		}
	})
	return supportedLeanFlags
}

// realHeadlessInvoker spawns `claude -p` with the given request parameters,
// captures stdout/stderr into buffers, honors context cancellation, and
// self-enforces the wall-clock timeout via time.AfterFunc.
//
// Stderr is never leaked to the caller's stderr — it is captured into a buffer.
// WIPNOTE_HEADLESS=1 is set in the spawned process env so any wipnote hook
// that still runs exits at once (headlessHookEnv).
//
// Bare mode (bug-48b023e5): --bare also skips discovery but accepts only
// ANTHROPIC_API_KEY or apiKeyHelper, so it would break subscription logins.
// --safe-mode (Claude Code 2.1.302) gives the same lean discovery and keeps
// OAuth, so it is used instead (supportedClaudeLeanFlags).
func realHeadlessInvoker(ctx context.Context, req headlessRequest) (*headlessResult, error) {
	args := append(buildHeadlessArgs(req), supportedClaudeLeanFlags(ctx)...)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(req.userPrompt)
	// After a timeout kill, do not wait on children that still hold the pipes.
	cmd.WaitDelay = time.Second

	// Capture stdout and stderr; never leak to caller.
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	cmd.Env = append(os.Environ(), headlessHookEnv+"=1")

	// Self-enforced wall-clock timeout.
	var timedOut bool
	var timedOutMu sync.Mutex
	timer := time.AfterFunc(req.maxWallClock, func() {
		timedOutMu.Lock()
		timedOut = true
		timedOutMu.Unlock()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	defer timer.Stop()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	waitErr := cmd.Wait()

	timedOutMu.Lock()
	wasTimedOut := timedOut
	timedOutMu.Unlock()

	if wasTimedOut {
		return nil, fmt.Errorf("timeout: wall-clock limit %s exceeded", req.maxWallClock)
	}

	if waitErr != nil {
		return nil, headlessExitError(waitErr, stdoutBuf.String(), stderrBuf.String())
	}

	// Parse the JSON response from claude.
	return parseClaudeOutput(stdoutBuf.Bytes())
}

// headlessExitError classifies a failed claude exit: a budget-exceeded
// error when either stream reports error_max_budget_usd, else the exit error
// with the stderr tail.
func headlessExitError(waitErr error, stdoutContent, stderrContent string) error {
	// Check stderr for budget exceeded error.
	if strings.Contains(stderrContent, "error_max_budget_usd") ||
		strings.Contains(stdoutContent, "error_max_budget_usd") {
		return &BudgetExceededError{msg: "max_budget_usd exceeded"}
	}
	return fmt.Errorf("claude exited with error: %w; stderr: %s", waitErr, truncateStr(stderrContent, 500))
}

// claudeOutputJSON is the structure of claude's --output-format json response.
type claudeOutputJSON struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Result  string          `json:"result"`
	CostUSD float64         `json:"total_cost_usd"`
	Error   json.RawMessage `json:"error"`
}

// parseClaudeOutput parses the JSON output from `claude -p --output-format json`.
func parseClaudeOutput(data []byte) (*headlessResult, error) {
	var out claudeOutputJSON
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse claude output JSON: %w; raw: %s", err, truncateStr(string(data), 300))
	}

	if out.Subtype == "error_max_budget_usd" {
		return nil, &BudgetExceededError{msg: "max_budget_usd exceeded"}
	}

	return &headlessResult{
		text:    out.Result,
		costUSD: out.CostUSD,
	}, nil
}

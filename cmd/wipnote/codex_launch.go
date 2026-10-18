package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/shakestzd/wipnote/internal/launcher"
	"github.com/shakestzd/wipnote/observe/otel/collector"
)

type codexLaunchMode string

// codexSandboxProbe exercises the same user-namespace path that Codex's
// bubblewrap sandbox requires. Codex uses --unshare-user (and --unshare-pid,
// --unshare-net) plus --ro-bind / / to create its sandbox environment. In
// Codespaces/devcontainers the kernel capabilities required to create user
// namespaces are absent, so this probe fails with "operation not permitted"
// or similar. We probe bwrap directly (not codex --help, which never exercises
// bwrap) so detection reliably fires on the real failure path.
//
// The probe resolves the bwrap binary via exec.LookPath. If bwrap is not on
// PATH at all, the probe returns ("bwrap not found", non-nil error) so the
// caller treats absence as "unavailable" — the same degraded outcome.
//
// The probe is a package-level var so tests can replace it without running
// a real bwrap process.
var codexSandboxProbe = func(_ string) ([]byte, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		// bwrap binary absent from PATH → treat as unavailable.
		return []byte("bwrap not found on PATH"), err
	}
	// Minimal invocation matching Codex's namespace-isolation requirement:
	// --unshare-user exercises the user-namespace path that fails in
	// Codespaces; --ro-bind / / provides the read-only root Codex requires;
	// `true` is the no-op command so the process exits immediately.
	return exec.Command(bwrapPath, "--unshare-user", "--ro-bind", "/", "/", "true").CombinedOutput()
}

const (
	codexLaunchModeDefault  codexLaunchMode = "default"
	codexLaunchModeDev      codexLaunchMode = "dev"
	codexLaunchModeContinue codexLaunchMode = "continue"
	codexLaunchModeYolo     codexLaunchMode = "yolo"
	codexLaunchModeYoloDev  codexLaunchMode = "yolo-dev"
	codexLaunchModeYoloCont codexLaunchMode = "yolo-continue"
	codexLaunchModeInit     codexLaunchMode = "init"
)

// appendOrReplaceEnv takes an env slice (KEY=VALUE strings) and one or more
// kv pairs in "KEY=VALUE" form. For each kv, if the key already exists in env
// its entry is replaced in-place; otherwise the kv is appended. The original
// slice is modified and returned. Order of existing entries is preserved.
func appendOrReplaceEnv(env []string, kv ...string) []string {
	return launcher.AppendOrReplaceEnv(env, kv...)
}

// spawnCodexOtelCollector spawns a per-session OTel collector and returns the
// port, session ID, and a cleanup function. On failure it writes a warning to
// stderr and returns zero port / nil cleanup so the caller can proceed without
// telemetry. Exits non-zero when WIPNOTE_OTEL_STRICT=1 and spawn fails.
func spawnCodexOtelCollector(projectDir string) (port int, sessionID string, cleanup func()) {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wipnote: warning: codex per-session collector skipped: %v\n", err)
		return 0, "", nil
	}

	sessionID = generateOtelSessionID()
	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr:     os.Stderr,
		StrictMode: os.Getenv("WIPNOTE_OTEL_STRICT") == "1",
	})

	spawnedPort, spawnCleanup, spawnErr := lc.Spawn(binPath, sessionID, projectDir)
	if spawnErr != nil {
		fmt.Fprintf(os.Stderr, "wipnote: FATAL: codex collector spawn failed: %v\n", spawnErr)
		if os.Getenv("WIPNOTE_OTEL_STRICT") == "1" {
			os.Exit(1)
		}
		return 0, "", nil
	}

	return spawnedPort, sessionID, spawnCleanup
}

// buildCodexOtelEnv returns a copy of base with OTel exporter variables set
// for the Codex CLI child process. port and sessionID come from
// spawnCodexOtelCollector; when port is 0 the base env is returned unchanged.
// Env var assembly is delegated to the harness registry to avoid hardcoding.
func buildCodexOtelEnv(base []string, port int, sessionID string) []string {
	return launcher.BuildHarnessOtelEnv(base, "codex", port, sessionID)
}

func buildCodexAgentEnv(base []string) []string {
	return launcher.BuildHarnessAgentEnv(base, "codex")
}

// applySandboxDegradedEnv sets WIPNOTE_CODEX_SANDBOX=degraded in env when
// degraded is true, signalling that the bwrap sandbox is unavailable and
// agents should stop retrying nested codex exec / sandbox paths. It is a
// no-op when degraded is false, so normal and user-overridden launches are
// not polluted.
func applySandboxDegradedEnv(env []string, degraded bool) []string {
	if !degraded {
		return env
	}
	return appendOrReplaceEnv(env, "WIPNOTE_CODEX_SANDBOX=degraded")
}

func resolveCodexSandboxMode(codexPath string, opts codexLaunchOpts, devcontainer bool) (mode string, notice string) {
	if !devcontainer || opts.Yolo || codexRequestedSandbox(opts.ExtraArgs) != "" || codexBypassesSandbox(opts.ExtraArgs) {
		return "", ""
	}

	out, err := codexSandboxProbe(codexPath)
	if err == nil {
		return "", ""
	}
	if !isCodexBubblewrapFailure(out, err) {
		return "", ""
	}

	return "danger-full-access",
		"wipnote: Codex bubblewrap sandbox is unavailable in this devcontainer/Codespace; launching with `--sandbox danger-full-access` so tools do not fail one by one. Approvals remain enabled. Override with `wipnote codex --sandbox <mode>` or use `--yolo` only when you intentionally want to bypass approvals too."
}

func codexRequestedSandbox(args []string) string {
	for i, arg := range args {
		if arg == "--sandbox" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "--sandbox=") {
			return strings.TrimPrefix(arg, "--sandbox=")
		}
	}
	return ""
}

func codexBypassesSandbox(args []string) bool {
	for _, arg := range args {
		if arg == "--dangerously-bypass-approvals-and-sandbox" {
			return true
		}
	}
	return false
}

// namespaceContextWords are terms that indicate a failure is specifically
// related to Linux namespace/bubblewrap setup rather than an unrelated EPERM.
var namespaceContextWords = []string{
	"bwrap", "bubblewrap", "namespace", "unshare",
}

func isCodexBubblewrapFailure(out []byte, err error) bool {
	text := strings.ToLower(string(out))
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}

	// Direct bwrap/namespace error indicators — unambiguous on their own.
	if strings.Contains(text, "bwrap") ||
		strings.Contains(text, "bubblewrap") ||
		strings.Contains(text, "failed to make / slave") ||
		strings.Contains(text, "cannot create namespace") {
		return true
	}

	// "operation not permitted" (EPERM) is too broad to match alone: many
	// unrelated failures surface the same errno string (e.g. file-permission
	// errors, capability drops). Only count it as a bwrap failure when it
	// co-occurs with a namespace/unshare/bwrap context word, indicating the
	// EPERM arose during namespace setup rather than from something else.
	if strings.Contains(text, "operation not permitted") {
		for _, word := range namespaceContextWords {
			if strings.Contains(text, word) {
				return true
			}
		}
	}

	return false
}

func buildCodexOtelConfigArgs(port int) []string {
	if port == 0 {
		return nil
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	return []string{
		"-c", "otel.log_user_prompt=true",
		"-c", fmt.Sprintf(`otel.exporter={ otlp-http = { endpoint = "%s/v1/logs", protocol = "binary" } }`, base),
		"-c", fmt.Sprintf(`otel.trace_exporter={ otlp-http = { endpoint = "%s/v1/traces", protocol = "binary" } }`, base),
		"-c", fmt.Sprintf(`otel.metrics_exporter={ otlp-http = { endpoint = "%s/v1/metrics", protocol = "binary" } }`, base),
	}
}

func (opts codexLaunchOpts) effectiveMode() codexLaunchMode {
	mode := opts.Mode
	if mode == "" {
		mode = codexLaunchModeDefault
	}
	if mode == codexLaunchModeDefault && (opts.ResumeLast || opts.ResumeID != "") {
		mode = codexLaunchModeContinue
	}
	if opts.Yolo {
		if mode == codexLaunchModeDev {
			return codexLaunchModeYoloDev
		}
		if mode == codexLaunchModeContinue {
			return codexLaunchModeYoloCont
		}
		return codexLaunchModeYolo
	}
	return mode
}

// buildCodexInstructionConfigArgs returns the `-c developer_instructions=...`
// override that carries the wipnote orchestrator prompt for this launch mode.
//
// Why developer_instructions and not model_instructions_file: the latter
// REPLACES Codex's built-in base prompt, which forced wipnote to shell out to
// `codex debug models` on every launch, copy the model's base_instructions
// into a temp file and append our addendum — pinning a stale copy of the
// upstream prompt. `developer_instructions` is a top-level config key
// (codex-rs/config/src/config_toml.rs:252-254) that Codex injects as a
// separate developer-role message (codex-rs/core/src/session/mod.rs:4495-4499)
// and leaves the built-in prompt intact. Codex has no
// `developer_instructions_file` key, so the text is passed inline.
//
// The key is a single string and a `-c` session flag outranks the user and
// project config layers, so the override would silently discard any
// developer_instructions the user configured. existing (see
// configuredCodexDeveloperInstructions) is therefore kept, ahead of ours.
func buildCodexInstructionConfigArgs(mode codexLaunchMode, existing string) []string {
	addendum := codexInstructionAddendum(mode)
	if addendum == "" {
		return nil
	}
	// Append the shared research-routing disposition exactly once to the final
	// composed instruction string (not per switch branch, so combined
	// system+yolo modes don't double-append). Source of truth:
	// cmd/wipnote/prompts/research-routing.md.
	text := fmt.Sprintf("# wipnote %s Instructions\n\n%s\n\n%s",
		codexInstructionModeTitle(mode),
		strings.TrimSpace(addendum),
		strings.TrimSpace(researchRoutingContent))
	if existing = strings.TrimSpace(existing); existing != "" {
		text = existing + "\n\n" + text
	}
	return []string{"-c", "developer_instructions=" + tomlBasicString(text)}
}

// codexHomeConfigPath is $CODEX_HOME/config.toml, falling back to
// ~/.codex/config.toml, which is where Codex reads its user layer.
func codexHomeConfigPath() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	return codexConfigPath()
}

// codexSystemConfigPath is Codex's system config layer (Unix).
const codexSystemConfigPath = "/etc/codex/config.toml"

// codexProfileFromArgs returns the profile selected by -p/--profile in the
// args forwarded to codex, or "".
func codexProfileFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case (a == "-p" || a == "--profile") && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--profile="):
			return strings.TrimPrefix(a, "--profile=")
		}
	}
	return ""
}

// codexInstructionLayers are the config files configuredCodexDeveloperInstructions
// reads, lowest precedence first.
type codexInstructionLayers struct {
	System, User string
	LaunchDir    string // its .codex/config.toml counts only when trusted
	Profile      string // from the command line; else the config's `profile`
}

// configuredCodexDeveloperInstructions returns the developer_instructions the
// launch would see without wipnote's -c override, following Codex's layering
// (codex-rs/config/src/loader): system, then user ($CODEX_HOME), then the
// project layer, then the active profile. The project layer counts only when
// the user config marks that exact directory trusted: Codex loads an
// untrusted project layer disabled, and re-injecting it through -c would
// bypass that check. Unreadable or unparsable files count as unset.
func configuredCodexDeveloperInstructions(l codexInstructionLayers) string {
	type profileConfig struct {
		DeveloperInstructions string `toml:"developer_instructions"`
	}
	type layerConfig struct {
		DeveloperInstructions string                   `toml:"developer_instructions"`
		Profile               string                   `toml:"profile"`
		Profiles              map[string]profileConfig `toml:"profiles"`
		Projects              map[string]struct {
			TrustLevel string `toml:"trust_level"`
		} `toml:"projects"`
	}
	read := func(path string) layerConfig {
		var cfg layerConfig
		if path == "" {
			return cfg
		}
		if data, err := os.ReadFile(path); err == nil {
			_ = toml.Unmarshal(data, &cfg)
		}
		return cfg
	}

	value := ""
	set := func(v string) {
		if v = strings.TrimSpace(v); v != "" {
			value = v
		}
	}
	system, user := read(l.System), read(l.User)
	set(system.DeveloperInstructions)
	set(user.DeveloperInstructions)

	if l.LaunchDir != "" {
		if abs, err := filepath.Abs(l.LaunchDir); err == nil && user.Projects[abs].TrustLevel == "trusted" {
			set(read(filepath.Join(abs, ".codex", "config.toml")).DeveloperInstructions)
		}
	}

	profile := l.Profile
	if profile == "" {
		profile = user.Profile
	}
	if profile == "" {
		profile = system.Profile
	}
	if profile != "" {
		set(system.Profiles[profile].DeveloperInstructions)
		set(user.Profiles[profile].DeveloperInstructions)
	}
	return value
}

// tomlBasicString encodes s as a single-line TOML basic string. Codex parses
// each `-c key=value` value as TOML (codex-rs/utils/cli/src/config_override.rs
// parse_overrides wraps it as `_x_ = <value>`) and, when that parse fails,
// silently falls back to the raw text with surrounding quotes trimmed. A
// multi-line prompt must therefore be escaped into one valid basic string or
// it would be mangled with no error. Go's %q is not used because some of its
// escapes (\a, \v, \xNN) are not valid TOML.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			// TOML forbids raw control characters (U+0000-U+001F, U+007F)
			// inside basic strings; everything else passes through as UTF-8.
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func codexInstructionAddendum(mode codexLaunchMode) string {
	switch mode {
	case codexLaunchModeDev:
		return strings.TrimSpace(systemPromptContent) + "\n\n" + strings.TrimSpace(codexDevInstructions)
	case codexLaunchModeContinue:
		return strings.TrimSpace(systemPromptContent) + "\n\n" + strings.TrimSpace(codexContinueInstructions)
	case codexLaunchModeYolo:
		return strings.TrimSpace(yoloPromptContent) + "\n\n" + strings.TrimSpace(codexYoloInstructions)
	case codexLaunchModeYoloDev:
		return strings.TrimSpace(yoloPromptContent) + "\n\n" + strings.TrimSpace(codexDevInstructions) + "\n\n" + strings.TrimSpace(codexYoloInstructions)
	case codexLaunchModeYoloCont:
		return strings.TrimSpace(yoloPromptContent) + "\n\n" + strings.TrimSpace(codexContinueInstructions) + "\n\n" + strings.TrimSpace(codexYoloInstructions)
	case codexLaunchModeInit:
		return strings.TrimSpace(codexInitInstructions)
	default:
		return strings.TrimSpace(systemPromptContent)
	}
}

const codexDevInstructions = `## Codex Dev Mode

This session was launched with ` + "`wipnote codex --dev`" + `.

- Treat ` + "`packages/codex-marketplace/`" + ` as a generated local plugin cache for Codex testing.
- Prefer editing the source of truth: ` + "`packages/plugin-core/manifest.json`" + ` and shared plugin assets under ` + "`plugin/`" + `.
- After plugin asset or manifest changes, rebuild generated ports with ` + "`wipnote plugin build-ports`" + ` before validating Codex behavior.
- Keep marketplace, plugin cache, and hook setup changes separate from product-code changes when possible.`

const codexContinueInstructions = `## Codex Continue Mode

This session is resuming an existing Codex conversation.

- Preserve the resumed session's prior intent and active work item unless the user explicitly redirects.
- Before starting new work, recover current context from the conversation, ` + "`wipnote status`" + `, and the active work item hints injected by hooks.
- Do not recreate setup, duplicate work items, or restart already-completed tasks just because the launcher resumed the session.`

const codexYoloInstructions = `## Codex YOLO Mode

This session was launched with Codex approvals and sandbox prompts bypassed.

- Permission prompts are disabled; self-enforce research, tests, quality gates, and diff review.
- Do not interpret bypass mode as permission to skip work attribution, validation, or careful scoping.
- Stop and report clearly if the task exceeds the current work item scope or would require destructive git operations.`

const codexInitInstructions = `## Codex Init Mode

This mode is setup-only. It installs or repairs the wipnote Codex plugin, hook configuration, and local plugin cache.

- Do not perform product development as part of init setup.
- After setup, start a separate ` + "`wipnote codex`" + `, ` + "`wipnote codex --dev`" + `, or ` + "`wipnote codex --continue`" + ` session for actual work.`

func codexInstructionModeTitle(mode codexLaunchMode) string {
	switch mode {
	case codexLaunchModeDev:
		return "Dev"
	case codexLaunchModeContinue:
		return "Continue"
	case codexLaunchModeYolo:
		return "YOLO"
	case codexLaunchModeYoloDev:
		return "YOLO Dev"
	case codexLaunchModeYoloCont:
		return "YOLO Continue"
	case codexLaunchModeInit:
		return "Init"
	default:
		return "Orchestrator"
	}
}

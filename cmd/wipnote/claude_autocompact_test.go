package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func envWith(k string) func(string) string {
	return func(name string) string {
		if name == k {
			return "1"
		}
		return ""
	}
}

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func countFlag(args []string) int {
	n := 0
	for _, a := range args {
		if a == "--autocompact" || strings.HasPrefix(a, "--autocompact=") {
			n++
		}
	}
	return n
}

func TestAutoCompactDefaultAddsFlagOnce(t *testing.T) {
	var warn bytes.Buffer
	got := withAutoCompact([]string{"--x"}, noEnv, "", &warn)
	if !slices.Equal(got, []string{"--autocompact", "300k", "--x"}) {
		t.Fatalf("got %v", got)
	}
	if countFlag(got) != 1 || warn.Len() != 0 {
		t.Fatalf("flag count %d, warn %q", countFlag(got), warn.String())
	}
}

func TestAutoCompactConfigOverrides(t *testing.T) {
	for body, want := range map[string]string{
		`{"launch":{"claude":{"autoCompactWindow":"400k"}}}`: "400k",
		`{"launch":{"claude":{"autoCompactWindow":400000}}}`: "400000",
		`{"launch":{"claude":{"autoCompactWindow":"1m"}}}`:   "1m",
	} {
		var warn bytes.Buffer
		got := withAutoCompact(nil, noEnv, writeCfg(t, body), &warn)
		if !slices.Equal(got, []string{"--autocompact", want}) || warn.Len() != 0 {
			t.Errorf("%s: got %v warn %q", body, got, warn.String())
		}
	}
}

func TestAutoCompactAutoAndOffSuppress(t *testing.T) {
	for _, v := range []string{"auto", "off", "OFF"} {
		var warn bytes.Buffer
		cfg := writeCfg(t, `{"launch":{"claude":{"autoCompactWindow":"`+v+`"}}}`)
		got := withAutoCompact([]string{"--x"}, noEnv, cfg, &warn)
		if !slices.Equal(got, []string{"--x"}) || warn.Len() != 0 {
			t.Errorf("%s: got %v warn %q", v, got, warn.String())
		}
	}
}

func TestAutoCompactInvalidFallsBackAndWarnsOnce(t *testing.T) {
	for _, v := range []string{`"banana"`, `"50k"`, `"2m"`, `99999`, `1000001`, `true`, `""`} {
		var warn bytes.Buffer
		cfg := writeCfg(t, `{"launch":{"claude":{"autoCompactWindow":`+v+`}}}`)
		got := withAutoCompact(nil, noEnv, cfg, &warn)
		if !slices.Equal(got, []string{"--autocompact", "300k"}) {
			t.Errorf("%s: got %v", v, got)
		}
		if strings.Count(warn.String(), "\n") != 1 || !strings.Contains(warn.String(), "autoCompactWindow") {
			t.Errorf("%s: warn %q", v, warn.String())
		}
	}
}

func TestAutoCompactMissingOrMalformedConfigUsesDefault(t *testing.T) {
	for _, cfg := range []string{"", filepath.Join(t.TempDir(), "nope.json"), writeCfg(t, "{not json")} {
		var warn bytes.Buffer
		got := withAutoCompact(nil, noEnv, cfg, &warn)
		if !slices.Equal(got, []string{"--autocompact", "300k"}) || warn.Len() != 0 {
			t.Errorf("cfg %q: got %v warn %q", cfg, got, warn.String())
		}
	}
}

func TestAutoCompactUserFlagWins(t *testing.T) {
	for _, extra := range [][]string{
		{"--autocompact", "500k"},
		{"--autocompact=500k", "--x"},
		{"--x", "--autocompact", "auto"},
	} {
		got := withAutoCompact(extra, noEnv, "", &bytes.Buffer{})
		if !slices.Equal(got, extra) || countFlag(got) != 1 {
			t.Errorf("extra %v: got %v", extra, got)
		}
	}
}

func TestAutoCompactEnvSuppresses(t *testing.T) {
	for _, k := range []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "DISABLE_AUTO_COMPACT", "DISABLE_COMPACT"} {
		got := withAutoCompact([]string{"--x"}, envWith(k), "", &bytes.Buffer{})
		if !slices.Equal(got, []string{"--x"}) {
			t.Errorf("%s: got %v", k, got)
		}
	}
}

// Resume and continue paths build their argv through buildClaudeArgs from
// opts.ExtraArgs, so the flag rides along with --resume.
func TestAutoCompactReachesResumeAndContinueArgv(t *testing.T) {
	for _, opts := range []LaunchOpts{{ResumeID: "sid"}, {Resume: true}, {}} {
		opts.ExtraArgs = withAutoCompact(opts.ExtraArgs, noEnv, "", &bytes.Buffer{})
		args := buildClaudeArgs(opts, "")
		if countFlag(args) != 1 || !slices.Contains(args, "300k") {
			t.Errorf("opts %+v: argv %v", opts, args)
		}
	}
}

func TestCodexArgvHasNoAutoCompact(t *testing.T) {
	// The injection lives in launchClaude only; the Codex builders never call it.
	src, err := os.ReadFile("codex_launch.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "withAutoCompact") || strings.Contains(string(src), "autocompact") {
		t.Fatal("codex launcher must not reference the Claude auto-compact flag")
	}
}

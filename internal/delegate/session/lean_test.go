package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ctxBG = context.Background()

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func settingsOf(t *testing.T, flags []string) map[string]any {
	t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(argAfter(flags, "--settings")), &s); err != nil {
		t.Fatalf("settings are not JSON: %v", err)
	}
	return s
}

func TestLeanFlags_Defaults(t *testing.T) {
	got := LeanFlags(nil, LeanSpec{Home: "/home/u"})
	for _, f := range []string{"--strict-mcp-config", "--no-chrome"} {
		if !hasFlag(got, f) {
			t.Errorf("missing %s in %q", f, got)
		}
	}
	if argAfter(got, "--tools") != "Bash,Read,Edit,Write,Grep,Glob" {
		t.Errorf("--tools = %q: want one comma-joined value", argAfter(got, "--tools"))
	}
	s := settingsOf(t, got)
	if ex := s["claudeMdExcludes"].([]any); len(ex) != 1 || ex[0] != "/home/u/.claude/CLAUDE.md" {
		t.Errorf("claudeMdExcludes = %v", ex)
	}
	for _, k := range []string{"autoMemoryEnabled", "syncClaudeAiPlugins", "remoteControlAtStartup", "promptSuggestionEnabled", "awaySummaryEnabled", "idleCompaction", "includeGitInstructions"} {
		if s[k] != false {
			t.Errorf("%s = %v, want false", k, s[k])
		}
	}
	if s["outputStyle"] != "default" {
		t.Errorf("outputStyle = %v", s["outputStyle"])
	}
}

func TestLeanFlags_HomeIsJSONEncoded(t *testing.T) {
	home := `/Users/we"ird\name`
	var s struct {
		Excludes []string `json:"claudeMdExcludes"`
	}
	if err := json.Unmarshal([]byte(argAfter(LeanFlags(nil, LeanSpec{Home: home}), "--settings")), &s); err != nil {
		t.Fatalf("a quote or backslash in home broke the JSON: %v", err)
	}
	if want := home + "/.claude/CLAUDE.md"; len(s.Excludes) != 1 || s.Excludes[0] != want {
		t.Fatalf("excludes = %q, want %q", s.Excludes, want)
	}
	for _, spec := range []LeanSpec{{}, {Home: "/h", UserRules: true}} {
		if strings.Contains(argAfter(LeanFlags(nil, spec), "--settings"), "claudeMdExcludes") {
			t.Errorf("%+v: wrote an exclude", spec)
		}
	}
}

func TestLeanFlags_UserFlagsReplaceDefaults(t *testing.T) {
	cases := []struct {
		user []string
		skip string
	}{
		{[]string{"--tools", "Bash"}, "--tools"},
		{[]string{"--tools=Bash"}, "--tools"},
		{[]string{"--chrome"}, "--no-chrome"},
		{[]string{"--settings", "s.json"}, "--settings"},
		{[]string{"--strict-mcp-config"}, "--strict-mcp-config"},
	}
	for _, tc := range cases {
		if got := LeanFlags(tc.user, LeanSpec{Home: "/h"}); hasFlag(got, tc.skip) {
			t.Errorf("user %q: still passes %s: %q", tc.user, tc.skip, got)
		}
	}
	if got := LeanFlags([]string{"--tools", "Bash"}, LeanSpec{Home: "/h"}); !hasFlag(got, "--settings") || !hasFlag(got, "--no-chrome") {
		t.Errorf("one override dropped the others: %q", got)
	}
}

func TestLeanFlags_ToolSearchWithAnyMCPAndSkillWithPlugins(t *testing.T) {
	if got := argAfter(LeanFlags(nil, LeanSpec{MCP: true, Tools: []string{"WebFetch", "Grep"}}), "--tools"); got != "Bash,Read,Edit,Write,Grep,Glob,WebFetch,ToolSearch" {
		t.Errorf("--tools = %q", got)
	}
	got := LeanFlags(nil, LeanSpec{Plugins: map[string]bool{"a@m": true}})
	if argAfter(got, "--tools") != "Bash,Read,Edit,Write,Grep,Glob,Skill" {
		t.Errorf("--tools = %q", argAfter(got, "--tools"))
	}
	if p := settingsOf(t, got)["enabledPlugins"].(map[string]any); p["a@m"] != true {
		t.Errorf("enabledPlugins = %v", p)
	}
}

func TestLeanFlags_ConnectorsDropStrictAndDenyOthersByName(t *testing.T) {
	got := LeanFlags(nil, LeanSpec{Open: true, MCP: true, Deny: []string{"claude.ai Gmail", "MCP_DOCKER"}})
	if hasFlag(got, "--strict-mcp-config") {
		t.Errorf("strict kept: %q", got)
	}
	deny := settingsOf(t, got)["deniedMcpServers"].([]any)
	if len(deny) != 2 || deny[0].(map[string]any)["serverName"] != "claude.ai Gmail" {
		t.Errorf("deniedMcpServers = %v", deny)
	}
	if strings.Contains(argAfter(got, "--settings"), "allowedMcpServers") {
		t.Error("allowedMcpServers removes every connector; never use it")
	}
	if !strings.Contains(argAfter(got, "--tools"), "ToolSearch") {
		t.Error("connectors without ToolSearch load every tool in full")
	}
}

func TestHarnessFlags_ProfilesAndOverrides(t *testing.T) {
	cases := []struct {
		name          string
		o             SpawnOptions
		model, effort string
	}{
		{"default is standard", SpawnOptions{Harness: HarnessClaude}, "sonnet", "medium"},
		{"simple", SpawnOptions{Harness: HarnessClaude, Profile: "simple"}, "haiku", "low"},
		{"deep", SpawnOptions{Harness: HarnessClaude, Profile: "deep"}, "opus", "high"},
		{"flags override the profile", SpawnOptions{Harness: HarnessClaude, Profile: "deep", Model: "haiku", Effort: "low"}, "haiku", "low"},
		{"a raw model after -- wins", SpawnOptions{Harness: HarnessClaude, Extra: []string{"--model", "opus"}}, "opus", "medium"},
		{"a raw effort after -- wins", SpawnOptions{Harness: HarnessClaude, Extra: []string{"--effort", "max"}}, "sonnet", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.o
			if err := harnessFlags(ctxBG, &o); err != nil {
				t.Fatal(err)
			}
			if o.Model != tc.model || o.Effort != tc.effort || !o.Lean || !hasFlag(o.Flags, "--tools") {
				t.Fatalf("model %q effort %q lean %v flags %q", o.Model, o.Effort, o.Lean, o.Flags)
			}
			if n := strings.Count(strings.Join(o.Flags, " "), "--effort"); n > 1 {
				t.Fatalf("--effort twice: %q", o.Flags)
			}
		})
	}
}

func TestHarnessFlags_UserConfigProfilesOverrideBuiltIns(t *testing.T) {
	cfg := ClaudeConfig{DefaultProfile: "mine", Profiles: map[string]Profile{
		"mine":   {Model: "opus", Effort: "low", Tools: []string{"Grep"}, UserRules: true},
		"simple": {Model: "sonnet"},
	}}
	o := SpawnOptions{Harness: HarnessClaude, Config: cfg, Tools: []string{"WebFetch"}}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if o.Profile != "mine" || o.Model != "opus" || !o.UserRules || argAfter(o.Flags, "--tools") != "Bash,Read,Edit,Write,Grep,Glob,WebFetch" {
		t.Fatalf("%+v", o)
	}
	o = SpawnOptions{Harness: HarnessClaude, Config: cfg, Profile: "simple"}
	if err := harnessFlags(ctxBG, &o); err != nil || o.Model != "sonnet" {
		t.Fatalf("a user profile must override the built-in: %v %q", err, o.Model)
	}
}

func TestHarnessFlags_UnknownProfileRefusedWithValidNames(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, Profile: "huge"}
	err := harnessFlags(ctxBG, &o)
	if !errors.Is(err, ErrBadSetup) || !strings.Contains(err.Error(), "simple, standard") {
		t.Fatalf("err = %v", err)
	}
}

func TestHarnessFlags_FullKeepsToday(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Flags) != 0 || o.Model != "" || o.Lean || !o.Full {
		t.Fatalf("flags %q model %q lean %v", o.Flags, o.Model, o.Lean)
	}
}

func TestHarnessFlags_CodexRefusesClaudeSetup(t *testing.T) {
	for _, o := range []SpawnOptions{
		{Harness: HarnessCodex, Profile: "simple"}, {Harness: HarnessCodex, MCP: []string{"x"}},
		{Harness: HarnessCodex, Plugins: []string{"x"}}, {Harness: HarnessCodex, Tools: []string{"Grep"}},
		{Harness: HarnessCodex, UserRules: true},
	} {
		if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) {
			t.Errorf("%+v: err = %v", o, err)
		}
	}
	o := SpawnOptions{Harness: HarnessCodex, Model: "gpt-x"}
	if err := harnessFlags(ctxBG, &o); err != nil || len(o.Flags) != 2 || o.Lean {
		t.Fatalf("codex changed: %v %q", err, o.Flags)
	}
}

func TestHarnessFlags_UsersMCPConfigStaysBesideStrict(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, Extra: []string{"--mcp-config", "m.json"}}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if argAfter(o.Flags, "--mcp-config") != "m.json" || !hasFlag(o.Flags, "--strict-mcp-config") {
		t.Fatalf("flags = %q", o.Flags)
	}
}

func TestHarnessFlags_ResumeThatChoosesNothingKeepsTheOldStart(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, inherited: true}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if len(o.Flags) != 0 || o.Model != "" {
		t.Fatalf("an old full run was made lean: %q %q", o.Flags, o.Model)
	}
}

func TestBootArgv_LeanFlagsFollowTheBrief(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	argv := BootArgv("wipnote", Info{Harness: HarnessClaude, Name: "w", Flags: o.Flags, Lean: o.Lean}, "the brief")
	dash, brief, tools := -1, -1, -1
	for i, a := range argv {
		switch a {
		case "--":
			dash = i
		case "the brief":
			brief = i
		case "--tools":
			tools = i
		}
	}
	if dash < 0 || brief != dash+1 || tools < brief {
		t.Fatalf("flags must follow the brief: %q", argv)
	}
	if !hasFlag(argv[:dash], NoOrchestratorFlag) {
		t.Fatalf("lean boot lacks %s before --: %q", NoOrchestratorFlag, argv)
	}
	if full := BootArgv("wipnote", Info{Harness: HarnessClaude, Name: "w"}, "b"); hasFlag(full, NoOrchestratorFlag) {
		t.Fatalf("a full worker keeps the orchestrator prompt: %q", full)
	}
}

func TestResume_KeepsTheOldRunsSetup(t *testing.T) {
	e := newEnv(t)
	lean := e.opts("w1")
	lean.Profile, lean.Tools = "simple", []string{"Grep"}
	stoppedWorker(t, e, lean, "sid-a")
	s, info := resume(t, e, e.resumeOpts("w2"), "w1")
	if !info.Lean || info.Model != "haiku" || info.Setup.Profile != "simple" || !hasFlag(info.Flags, "--tools") {
		t.Fatalf("a resume that chose nothing lost the setup: %+v", info)
	}
	if s.Profile != "simple" || s.Effort != "low" || len(s.Tools) != 1 {
		t.Fatalf("spawned = %+v", s)
	}
	if !hasFlag(BootArgv("wipnote", info, "b"), NoOrchestratorFlag) {
		t.Fatal("a lean resume got the orchestrator prompt back")
	}
}

func TestResume_NewChoiceAppliesProfile(t *testing.T) {
	e := newEnv(t)
	stoppedWorker(t, e, e.opts("w1"), "sid-a") // a full run
	o := e.resumeOpts("w2")
	o.Profile = "deep"
	_, info := resume(t, e, o, "w1")
	if !info.Lean || info.Model != "opus" || !hasFlag(info.Flags, "--strict-mcp-config") {
		t.Fatalf("a resume with --profile deep should start lean on opus: %+v", info)
	}
}

func TestMCP_LocalServersGoToA0600FileNotArgv(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	writeJSON(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"a":{"command":"a","env":{"K":"SECRET"}},"b":{"command":"b"}}}`)
	e := newEnv(t)
	o := e.opts("w1")
	o.Profile, o.MCP, o.Root = "standard", []string{"a"}, root
	s, err := Spawn(ctxBG, e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := ReadInfo(e.runFolder(s.Run))
	path := argAfter(info.Flags, "--mcp-config")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mcp.json: %v %v", err, st)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"a"`) || strings.Contains(string(data), `"b"`) {
		t.Fatalf("mcp.json = %s: want only a", data)
	}
	if strings.Contains(strings.Join(info.Flags, " "), "SECRET") || !hasFlag(info.Flags, "--strict-mcp-config") {
		t.Fatalf("flags = %q", info.Flags)
	}
	if !strings.Contains(argAfter(info.Flags, "--tools"), "ToolSearch") {
		t.Fatalf("any MCP server needs ToolSearch: %q", info.Flags)
	}
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMCP_ConnectorDropsStrictAndDeniesTheRest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	listMCP = func(context.Context, string) ([]string, error) {
		return []string{"claude.ai Notion", "claude.ai Gmail", "plugin:x:y", "MCP_DOCKER"}, nil
	}
	t.Cleanup(func() { listMCP = claudeMCPList })
	o := SpawnOptions{Harness: HarnessClaude, MCP: []string{"claude.ai Notion"}, Root: t.TempDir()}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if hasFlag(o.Flags, "--strict-mcp-config") || hasFlag(o.Flags, "--mcp-config") {
		t.Fatalf("flags = %q", o.Flags)
	}
	deny := settingsOf(t, o.Flags)["deniedMcpServers"].([]any)
	if len(deny) != 3 {
		t.Fatalf("deny = %v: want every server but Notion", deny)
	}
	for _, d := range deny {
		if d.(map[string]any)["serverName"] == "claude.ai Notion" {
			t.Fatal("denied the server that was asked for")
		}
	}
	if !strings.Contains(argAfter(o.Flags, "--tools"), "ToolSearch") {
		t.Fatalf("connectors need ToolSearch: %q", o.Flags)
	}
}

func TestMCP_UnknownRefusedWithValidNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeJSON(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"a":{}}}`)
	o := SpawnOptions{Harness: HarnessClaude, MCP: []string{"nope"}, Root: t.TempDir()}
	if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) || !strings.Contains(err.Error(), "a") {
		t.Fatalf("err = %v", err)
	}
	listMCP = func(context.Context, string) ([]string, error) { return []string{"claude.ai Gmail"}, nil }
	t.Cleanup(func() { listMCP = claudeMCPList })
	o = SpawnOptions{Harness: HarnessClaude, MCP: []string{"claude.ai Notion"}, Root: t.TempDir()}
	if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) || !strings.Contains(err.Error(), "claude.ai Gmail") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseMCPList_KeepsOnlyNames(t *testing.T) {
	out := "Checking MCP server health…\n\nclaude.ai Notion: https://mcp.notion.com/mcp - ✔ Connected\nplugin:cloudflare:cloudflare-docs: https://x/mcp (HTTP) - ✔ Connected\nMCP_DOCKER: docker mcp gateway run - ✘ Failed\n"
	got := strings.Join(parseMCPList(out), "|")
	if got != "claude.ai Notion|plugin:cloudflare:cloudflare-docs|MCP_DOCKER" {
		t.Fatalf("names = %q", got)
	}
}

func TestPlugins_OnlyTheRequestedStayOn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeJSON(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins":{"a@m":true,"b@m":true,"c@m1":true,"c@m2":true}}`)
	o := SpawnOptions{Harness: HarnessClaude, Plugins: []string{"a"}}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	p := settingsOf(t, o.Flags)["enabledPlugins"].(map[string]any)
	if p["a@m"] != true || p["b@m"] != false || p["c@m1"] != false {
		t.Fatalf("enabledPlugins = %v", p)
	}
	if !strings.Contains(argAfter(o.Flags, "--tools"), "Skill") {
		t.Fatalf("plugins need Skill: %q", o.Flags)
	}
	for _, bad := range []string{"zzz", "c"} {
		o = SpawnOptions{Harness: HarnessClaude, Plugins: []string{bad}}
		if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	o = SpawnOptions{Harness: HarnessClaude, Plugins: []string{"c@m2"}}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatalf("a full name must work: %v", err)
	}
}

func TestLeanSettings_CacheTTLDefaultsTo5m(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(argAfter(LeanFlags(nil, LeanSpec{}), "--settings")), &m); err != nil {
		t.Fatal(err)
	}
	if m["promptCacheTtl"] != "5m" {
		t.Fatalf("promptCacheTtl = %v", m["promptCacheTtl"])
	}
}

func TestHarnessFlags_CacheTTLOverridesProfile(t *testing.T) {
	cfg := ClaudeConfig{Profiles: map[string]Profile{"idle": {CacheTTL: "1h"}}}
	o := SpawnOptions{Harness: HarnessClaude, Config: cfg, Profile: "idle"}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argAfter(o.Flags, "--settings"), `"promptCacheTtl":"1h"`) || o.setup().CacheTTL != "1h" {
		t.Fatalf("profile cacheTTL not applied: %q", argAfter(o.Flags, "--settings"))
	}
	o = SpawnOptions{Harness: HarnessClaude, Config: cfg, Profile: "idle", CacheTTL: "5m"}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argAfter(o.Flags, "--settings"), `"promptCacheTtl":"5m"`) {
		t.Fatalf("--cache-ttl must beat the profile: %q", argAfter(o.Flags, "--settings"))
	}
}

func TestHarnessFlags_BadCacheTTLRefused(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, CacheTTL: "30m"}
	if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) {
		t.Fatalf("flag: err = %v", err)
	}
	cfg := ClaudeConfig{Profiles: map[string]Profile{"bad": {CacheTTL: "2h"}}}
	o = SpawnOptions{Harness: HarnessClaude, Config: cfg, Profile: "bad"}
	if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) {
		t.Fatalf("profile: err = %v", err)
	}
	o = SpawnOptions{Harness: HarnessCodex, CacheTTL: "1h"}
	if err := harnessFlags(ctxBG, &o); !errors.Is(err, ErrBadSetup) {
		t.Fatalf("codex: err = %v", err)
	}
}

func TestLeanSpec_MdExcludesTheProjectNotTheUser(t *testing.T) {
	root := t.TempDir()
	ex := LeanSpec{Home: "/h", UserRules: true, RulesRoots: []string{root, root, ""}}.mdExcludes()
	joined := strings.Join(ex, "\n")
	if strings.Contains(joined, "/h/") {
		t.Fatalf("--user-rules must keep the user's file: %q", ex)
	}
	for _, want := range []string{root + "/**/CLAUDE.md", root + "/.claude/rules/**"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %q in %q", want, ex)
		}
	}
	n := 0
	for _, e := range ex {
		if e == root+"/**/CLAUDE.md" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("root listed %d times: %q", n, ex)
	}
	if got := (LeanSpec{}).mdExcludes(); len(got) != 0 {
		t.Errorf("zero spec excludes %q", got)
	}
}

func TestBuiltinProfiles_ProjectRulesOnlyInDeep(t *testing.T) {
	for name, want := range map[string]bool{"simple": false, "standard": false, "deep": true} {
		if builtinProfiles[name].ProjectRules != want {
			t.Errorf("%s: ProjectRules = %v, want %v", name, !want, want)
		}
	}
}

func TestRulesLine(t *testing.T) {
	lean := Info{Lean: true}
	if rulesLine(lean) == "" {
		t.Error("a lean worker without rules must be told")
	}
	kept := Info{Lean: true}
	kept.Setup.ProjectRules = true
	box := Info{Lean: true, Place: PlaceContainer}
	for name, i := range map[string]Info{"not lean": {}, "kept": kept, "container": box} {
		if got := rulesLine(i); got != "" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

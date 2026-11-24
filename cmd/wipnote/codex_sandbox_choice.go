package main

import (
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// codexSandboxChoice is what the user already decided about the Codex sandbox
// for this launch. The launcher reads it only to stay out of the way: a set
// choice is never replaced (bug-fc843d71).
type codexSandboxChoice struct {
	Set      bool   // a flag, an override, a profile, a config layer or a trust entry decides
	ReadOnly bool   // the decision is a read-only sandbox
	Source   string // where the decision came from, for the note
}

// codexSandboxLayer is the part of one Codex config file that selects the
// sandbox. Pointers tell an absent key from an empty one.
type codexSandboxLayer struct {
	SandboxMode        *string                      `toml:"sandbox_mode"`
	DefaultPermissions *string                      `toml:"default_permissions"`
	Profile            string                       `toml:"profile"`
	Profiles           map[string]codexSandboxLayer `toml:"profiles"`
	Projects           map[string]struct {
		TrustLevel string `toml:"trust_level"`
	} `toml:"projects"`
}

func readCodexSandboxLayer(path string) codexSandboxLayer {
	var cfg codexSandboxLayer
	if path == "" {
		return cfg
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(data, &cfg)
	}
	return cfg
}

// choice is the sandbox this layer selects, if it selects one.
func (l codexSandboxLayer) choice(source string) (codexSandboxChoice, bool) {
	for _, v := range []*string{l.SandboxMode, l.DefaultPermissions} {
		if v != nil {
			return codexSandboxChoice{Set: true, ReadOnly: strings.Contains(*v, "read-only"), Source: source}, true
		}
	}
	return codexSandboxChoice{}, false
}

// readCodexSandboxChoice finds the user's sandbox decision, highest precedence
// first: launch flags, -c overrides, the active profile, the project config,
// the user config, the system config, then the project's trust entry.
//
// It reads the project layer although Codex may ignore it for an untrusted
// project. The error then is only that wipnote adds no sandbox, which is the
// behaviour before this code existed.
func readCodexSandboxChoice(opts codexLaunchOpts) codexSandboxChoice {
	args := opts.ExtraArgs
	if v := codexRequestedSandbox(args); v != "" {
		return codexSandboxChoice{Set: true, ReadOnly: v == "read-only", Source: "--sandbox " + v}
	}
	if flag := codexPresetFlag(args); flag != "" {
		return codexSandboxChoice{Set: true, Source: flag}
	}
	overrides := codexConfigOverrides(args)
	for _, key := range []string{"sandbox_mode", "default_permissions"} {
		if v, ok := overrides[key]; ok {
			return codexSandboxChoice{Set: true, ReadOnly: strings.Contains(v, "read-only"), Source: "-c " + key}
		}
	}
	home := codexHomeDir()
	user, system := readCodexSandboxLayer(codexHomeConfigPath()), readCodexSandboxLayer(codexSystemConfigPath)
	launchDir := codexLaunchDir(opts)
	type sourced struct {
		layer  codexSandboxLayer
		source string
	}
	var layers []sourced
	add := func(l codexSandboxLayer, source string) { layers = append(layers, sourced{l, source}) }
	if profile := firstNonEmpty(codexProfileFromArgs(args), overrides["profile"], user.Profile, system.Profile); profile != "" {
		add(readCodexSandboxLayer(filepath.Join(home, profile+".config.toml")), "profile "+profile)
		add(user.Profiles[profile], "profile "+profile)
	}
	add(readCodexSandboxLayer(filepath.Join(launchDir, ".codex", "config.toml")), "the project's .codex/config.toml")
	add(user, "your Codex config")
	add(system, codexSystemConfigPath)
	for _, l := range layers {
		if c, ok := l.layer.choice(l.source); ok {
			return c
		}
	}
	return codexTrustChoice(user, launchDir)
}

// codexTrustChoice turns the project's trust entry into a sandbox decision.
// With an entry, trusted or untrusted, Codex 0.172.0 accepts --add-dir with no
// sandbox flag (probed with an isolated CODEX_HOME). Only a project with no
// entry gets the read-only default that refuses it, so an entry settles the
// question and its absence leaves it open.
func codexTrustChoice(user codexSandboxLayer, launchDir string) codexSandboxChoice {
	dir := resolvedPath(launchDir)
	for path, project := range user.Projects {
		p := resolvedPath(path)
		if project.TrustLevel == "" || (dir != p && !strings.HasPrefix(dir, p+string(filepath.Separator))) {
			continue
		}
		return codexSandboxChoice{Set: true, Source: "project marked " + project.TrustLevel}
	}
	return codexSandboxChoice{}
}

// codexLaunchDir is the folder Codex starts in.
func codexLaunchDir(opts codexLaunchOpts) string {
	if dir := firstNonEmpty(opts.WorktreeRoot, opts.ProjectRoot); dir != "" {
		return dir
	}
	dir, _ := os.Getwd()
	return dir
}

func resolvedPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// codexPresetFlag returns the flag in args that selects a writable sandbox as
// part of a preset, or "".
func codexPresetFlag(args []string) string {
	for _, a := range args {
		switch a {
		case "--":
			return ""
		case "--approve-for-me", "--full-auto":
			return a
		}
	}
	return ""
}

// codexConfigOverrides collects the key=value overrides in args, in each
// spelling Codex accepts: -c K=V, -cK=V, --config K=V and --config=K=V, with
// optional spaces round "=" and quotes round V. A later override replaces an
// earlier one, as it does in Codex.
func codexConfigOverrides(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		var kv string
		switch a := args[i]; {
		case a == "--":
			return out
		case a == "-c" || a == "--config":
			if i+1 < len(args) {
				i++
				kv = args[i]
			}
		case strings.HasPrefix(a, "--config="):
			kv = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c") && !strings.HasPrefix(a, "--"):
			kv = strings.TrimPrefix(a, "-c")
		}
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

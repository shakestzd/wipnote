package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// wipnotePermRules are the permission rules that wipnote recommends
// for optimal subagent operation.
var wipnotePermRules = []string{
	"Bash(wipnote *)",
}

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure Claude Code permissions for wipnote",
		Long: `Add recommended permission rules to your Claude Code settings.

This adds Bash(wipnote *) to your ~/.claude/settings.json so that
subagents can run wipnote CLI commands (like work item registration)
without requiring manual approval each time.

This is optional — wipnote works without it, but subagents may be
blocked by permission prompts when running in the background.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd.OutOrStdout())
		},
	}
	return cmd
}

func runSetup(out io.Writer) error {
	settingsPath, err := claudeSettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettingsJSON(settingsPath)
	if err != nil {
		return err
	}

	added := mergePermissions(settings, wipnotePermRules)

	if len(added) == 0 {
		fmt.Fprintln(out, "Claude Code permissions already configured — nothing to do.")
		return nil
	}

	if err := writeSettingsJSON(settingsPath, settings); err != nil {
		return err
	}

	fmt.Fprintln(out, "Updated:", settingsPath)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Added permissions:")
	for _, rule := range added {
		fmt.Fprintf(out, "  + %s\n", rule)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Subagents can now run wipnote CLI commands without manual approval.")
	return nil
}

// claudeSettingsPath returns ~/.claude/settings.json.
func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// readSettingsJSON reads and parses the settings file.
// Returns an empty map if the file doesn't exist.
func readSettingsJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return settings, nil
}

// writeSettingsJSON atomically writes settings back to disk.
func writeSettingsJSON(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	data = append(data, '\n')

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write atomically via temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

// mergePermissions adds rules to settings["permissions"]["allow"] if not
// already present. Returns the list of rules that were actually added.
func mergePermissions(settings map[string]any, rules []string) []string {
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		perms = map[string]any{}
		settings["permissions"] = perms
	}

	// Get existing allow list.
	var existing []string
	if raw, ok := perms["allow"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					existing = append(existing, s)
				}
			}
		}
	}

	// Check which rules need adding.
	existSet := make(map[string]bool, len(existing))
	for _, e := range existing {
		existSet[e] = true
	}

	var added []string
	for _, rule := range rules {
		if !existSet[rule] {
			existing = append(existing, rule)
			added = append(added, rule)
		}
	}

	if len(added) > 0 {
		// Convert back to []any for JSON.
		allowAny := make([]any, len(existing))
		for i, s := range existing {
			allowAny[i] = s
		}
		perms["allow"] = allowAny
	}

	return added
}

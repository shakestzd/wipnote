package main

// Register in main.go: rootCmd.AddCommand(orchestratorCmd())

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// orchestratorConfig mirrors the JSON stored in .wipnote/orchestrator.json.
type orchestratorConfig struct {
	Enabled       bool   `json:"enabled"`
	Mode          string `json:"mode,omitempty"`
	Violations    int    `json:"violations,omitempty"`
	MaxViolations int    `json:"max_violations,omitempty"`
}

func orchestratorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Manage orchestrator mode",
	}
	cmd.AddCommand(orchestratorStatusCmd())
	cmd.AddCommand(orchestratorEnableCmd())
	cmd.AddCommand(orchestratorDisableCmd())
	return cmd
}

func orchestratorStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show orchestrator mode status",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOrchestratorStatus()
		},
	}
}

func orchestratorEnableCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable orchestrator mode",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOrchestratorEnable(strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "Use strict enforcement mode")
	return cmd
}

func orchestratorDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable orchestrator mode",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runOrchestratorDisable()
		},
	}
}

func runOrchestratorStatus() error {
	cfg, err := loadOrchestratorConfig()
	if err != nil {
		fmt.Println("Orchestrator: disabled (no config)")
		return nil
	}
	status := "disabled"
	if cfg.Enabled {
		status = "enabled"
	}
	fmt.Printf("Orchestrator: %s\n", status)
	if cfg.Mode != "" {
		fmt.Printf("  Mode:       %s\n", cfg.Mode)
	}
	fmt.Printf("  Violations: %d", cfg.Violations)
	if cfg.MaxViolations > 0 {
		fmt.Printf(" / %d", cfg.MaxViolations)
	}
	fmt.Println()
	return nil
}

func runOrchestratorEnable(strict bool) error {
	mode := "guidance"
	if strict {
		mode = "strict"
	}
	cfg := orchestratorConfig{
		Enabled:       true,
		Mode:          mode,
		Violations:    0,
		MaxViolations: 3,
	}
	if err := saveOrchestratorConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Orchestrator enabled (mode: %s)\n", mode)
	return nil
}

func runOrchestratorDisable() error {
	cfg := orchestratorConfig{Enabled: false}
	if err := saveOrchestratorConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Orchestrator disabled")
	return nil
}

func orchestratorConfigPath() (string, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "orchestrator.json"), nil
}

func loadOrchestratorConfig() (orchestratorConfig, error) {
	path, err := orchestratorConfigPath()
	if err != nil {
		return orchestratorConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return orchestratorConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg orchestratorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return orchestratorConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func saveOrchestratorConfig(cfg orchestratorConfig) error {
	path, err := orchestratorConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

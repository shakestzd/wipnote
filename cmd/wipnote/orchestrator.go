package main

// Register in main.go: rootCmd.AddCommand(orchestratorCmd())

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/hooks"
)

// orchestratorConfig is the canonical config type, owned by core/hooks so the
// PreToolUse guard that enforces it and this CLI cannot drift apart
// (feat-567c0211).
type orchestratorConfig = hooks.OrchestratorConfig

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
	mode := hooks.OrchestratorModeGuidance
	if strict {
		mode = hooks.OrchestratorModeStrict
	}
	// Violations reset to 0 here: enabling is the documented way to clear an
	// escalation that has reached max_violations.
	cfg := orchestratorConfig{
		Enabled:       true,
		Mode:          mode,
		Violations:    0,
		MaxViolations: hooks.OrchestratorDefaultMaxViolations,
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

func loadOrchestratorConfig() (orchestratorConfig, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return orchestratorConfig{}, err
	}
	cfg, ok := hooks.LoadOrchestratorConfig(dir)
	if !ok {
		return orchestratorConfig{}, fmt.Errorf("read config: %s not present or unreadable",
			hooks.OrchestratorConfigPath(dir))
	}
	return cfg, nil
}

func saveOrchestratorConfig(cfg orchestratorConfig) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	return hooks.SaveOrchestratorConfig(dir, cfg)
}

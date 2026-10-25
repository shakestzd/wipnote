package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

func wiAddStepCmd(typeName string) *cobra.Command {
	var allowHostPaths bool
	cmd := &cobra.Command{
		Use:   "add-step <id> <description>",
		Short: "Add an implementation step to a " + typeName,
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiAddStep(typeName, args[0], args[1], allowHostPaths)
		},
	}
	cmd.Flags().BoolVar(&allowHostPaths, "allow-host-paths", false, "bypass host-local path check in step description")
	return cmd
}

func runWiAddStep(typeName, id, description string, allowHostPaths bool) error {
	if err := validateDescriptionForHostPaths(description, allowHostPaths); err != nil {
		return err
	}

	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	id, err = workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	col := collectionFor(p, typeName)
	if err := col.Edit(id).AddStep(description).Save(); err != nil {
		return fmt.Errorf("add step: %w", err)
	}
	fmt.Printf("Added step to %s: %s\n", id, description)
	return nil
}

// wiAddTaskStepCmd registers `add-task-step` — a hook-only command that adds a
// step with StepID="task-<taskID>" so CompleteTaskStep can find and tick it later.
// Used by internal/hooks/task_tracking.go addTaskStep (TaskCreated hook).
func wiAddTaskStepCmd(typeName string) *cobra.Command {
	return &cobra.Command{
		Use:    "add-task-step <id> <task-id> <description>",
		Short:  "Add a task-associated step (hook-only)",
		Args:   cobra.ExactArgs(3),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiAddTaskStep(typeName, args[0], args[1], args[2])
		},
	}
}

func runWiAddTaskStep(typeName, id, taskID, description string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	id, err = workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	col := collectionFor(p, typeName)
	if err := col.AddTaskStep(id, taskID, description); err != nil {
		return fmt.Errorf("add task step: %w", err)
	}
	return nil
}

// wiCompleteTaskStepCmd registers `complete-task-step` — a hook-only command
// that flips data-completed=true on the step with StepID="task-<taskID>".
// Used by internal/hooks/task_tracking.go completeTaskStep (TaskCompleted hook).
func wiCompleteTaskStepCmd(typeName string) *cobra.Command {
	return &cobra.Command{
		Use:    "complete-task-step <id> <task-id>",
		Short:  "Mark a task-associated step as completed (hook-only)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiCompleteTaskStep(typeName, args[0], args[1])
		},
	}
}

func runWiCompleteTaskStep(typeName, id, taskID string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	id, err = workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	col := collectionFor(p, typeName)
	if err := col.CompleteTaskStep(id, taskID); err != nil {
		return fmt.Errorf("complete task step: %w", err)
	}
	return nil
}

// wiCompleteStepCmd creates the complete-step subcommand for a work item type.
func wiCompleteStepCmd(typeName string) *cobra.Command {
	return &cobra.Command{
		Use:   "complete-step <id> [step-number]",
		Short: "Mark a manual step as completed",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			stepNum := 0
			if len(args) > 1 {
				var err error
				stepNum, err = strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("step number must be an integer, got %q", args[1])
				}
			}
			return runWiCompleteStep(typeName, args[0], stepNum)
		},
	}
}

// runWiCompleteStep completes a manual step by index (1-based) or auto-completes the next incomplete step if stepNum is 0.
func runWiCompleteStep(typeName, id string, stepNum int) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	id, err = workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	col := collectionFor(p, typeName)
	if err := col.CompleteStep(id, stepNum); err != nil {
		return fmt.Errorf("complete step: %w", err)
	}
	return nil
}

// splitSteps splits a comma-separated steps string into trimmed non-empty parts.
func splitSteps(s string) []string {
	var steps []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			steps = append(steps, trimmed)
		}
	}
	return steps
}

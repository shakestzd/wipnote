package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ---- slice lifecycle commands (slice-4) ----------------------------------------

// validSliceExecutionStatuses is the canonical set of slice execution status values.
var validSliceExecutionStatuses = []string{
	"not_started", "promoted", "in_progress", "done", "blocked", "superseded",
}

// planApproveSliceCmd sets approval_status=approved for a slice in the plan YAML.
func planApproveSliceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve-slice <plan-id> <slice-num>",
		Short: "Set approval_status=approved for a plan slice",
		Long: `Record an approval for a specific plan slice. The approval is written to
the plan YAML as a feedback entry (section='slice-<num>', action='approve')
and mirrored onto the slice's approval_status.

Example:
  wipnote plan approve-slice plan-abc12345 3`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runApproveSlice(wipnoteDir, args[0], args[1])
		},
	}
}

func runApproveSlice(wipnoteDir, planID, sliceNumStr string) error {
	sliceNum, err := parseSliceNum(sliceNumStr)
	if err != nil {
		return err
	}
	section := fmt.Sprintf("slice-%d", sliceNum)
	if err := storePlanFeedback(wipnoteDir, planID, section, "approve", "true", ""); err != nil {
		return fmt.Errorf("store approve feedback: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Slice %d approved for plan %s\n", sliceNum, planID)
	return nil
}

// planRejectSliceCmd sets approval_status=rejected (or changes_requested) for a slice.
func planRejectSliceCmd() *cobra.Command {
	var changesRequested bool
	cmd := &cobra.Command{
		Use:   "reject-slice <plan-id> <slice-num>",
		Short: "Set approval_status=rejected for a plan slice",
		Long: `Record a rejection for a specific plan slice in the plan YAML. By default
it writes a feedback entry with action='reject' and sets the slice's
approval_status to rejected. Use --changes-requested to write
action='changes_requested' and approval_status=changes_requested instead.

Example:
  wipnote plan reject-slice plan-abc12345 3
  wipnote plan reject-slice plan-abc12345 3 --changes-requested`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runRejectSlice(wipnoteDir, args[0], args[1], changesRequested)
		},
	}
	cmd.Flags().BoolVar(&changesRequested, "changes-requested", false, "store action=changes_requested instead of reject")
	return cmd
}

func runRejectSlice(wipnoteDir, planID, sliceNumStr string, changesRequested bool) error {
	sliceNum, err := parseSliceNum(sliceNumStr)
	if err != nil {
		return err
	}
	section := fmt.Sprintf("slice-%d", sliceNum)
	action := "reject"
	approval := "rejected"
	if changesRequested {
		action = "changes_requested"
		approval = "changes_requested"
	}
	if err := storePlanFeedback(wipnoteDir, planID, section, action, "false", ""); err != nil {
		return fmt.Errorf("store reject feedback: %w", err)
	}
	if err := storePlanFeedback(wipnoteDir, planID, section, "approve", approval, ""); err != nil {
		return fmt.Errorf("store approve=false feedback: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Slice %d rejected (action=%s) for plan %s\n", sliceNum, action, planID)
	return nil
}

// planAnswerSliceQuestionCmd records the answer to a slice-local question.
func planAnswerSliceQuestionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "answer-slice-question <plan-id> <slice-num> <question-id> <answer-key>",
		Short: "Record the answer to a slice-local question in plan_feedback",
		Long: `Record the answer to a slice-local question. Stored in SQLite plan_feedback
with section='slice-<num>-question-<question-id>' and action='answer'.

Example:
  wipnote plan answer-slice-question plan-abc12345 3 q-error-handling yes`,
		Args: cobra.ExactArgs(4),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runAnswerSliceQuestion(wipnoteDir, args[0], args[1], args[2], args[3])
		},
	}
}

func runAnswerSliceQuestion(wipnoteDir, planID, sliceNumStr, questionID, answerKey string) error {
	sliceNum, err := parseSliceNum(sliceNumStr)
	if err != nil {
		return err
	}
	section := fmt.Sprintf("slice-%d-question-%s", sliceNum, questionID)
	if err := storePlanFeedback(wipnoteDir, planID, section, "answer", answerKey, questionID); err != nil {
		return fmt.Errorf("store answer feedback: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Answer recorded: plan=%s slice=%d question=%s answer=%s\n",
		planID, sliceNum, questionID, answerKey)
	return nil
}

// planSetSliceStatusCmd sets the execution_status for a slice in plan_feedback.
func planSetSliceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-slice-status <plan-id> <slice-num> <status>",
		Short: "Set the execution_status for a plan slice",
		Long: `Set the execution_status for a specific plan slice. Valid statuses:
  not_started | promoted | in_progress | done | blocked | superseded

Stored in SQLite plan_feedback with section='slice-<num>' and action='set-status'.

Example:
  wipnote plan set-slice-status plan-abc12345 3 in_progress`,
		Args: cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runSetSliceStatus(wipnoteDir, args[0], args[1], args[2])
		},
	}
}

func runSetSliceStatus(wipnoteDir, planID, sliceNumStr, status string) error {
	if err := validateSliceExecutionStatus(status); err != nil {
		return err
	}
	sliceNum, err := parseSliceNum(sliceNumStr)
	if err != nil {
		return err
	}
	section := fmt.Sprintf("slice-%d", sliceNum)
	if err := storePlanFeedback(wipnoteDir, planID, section, "set_execution_status", status, ""); err != nil {
		return fmt.Errorf("store set_execution_status feedback: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Slice %d execution_status → %s for plan %s\n", sliceNum, status, planID)
	return nil
}

// validateSliceExecutionStatus returns an error if status is not a valid slice execution status.
func validateSliceExecutionStatus(status string) error {
	for _, v := range validSliceExecutionStatuses {
		if v == status {
			return nil
		}
	}
	return fmt.Errorf("invalid slice execution status %q — valid values: %s",
		status, strings.Join(validSliceExecutionStatuses, ", "))
}

// parseSliceNum parses a slice number string and returns the int or an error.
func parseSliceNum(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("slice-num must be a positive integer (got %q)", s)
	}
	return n, nil
}

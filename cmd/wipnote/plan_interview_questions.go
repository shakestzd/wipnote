package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/plan/interview"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planInterviewQuestionsCmd prints the canonical staged interview question set
// (JSON) for a plan or slice. Any harness can fetch this and render it via its
// native ask-user tool (Claude AskUserQuestion) or as chat questions, so the
// questions are defined once.
func planInterviewQuestionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "interview-questions <plan-id> [slice-num]",
		Short: "Print the canonical interview question set (JSON) for a plan or slice",
		Long: "Emits the staged interview question model ({\"stages\":[…]}) so any\n" +
			"harness can render it: inline via a native ask-user tool (Claude\n" +
			"AskUserQuestion), or as plain conversational questions when the harness\n" +
			"has no such tool (Codex). One source of truth for the questions.\n\n" +
			"  interview-questions <plan>            upfront intake: triage + problem/\n" +
			"                                         goals/constraints (before slices).\n" +
			"  interview-questions <plan> <slice>    per-slice: complexity template +\n" +
			"                                         the slice's open questions.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			var stages []interview.Stage
			if len(args) == 1 {
				stages = interview.PlanIntakeStages()
			} else {
				sliceNum, err := parseSliceNum(args[1])
				if err != nil {
					return err
				}
				plan, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", args[0]+".yaml"))
				if err != nil {
					return fmt.Errorf("load plan: %w", err)
				}
				_, slice, err := findPlanSlice(plan, sliceNum)
				if err != nil {
					return err
				}
				stages = attachBlockPrompts(interview.BuildForSlice(slice.Complexity, sliceOpenQuestions(slice)))
			}
			out, err := json.MarshalIndent(interview.Definition{Stages: stages}, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal questions: %w", err)
			}
			fmt.Println(string(out))
			return nil
		},
	}
}

// attachBlockPrompts decorates each stage with its blocks-first authoring
// prompts (interview.StageBlockPlan), keeping planyaml.BlockCatalog as the
// source of truth for WHICH block types exist: any prompt naming a type the live
// catalog no longer defines is dropped, and each surviving prompt is annotated
// with the catalog's own one-line description. This is how blocks drive the
// interview in every renderer (native ask-user, plain chat): block
// elicitation is folded into the stages, not bolted on in a post-pass.
func attachBlockPrompts(stages []interview.Stage) []interview.Stage {
	catalog := map[string]string{}
	for _, spec := range planyaml.BlockCatalog() {
		catalog[spec.Type] = spec.Description
	}
	for i := range stages {
		var prompts []interview.BlockPrompt
		for _, bp := range interview.StageBlockPlan(stages[i].Key) {
			desc, known := catalog[bp.Type]
			if !known {
				continue // catalog dropped this type — never emit a stale tag
			}
			bp.Description = desc
			prompts = append(prompts, bp)
		}
		stages[i].Blocks = prompts
	}
	return stages
}

// sliceOpenQuestions maps a slice's unanswered slice-local questions into
// interview questions so the interview asks the plan's real open questions.
// Choice when the question carries options, free text otherwise.
func sliceOpenQuestions(slice planyaml.PlanSlice) []interview.Question {
	var out []interview.Question
	for _, q := range slice.Questions {
		if strings.TrimSpace(q.Answer) != "" {
			continue // already answered
		}
		iq := interview.Question{
			ID:     "slicequestion." + q.ID,
			Header: q.ID,
			Prompt: q.Text,
		}
		if strings.TrimSpace(q.Description) != "" {
			iq.Prompt = strings.TrimSpace(q.Text + " — " + q.Description)
		}
		if len(q.Options) > 0 {
			iq.Type = interview.Choice
			for _, o := range q.Options {
				label := o.Label
				if label == "" {
					label = o.Key
				}
				iq.Options = append(iq.Options, interview.Option{Label: label})
			}
		} else {
			iq.Type = interview.Text
			iq.Placeholder = "your answer"
		}
		out = append(out, iq)
	}
	return out
}

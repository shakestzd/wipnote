package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/core/workitem"
	commandspkg "github.com/shakestzd/wipnote/internal/commands"
	"github.com/spf13/cobra"
)

// workitemCmd builds a standard CRUD command group for any work item type.
// Usage: workitemCmd("feature", "features"), workitemCmd("bug", "bugs"), etc.
func workitemCmd(typeName, dirName string) *cobra.Command {
	return commandspkg.BuildWorkItemCommand(commandspkg.WorkItemOptions{
		TypeName:         typeName,
		DirName:          dirName,
		Create:           func() *cobra.Command { return wiCreateCmd(typeName, dirName) },
		List:             func() *cobra.Command { return wiListCmd(typeName, dirName) },
		Show:             func() *cobra.Command { return wiShowCmd(typeName) },
		Start:            func() *cobra.Command { return wiStartCmd(typeName) },
		Complete:         func() *cobra.Command { return wiCompleteCmd(typeName) },
		Delete:           func() *cobra.Command { return wiDeleteCmd(typeName) },
		AddStep:          func() *cobra.Command { return wiAddStepCmd(typeName) },
		AddTaskStep:      func() *cobra.Command { return wiAddTaskStepCmd(typeName) },
		CompleteTaskStep: func() *cobra.Command { return wiCompleteTaskStepCmd(typeName) },
		CompleteStep:     func() *cobra.Command { return wiCompleteStepCmd(typeName) },
		Update:           func() *cobra.Command { return wiUpdateCmd(typeName) },
		SetDescription:   func() *cobra.Command { return setDescriptionCmd(typeName) },
		Move:             func() *cobra.Command { return wiMoveCmd(typeName) },
	})
}

func wiListCmd(_ string, dirName string) *cobra.Command {
	var statusFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List " + dirName,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWiList(dirName, statusFilter)
		},
	}
	cmd.Flags().StringVarP(&statusFilter, "status", "s", "",
		"Filter by status (todo, in-progress, blocked, done)")
	return cmd
}

func runWiList(dirName, statusFilter string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	nodes, err := graph.LoadDir(filepath.Join(dir, dirName))
	if err != nil {
		return fmt.Errorf("load %s: %w", dirName, err)
	}

	var filtered []*models.Node
	for _, n := range nodes {
		if statusFilter != "" && string(n.Status) != statusFilter {
			continue
		}
		filtered = append(filtered, n)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	if len(filtered) == 0 {
		fmt.Printf("No %s found.\n", dirName)
		return nil
	}

	fmt.Printf("%-22s  %-11s  %-8s  %s\n", "ID", "STATUS", "PRIORITY", "TITLE")
	fmt.Println(strings.Repeat("-", 80))
	for _, n := range filtered {
		marker := "  "
		if n.Status == models.StatusInProgress {
			marker = "* "
		}
		fmt.Printf("%s%-20s  %-11s  %-8s  %s\n",
			marker, n.ID, n.Status, n.Priority, slug.Truncate(n.Title, 44))
	}
	fmt.Printf("\n%d %s\n", len(filtered), dirName)
	return nil
}

// wiForceStart is set by --force on the start sub-command and consumed by
// wiSetStatusWithAgent to bypass the live-collision refusal.
var wiForceStart bool

func wiStartCmd(typeName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Mark a " + typeName + " as in-progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiSetStatus(typeName, args[0], "in-progress")
		},
	}
	cmd.Flags().BoolVar(&wiForceStart, "force", false,
		"claim anyway even when a live collision is detected (emits a warning and proceeds)")
	return cmd
}

// wiAllowSpecSkip and wiAllowDirtyComplete are set by completion flags and
// consumed by the completion gates below. Package-level because
// wiSetStatusWithAgent has many test callers and we don't want to thread
// parameters through all of them just for opt-in overrides.
var wiAllowSpecSkip bool
var wiAllowDirtyComplete bool

// wiAcceptedAdvisory carries the --accepted-advisory reason. When non-empty
// it provides an audited override of the provenance completion gate: a
// code-bearing item may be completed with zero linked source commits, but
// the operator-supplied rationale is recorded on the .wipnote artifact and
// surfaced by `wipnote check accepted-advisory` and `wipnote show`.
var wiAcceptedAdvisory string

// wiLearning carries the --learning body text. When non-empty, completion
// validates the text as an arch card body and, on success, creates a card
// linked to the work item. A validation failure ABORTS the entire completion
// before any state change.
var wiLearning string

// wiResearchURL and wiResearchWaiver carry the --research-url / --research-waiver
// completion flags. When a code-bearing item changes a dependency manifest, the
// completion must cite at least one http(s) research URL OR record an explicit
// waiver — mirroring the v4 plan research gate (plan/planyaml validate). The
// cited evidence/waiver is persisted on the .wipnote artifact for audit
// (feat-d1bcbf10).
var wiResearchURL []string
var wiResearchWaiver string

// wiLearningKind is the --learning-kind flag value. Defaults to "decision"
// when empty. Must be one of: subsystem-map, invariant, hazard, decision.
var wiLearningKind string

// acceptedAdvisoryMarker prefixes the content note the override rationale is
// persisted under. This predates bug-c65a5f4e, when Node.Properties genuinely
// did not round-trip through the canonical HTML writer/parser and Node.Content
// (via section[data-content]) was the only durable place to put it. Properties
// round-trip now — feat-7ee73444's rollups rely on exactly that — but the
// content-note encoding is kept because existing artifacts carry it and
// acceptedAdvisoryOf reads them back by this stable, parseable prefix.
const acceptedAdvisoryMarker = "accepted-advisory (provenance override): "

// acceptedAdvisoryOf returns the recorded provenance-advisory reason for a
// node, or "" if none. The reason was written into the node content as a
// note prefixed with acceptedAdvisoryMarker.
func acceptedAdvisoryOf(n *models.Node) string {
	return markedNoteOf(n, acceptedAdvisoryMarker)
}

// markedNoteOf returns the text following marker on the first content-note
// line that carries it, or "". Shared by the audited-override readers
// (acceptedAdvisoryOf, and the --allow-orphan marker in tests).
func markedNoteOf(n *models.Node, marker string) string {
	if n == nil || n.Content == "" {
		return ""
	}
	for _, line := range strings.Split(n.Content, "\n") {
		s := strings.TrimSpace(stripHTMLTags(line))
		if idx := strings.Index(s, marker); idx >= 0 {
			return strings.TrimSpace(s[idx+len(marker):])
		}
	}
	return ""
}

func wiCompleteCmd(typeName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "complete <id>",
		Short: "Mark a " + typeName + " as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiSetStatus(typeName, args[0], "done")
		},
	}
	if typeName == "feature" {
		cmd.Flags().BoolVar(&wiAllowSpecSkip, "allow-spec-skip", false,
			"bypass spec_enforcement.feature_complete gate; intended for emergency overrides only")
	}
	if shouldAutocommitWorkitemArtifact(typeName) {
		cmd.Flags().BoolVar(&wiAllowDirtyComplete, "allow-dirty", false,
			"bypass the uncommitted source gate; intended for intentional dirty-tree completion only")
		cmd.Flags().StringVar(&wiAcceptedAdvisory, "accepted-advisory", "",
			"audited override of the zero-commit provenance gate; records the rationale on the artifact")
		cmd.Flags().StringVar(&wiAllowOrphan, "allow-orphan", "",
			"audited override of the merged-upstream gate for intentionally branch-local work; records the rationale on the artifact")
		cmd.Flags().StringArrayVar(&wiResearchURL, "research-url", nil,
			"http(s) URL of the docs/changelog verifying a dependency change; required (or --research-waiver) when the item changes a dependency manifest. Repeatable.")
		cmd.Flags().StringVar(&wiResearchWaiver, "research-waiver", "",
			"waive the research gates (research-first and dependency research); records the rationale on the artifact")
	}
	cmd.Flags().StringVar(&wiLearning, "learning", "",
		"arch card body text to capture as a durable learning; validation failure warns and skips attaching (non-fatal; completion still succeeds)")
	cmd.Flags().StringVar(&wiLearningKind, "learning-kind", "",
		"arch card kind for --learning (default: decision); one of: subsystem-map, invariant, hazard, decision")
	return cmd
}

func printStartTip(typeName, title string) {
	titleSlug := slug.Make(title, 30)
	color := slug.WorkItemColor(typeName)
	switch currentHarness() {
	case "claude":
		fmt.Printf("\nTip: sync your Claude session label to this item:\n")
		fmt.Printf("  /rename %s\n", titleSlug)
		fmt.Printf("  /color %s\n", color)
	default:
		fmt.Printf("\nTip: keep this session aligned with the item:\n")
		fmt.Printf("  label: %s\n", titleSlug)
		fmt.Printf("  color: %s\n", color)
	}
}

func currentHarness() string {
	for _, v := range []string{os.Getenv("WIPNOTE_AGENT_TYPE"), os.Getenv("WIPNOTE_AGENT_ID")} {
		switch {
		case strings.Contains(v, "codex"):
			return "codex"
		case strings.Contains(v, "claude"):
			return "claude"
		case strings.Contains(v, "gemini"):
			return "gemini"
		}
	}
	return "claude"
}

func collectionFor(p *workitem.Project, typeName string) *workitem.Collection {
	switch typeName {
	case "bug":
		return p.Bugs.Collection
	case "spike":
		return p.Spikes.Collection
	case "track":
		return p.Tracks.Collection
	case "plan":
		return p.Plans.Collection
	case "spec":
		return p.Specs.Collection
	default:
		return p.Features.Collection
	}
}

func wiDeleteCmd(typeName string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a " + typeName,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiDelete(args[0])
		},
	}
}

func runWiDelete(id string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	resolved, err := workitem.ResolvePartialID(dir, id)
	if err != nil {
		return err
	}
	path := workitem.NodePath(dir, resolved)
	if path == "" {
		kind := workitem.KindForID(resolved)
		return workitem.ErrNotFoundOnDisk(kind, resolved)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete %s: %w", resolved, err)
	}
	fmt.Printf("Deleted: %s\n", resolved)
	return nil
}

// agentForClaim returns the agent string for claim ownership.
func agentForClaim() string {
	if v := os.Getenv("WIPNOTE_AGENT_TYPE"); v != "" {
		return v
	}
	return "claude-code"
}

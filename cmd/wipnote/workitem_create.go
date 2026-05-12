package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dbpkg "github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/hooks"
	"github.com/shakestzd/wipnote/internal/models"
	"github.com/shakestzd/wipnote/internal/paths"
	"github.com/shakestzd/wipnote/internal/provenance"
	"github.com/shakestzd/wipnote/internal/workitem"
	"github.com/spf13/cobra"
)

type wiCreateOpts struct {
	trackID          string
	planID           string // feature: link to a plan (alternative to --track)
	standaloneReason string // feature: explicit standalone reason (e.g. "pre-plan hotfix")
	priority         string
	description      string
	files            string
	steps            string // comma-separated implementation steps
	start            bool
	noLink           bool
	causedBy         string // explicit caused_by feature ID for bugs
	allowHostPaths   bool   // bypass host-path validation in description

	// Provenance overrides (default: inherit from active session, then env).
	createdByModel      string
	createdByRole       string
	createdByCLIVersion string
}

func wiCreateCmd(typeName, _ string) *cobra.Command {
	var opts wiCreateOpts

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new " + typeName,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiCreate(typeName, args[0], &opts)
		},
	}
	cmd.Flags().StringVar(&opts.trackID, "track", "", "track ID to link to")
	cmd.Flags().StringVar(&opts.priority, "priority", "medium", "priority (low|medium|high|critical)")
	cmd.Flags().StringVar(&opts.description, "description", "", "description text")
	cmd.Flags().BoolVar(&opts.start, "start", false, "immediately mark as in-progress")
	cmd.Flags().BoolVar(&opts.noLink, "no-link", false, "skip auto-linking (e.g. bug to active feature)")
	cmd.Flags().StringVar(&opts.files, "files", "", "comma-separated affected file paths. Paths are stored repo-relative; absolute paths are normalized at write time.")
	cmd.Flags().StringVar(&opts.steps, "steps", "", "comma-separated implementation steps")
	cmd.Flags().BoolVar(&opts.allowHostPaths, "allow-host-paths", false, "bypass host-local path check in --description")
	cmd.Flags().StringVar(&opts.createdByModel, "created-by-model", "",
		"override the model identity recorded as provenance (default: inherit from active session)")
	cmd.Flags().StringVar(&opts.createdByRole, "created-by-role", "",
		"override the agent role recorded as provenance (default: inherit from active session)")
	cmd.Flags().StringVar(&opts.createdByCLIVersion, "created-by-cli-version", "",
		"override the wipnote CLI version recorded as provenance (default: this binary's version)")
	if typeName == "bug" {
		cmd.Flags().StringVar(&opts.causedBy, "caused-by", "", "feature ID that caused this bug")
	}
	if typeName == "feature" {
		cmd.Flags().StringVar(&opts.planID, "plan", "", "plan ID to link this feature to (e.g. plan-abc12345)")
		cmd.Flags().StringVar(&opts.standaloneReason, "standalone", "", "reason this feature exists without a plan (e.g. 'hotfix')")
	}
	return cmd
}

func runWiCreate(typeName, title string, o *wiCreateOpts) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	// Enforce plan hierarchy for features first: require --plan OR --standalone.
	// Features with an explicit --track but no --plan are also accepted (e.g.
	// created by automated finalize), so only reject truly bare feature creates.
	if typeName == "feature" && o.planID == "" && o.standaloneReason == "" && o.trackID == "" {
		return fmt.Errorf("feature must have a parent plan OR --standalone <reason>.\nRun 'wipnote relevant <topic>' to find existing context first.")
	}

	// When --plan is given, resolve the plan to get its track ID so the feature
	// is linked to both plan and track. This must run BEFORE warnMissingFields,
	// otherwise validation rejects --plan-only feature creates for missing --track.
	if typeName == "feature" && o.planID != "" && o.trackID == "" {
		planNode, planErr := p.Plans.Get(o.planID)
		if planErr != nil {
			return fmt.Errorf("plan %s not found: %w", o.planID, planErr)
		}
		if planNode.TrackID != "" {
			o.trackID = planNode.TrackID
		}
	}

	if err := validateDescriptionForHostPaths(o.description, o.allowHostPaths); err != nil {
		return err
	}

	if err := warnMissingFields(typeName, o); err != nil {
		return err
	}

	node, err := createNode(p, typeName, title, o)
	if err != nil {
		return fmt.Errorf("create %s: %w", typeName, err)
	}

	// Post-creation: record steps, session provenance, affected files, and
	// model/role/CLI attribution. Resolve provenance now (after createNode so
	// session lookup is available) and apply via the same Edit chain.
	sessionID := hooks.EnvSessionID("")
	prov := resolveCreateProvenance(dir, sessionID, o)
	hasProvenance := !prov.IsEmpty()
	if o.steps != "" || sessionID != "" || (o.files != "" && typeName != "bug") || hasProvenance {
		col := collectionFor(p, typeName)
		edit := col.Edit(node.ID)
		for _, step := range splitSteps(o.steps) {
			edit = edit.AddStep(step)
		}
		if sessionID != "" {
			edit = edit.SetProperty("created_in_session", sessionID)
		}
		if o.files != "" && typeName != "bug" {
			// Empty repoRoot lets paths.MustNormalize discover the local
			// worktree anchor via git, so a path under a linked worktree
			// normalises to its stable repo-relative form (e.g. cmd/foo.go)
			// instead of .claude/worktrees/<wt>/cmd/foo.go.
			normalized := normalizeFilesInput(o.files, "")
			edit = edit.SetProperty("affected_files", normalized)
		}
		if hasProvenance {
			edit = edit.SetProvenance(prov.Agent, prov.Model, prov.Role, prov.CLIVersion)
		}
		if saveErr := edit.Save(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save metadata: %v\n", saveErr)
		}
	}

	// Wire feature → plan (planned_in edge) and record standalone_reason.
	if typeName == "feature" {
		if o.planID != "" {
			p.Features.AddEdge(node.ID, models.Edge{ //nolint:errcheck
				TargetID:     o.planID,
				Relationship: models.RelPlannedIn,
				Title:        o.planID,
				Since:        time.Now().UTC(),
			})
		}
		if o.standaloneReason != "" {
			edit := p.Features.Edit(node.ID)
			edit = edit.SetProperty("standalone_reason", o.standaloneReason)
			if saveErr := edit.Save(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save standalone_reason: %v\n", saveErr)
			}
		}
	}

	if typeName == "bug" && !o.noLink {
		if o.causedBy != "" {
			autoCausedByEdge(p, node.ID, o.causedBy)
			fmt.Printf("  (caused by %s)\n", o.causedBy)
		} else if featID := detectActiveFeature(p, dir); featID != "" {
			autoCausedByEdge(p, node.ID, featID)
			fmt.Printf("  (linked to %s)\n", featID)
		}
	}

	if o.trackID != "" && typeName != "track" {
		if linkErr := autoTrackEdges(p, node.ID, typeName, o.trackID, node.Title); linkErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-link to track failed: %v\n", linkErr)
		}
	}

	if o.start {
		if _, startErr := collectionFor(p, typeName).Start(node.ID); startErr != nil {
			return fmt.Errorf("start %s: %w", typeName, startErr)
		}
		// Update per-agent attribution so the status line reflects the
		// newly-started work item (mirrors runWiSetStatus logic).
		if sessionID != "" && p.DB != nil {
			agentID := dbpkg.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID"))
			_ = dbpkg.SetActiveWorkItem(p.DB, sessionID, agentID, node.ID)
			// Legacy dual-write for consumers not yet reading active_work_items.
			_ = hooks.UpdateActiveFeature(p.DB, sessionID, node.ID)
		}
		fmt.Printf("Created and started: %s  %s\n", node.ID, node.Title)
	} else {
		fmt.Printf("Created: %s  %s\n", node.ID, node.Title)
	}
	return nil
}

// resolveCreateProvenance returns the provenance to record on a newly-created
// work item. Resolution order, lowest-precedence first, so explicit flags win:
//
//  1. Provenance read from the active session HTML (inheritance default)
//  2. Provenance detected from env vars / CLIVersion (process-level baseline)
//  3. Explicit --created-by-* flags from the create command (user override)
//
// wipnoteDir is the .wipnote directory; the parent of that is the project root
// passed to FromActiveSession.
func resolveCreateProvenance(wipnoteDir, sessionID string, o *wiCreateOpts) provenance.Provenance {
	projectDir := filepath.Dir(wipnoteDir)

	sessionProv := provenance.FromActiveSession(projectDir, sessionID)
	envProv := provenance.Detect()
	flagProv := provenance.Provenance{
		Model:      o.createdByModel,
		Role:       o.createdByRole,
		CLIVersion: o.createdByCLIVersion,
	}

	// Layer: flag → session → env. Session inherits beat env defaults (e.g.
	// "dev" cli-version should not shadow "1.2.3" recorded in the session).
	return flagProv.Merge(sessionProv).Merge(envProv)
}

// normalizeFilesInput splits the comma-separated --files value, trims whitespace
// from each segment, drops empty segments, normalizes each path to be repo-relative
// via paths.MustNormalize (absolute outside-repo paths receive the "unresolved:"
// prefix per the slice-1 policy), and rejoins with commas.
//
// When input is empty the empty string is returned immediately without calling the
// normalizer.
func normalizeFilesInput(input, repoRoot string) string {
	if input == "" {
		return ""
	}
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		seg := strings.TrimSpace(p)
		if seg == "" {
			continue
		}
		out = append(out, paths.MustNormalize(seg, repoRoot))
	}
	return strings.Join(out, ",")
}

func createNode(p *workitem.Project, typeName, title string, o *wiCreateOpts) (*models.Node, error) {
	switch typeName {
	case "feature":
		opts := []workitem.FeatureOption{workitem.FeatWithPriority(o.priority)}
		if o.trackID != "" {
			opts = append(opts, workitem.FeatWithTrack(o.trackID))
		}
		if o.description != "" {
			opts = append(opts, workitem.FeatWithContent(o.description))
		}
		return p.Features.Create(title, opts...)
	case "bug":
		opts := []workitem.BugOption{workitem.BugWithPriority(o.priority)}
		if o.trackID != "" {
			opts = append(opts, workitem.BugWithTrack(o.trackID))
		}
		if o.description != "" {
			opts = append(opts, workitem.BugWithContent(o.description))
		}
		return p.Bugs.Create(title, opts...)
	case "spike":
		opts := []workitem.SpikeOption{workitem.SpikeWithPriority(o.priority)}
		if o.trackID != "" {
			opts = append(opts, workitem.SpikeWithTrack(o.trackID))
		}
		if o.description != "" {
			opts = append(opts, workitem.SpikeWithContent(o.description))
		}
		return p.Spikes.Create(title, opts...)
	case "track":
		opts := []workitem.TrackOption{workitem.TrackWithPriority(o.priority)}
		if o.description != "" {
			opts = append(opts, workitem.TrackWithContent(o.description))
		}
		return p.Tracks.Create(title, opts...)
	case "plan":
		opts := []workitem.PlanOption{workitem.PlanWithPriority(o.priority)}
		if o.trackID != "" {
			opts = append(opts, workitem.PlanWithTrack(o.trackID))
		}
		if o.description != "" {
			opts = append(opts, workitem.PlanWithContent(o.description))
		}
		return p.Plans.Create(title, opts...)
	case "spec":
		opts := []workitem.SpecOption{workitem.SpecWithPriority(o.priority)}
		if o.trackID != "" {
			opts = append(opts, workitem.SpecWithTrack(o.trackID))
		}
		if o.description != "" {
			opts = append(opts, workitem.SpecWithContent(o.description))
		}
		return p.Specs.Create(title, opts...)
	default:
		return nil, fmt.Errorf("unknown type %q\nValid types: feature, bug, spike, track, plan, spec", typeName)
	}
}

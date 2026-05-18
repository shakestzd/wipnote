// Register in main.go: root.AddCommand(whoCmd())
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	dbpkg "github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/harness"
	"github.com/shakestzd/wipnote/internal/hooks"
	"github.com/shakestzd/wipnote/internal/provenance"
	"github.com/spf13/cobra"
)

// whoCmd implements `wipnote who` — prints the current session identity:
// claim owner, session family, harness, work item, execution root.
// Also surfaces any claim collision/collaboration state for the active item.
func whoCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "who",
		Short: "Show current session identity and claim attribution",
		Long: `Show the identity, harness, session family, active work item claim,
and any concurrent-claimant (collision/collaboration) state for this session.

Fields exported:
  session_id        — this session's ID
  session_family_id — the launch family (shared across --resume continuations)
  harness           — detected CLI harness (claude-code, codex-cli, gemini-cli)
  work_item         — the active claimed work item (if any)
  claim_id          — the claim record ID
  claim_status      — claim lifecycle status
  execution_root    — root session for subagent chains
  collision         — warns when two+ sessions hold concurrent claims

Step/task event support per harness:
  claude-code  — TaskCreated/TaskCompleted mapped to task_created/task_completed
  codex-cli    — UNSUPPORTED: no native task lifecycle hooks; step tracking unavailable
  gemini-cli   — UNSUPPORTED: no native task lifecycle hooks; step tracking unavailable`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWho(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit structured JSON output")
	return cmd
}

// whoOutput is the stable JSON schema for `wipnote who --json`.
type whoOutput struct {
	SessionID       string           `json:"session_id"`
	SessionFamilyID string           `json:"session_family_id"`
	Harness         string           `json:"harness"`
	WorkItem        string           `json:"work_item,omitempty"`
	ClaimID         string           `json:"claim_id,omitempty"`
	ClaimStatus     string           `json:"claim_status,omitempty"`
	ClaimedAt       string           `json:"claimed_at,omitempty"`
	ExecutionRoot   string           `json:"execution_root,omitempty"`
	IsSubagent      bool             `json:"is_subagent"`
	Collaboration   *collabOutput    `json:"collaboration,omitempty"`
	TaskTracking    taskTrackingInfo `json:"task_tracking"`
}

type collabOutput struct {
	HasCollision bool             `json:"has_collision"`
	Claimants    []claimantRecord `json:"claimants"`
}

type claimantRecord struct {
	ClaimID   string `json:"claim_id"`
	SessionID string `json:"session_id"`
	Harness   string `json:"harness"`
	ClaimedAt string `json:"claimed_at"`
}

// taskTrackingInfo describes per-harness task/step lifecycle support.
type taskTrackingInfo struct {
	Supported bool   `json:"supported"`
	Detail    string `json:"detail"`
}

func runWho(jsonOut bool) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	database, err := openDB(dir)
	if err != nil {
		return err
	}
	defer database.Close()

	// Resolve session ID: prefer env var, fall back to most-recent active.
	sessionID := hooks.EnvSessionID("")
	if sessionID == "" {
		sessionID, _ = dbpkg.MostRecentActiveSession(database)
	}

	// Detect the raw harness token from the environment. The launcher sets
	// WIPNOTE_AGENT_ID to the AgentID ("codex"/"gemini"), and subagents set it
	// to an arbitrary agent role name — neither is the display harness name
	// resolveTaskTrackingInfo keys on. Authoritative resolution happens below
	// once the claim identity (DB owner_agent) is loaded; this is the fallback.
	prov := provenance.Detect()
	rawHarness := prov.Agent

	// Resolve session family from DB.
	familyID := sessionID
	if sessionID != "" {
		var fid string
		if err := database.QueryRow(
			`SELECT COALESCE(session_family_id, session_id) FROM sessions WHERE session_id = ?`, sessionID,
		).Scan(&fid); err == nil && fid != "" {
			familyID = fid
		}
	}

	// Load claim identity.
	var identity *dbpkg.ClaimIdentity
	if sessionID != "" {
		identity, _ = dbpkg.GetClaimIdentity(database, sessionID)
	}

	// Resolve the display harness name. Prefer the claim/session DB owner_agent
	// (authoritative for what actually claimed the work, and correct even for
	// subagent sessions whose WIPNOTE_AGENT_ID is a role name, not a harness),
	// falling back to the env token. Normalize through the harness registry so
	// launcher AgentIDs ("codex"/"gemini") and DB IDs ("gemini_cli") map to the
	// canonical display name ("codex-cli"/"gemini-cli") before capability lookup.
	harnessToken := rawHarness
	if identity != nil && identity.Harness != "" {
		harnessToken = identity.Harness
	}
	displayHarness := harness.NormalizeDisplayName(harnessToken)
	if displayHarness == "" {
		displayHarness = "claude-code" // default when nothing is known
	}

	// Per-harness task tracking support.
	taskInfo := resolveTaskTrackingInfo(displayHarness)

	out := whoOutput{
		SessionID:       sessionID,
		SessionFamilyID: familyID,
		Harness:         displayHarness,
		IsSubagent:      false,
		TaskTracking:    taskInfo,
	}

	if identity != nil {
		out.WorkItem = identity.WorkItemID
		out.ClaimID = identity.ClaimID
		out.ClaimStatus = string(identity.ClaimStatus)
		out.ClaimedAt = identity.LeasedAt.UTC().Format(time.RFC3339)
		out.ExecutionRoot = identity.ExecutionRoot
		out.IsSubagent = identity.IsSubagent
		if identity.SessionFamilyID != "" {
			out.SessionFamilyID = identity.SessionFamilyID
		}

		// Check for collaboration/collision on the active work item.
		coll, err := dbpkg.DetectCollaboration(database, identity.WorkItemID)
		if err == nil && coll.HasCollision {
			collOut := &collabOutput{HasCollision: true}
			for _, c := range coll.Claimants {
				collOut.Claimants = append(collOut.Claimants, claimantRecord{
					ClaimID:   c.ClaimID,
					SessionID: c.OwnerSessionID,
					Harness:   c.OwnerAgent,
					ClaimedAt: c.LeasedAt.UTC().Format(time.RFC3339),
				})
			}
			out.Collaboration = collOut
		}
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	return renderWhoText(out)
}

// resolveTaskTrackingInfo returns per-harness task lifecycle tracking support.
func resolveTaskTrackingInfo(displayHarness string) taskTrackingInfo {
	switch displayHarness {
	case "claude-code":
		return taskTrackingInfo{
			Supported: true,
			Detail:    "TaskCreated/TaskCompleted mapped to task_created/task_completed events",
		}
	case "codex-cli":
		return taskTrackingInfo{
			Supported: false,
			Detail:    "UNSUPPORTED: codex-cli has no native task lifecycle hooks; step tracking unavailable",
		}
	case "gemini-cli":
		return taskTrackingInfo{
			Supported: false,
			Detail:    "UNSUPPORTED: gemini-cli has no native task lifecycle hooks; step tracking unavailable",
		}
	default:
		return taskTrackingInfo{
			Supported: false,
			Detail:    fmt.Sprintf("unknown harness %q; task lifecycle tracking status unknown", displayHarness),
		}
	}
}

func renderWhoText(out whoOutput) error {
	sep := strings.Repeat("─", 60)
	fmt.Println(sep)
	fmt.Printf("  Session Identity\n")
	fmt.Println(sep)
	fmt.Printf("  Session ID:      %s\n", out.SessionID)
	fmt.Printf("  Family ID:       %s\n", out.SessionFamilyID)
	fmt.Printf("  Harness:         %s\n", out.Harness)
	if out.IsSubagent {
		fmt.Printf("  Role:            subagent\n")
		fmt.Printf("  Execution root:  %s\n", out.ExecutionRoot)
	} else {
		fmt.Printf("  Role:            root CLI\n")
	}

	if out.WorkItem != "" {
		fmt.Printf("\n  Active claim:\n")
		fmt.Printf("    Work item:   %s\n", out.WorkItem)
		fmt.Printf("    Claim ID:    %s\n", out.ClaimID)
		fmt.Printf("    Status:      %s\n", out.ClaimStatus)
		fmt.Printf("    Claimed at:  %s\n", out.ClaimedAt)
	} else {
		fmt.Printf("\n  No active claim.\n")
	}

	if out.Collaboration != nil && out.Collaboration.HasCollision {
		fmt.Printf("\n  COLLISION/COLLABORATION DETECTED:\n")
		for _, c := range out.Collaboration.Claimants {
			fmt.Printf("    %s  session=%s  harness=%s  claimed=%s\n",
				c.ClaimID, c.SessionID, c.Harness, c.ClaimedAt)
		}
		fmt.Printf("  (warn-and-allow: work continues; coordinate manually)\n")
	}

	fmt.Printf("\n  Task tracking:   %s\n", out.TaskTracking.Detail)
	fmt.Println(sep)
	return nil
}

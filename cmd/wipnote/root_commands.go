package main

import (
	cliinternal "github.com/shakestzd/wipnote/internal/cli"
	"github.com/spf13/cobra"
)

// The root command's subcommands, by help group, in registration order.
// buildRoot (main.go) assembles them.

// initLedgerCommitSeams installs the producer seams through which canonical
// ledger writes reach the commit queue. core/claimledger and its siblings
// cannot import internal/commitqueue, so the seams are set here, where every
// CLI command and hook handler in this binary picks them up.
func initLedgerCommitSeams() {
	initClaimLedgerCommitSeam()
	initSessionLedgerCommitSeam()
	initGateLedgerCommitSeam()
	initDelegationLedgerCommitSeam()
	initRunLedgerCommitSeam()
}

// researchItemCmd is the spike or bug command with its reset, reopen,
// link-commit and research subcommands.
func researchItemCmd(typeName, dirName string, reset *cobra.Command) *cobra.Command {
	cmd := workitemCmd(typeName, dirName)
	cmd.AddCommand(reset)
	cmd.AddCommand(wiReopenCmd(typeName))
	cmd.AddCommand(linkCommitCmd(typeName))
	cmd.AddCommand(wiResearchCmd(typeName))
	return cmd
}

// grouped puts cmds in help group id.
func grouped(id string, cmds ...*cobra.Command) []cliinternal.GroupedCommand {
	out := make([]cliinternal.GroupedCommand, len(cmds))
	for i, c := range cmds {
		out[i] = cliinternal.GroupedCommand{GroupID: id, Command: c}
	}
	return out
}

// workItemCommands is the work items group.
func workItemCommands(spike, bug *cobra.Command) []cliinternal.GroupedCommand {
	return grouped("workitems",
		featureCmdWithExtras(),
		spike,
		bug,
		trackCmdWithExtras(),
		planCmdWithExtras(),
		archCmd(),
		questionCmd(),
	)
}

// queryCommands is the query group.
func queryCommands() []cliinternal.GroupedCommand {
	return grouped("query",
		recapCmd(),
		findCmd(),
		wipCmd(),
		statusCmd(),
		snapshotCmd(),
		mcpCmd(),
		linkCmd(),
		sessionCmd(),
		sessionsCmd(),
		analyticsCmd(),
		skillCmd(),
		recommendCmd(),
		relevantCmd(),
		newHistoryCmd(),
		newLineageCmd(),
		blameCmd(),
		codeAreasCmd(),
		contextPackCmd(),
		executePreviewCmd(),
		searchCmd(),
		whoCmd(),
	)
}

// qualityCommands is the quality group.
func qualityCommands() []cliinternal.GroupedCommand {
	return grouped("quality",
		checkCmd(),
		healthCmd(),
		specCmd(),
		tddCmd(),
		reviewCmd(),
		complianceCmd(),
		reconcileCmd(),
		launcherCmd(),
		doctorCmd(),
		guardCmd(),
		researchCmd(),
		ciCmd(),
	)
}

// dataCommands is the data group.
func dataCommands() []cliinternal.GroupedCommand {
	return grouped("data",
		batchCmd(),
		ingestCmd(),
		// `wipnote backfill` is gone (feat-fc3cc9e0). Both subcommands
		// (`feature-files`, `tool-calls-feature`) read `tool_calls` — a
		// table hydration never populates — and wrote to `feature_files` /
		// `tool_calls` in the per-process projection. They could only ever
		// scan zero rows and persist nothing, while printing a count. The
		// surviving capability is `wipnote reindex backfill-orphans`, which
		// derives file attribution from git and writes it to the work
		// item's canonical HTML.
		// `wipnote sweep orphaned-events` is gone (feat-fc3cc9e0). It read
		// agent_events rows still marked 'started' out of the per-project
		// SQLite file. No such file exists, and an in-flight tool call is
		// not recorded in any durable store — so a standalone CLI process
		// can never see one. It would have reported "Swept 0" forever. The
		// automatic sweeps that share the writer daemon's live projection
		// (PostToolUse per-session, SessionStart project-wide, the daemon's
		// own drain loop) are unaffected by this removal.
		reindexCmd(),
		migrateCmd(),
		migrateTracksCmd(),
		cleanCmd(),
		cleanupCmd(),
		archiveCmd(),
		claimsCmd(),
		staleCmd(),
		cacheCmd(),
		syncCmd(),
		pruneCmd(),
		registryCmd(),
		storeCmd(),
		commitQueueCmd(),
	)
}

// devCommands is the dev group.
func devCommands() []cliinternal.GroupedCommand {
	return grouped("dev",
		yoloCmd(),
		upgradeCmd(),
		buildCmd(),
		agentInitCmd(),
		shCmd(),
		delegateCmd(),
		wrapperRunCmd(),
	)
}

// ungroupedCommands are the commands outside the help groups.
func ungroupedCommands() []*cobra.Command {
	return []*cobra.Command{
		versionCmd(),
		statuslineCmd(),
		serveChildCmd(),
		otelCollectCmd(),
		hookCmd(),
		claudeCmd(),
		codexCmd(),
		antigravityCmd(),
		orchestratorCmd(),
		installHooksCmd(),
		reportCmd(),
		budgetCmd(),
		helpCmd(),
		claimCmd(),
		purgeSpikesCmd(),
		traceCmd(),
		graphCmd(),
		queryCmd(),
		devCmd(),
		pluginCmd(),
		projectsCmd(),
		initCmd(),
		setupCmd(),
		setupCLICmd(),
		shellAliasCmd(),
		pricingCmd(),
		harnessCmd(),
	}
}

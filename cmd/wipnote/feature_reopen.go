package main

import "github.com/spf13/cobra"

// featureReopenCmd creates `feature reopen`; bug and spike share the same
// verb and code path (workitem_reopen.go).
func featureReopenCmd() *cobra.Command {
	return wiReopenCmd("feature")
}

// executeFeatureReopen sets a done feature back to in-progress.
func executeFeatureReopen(featureID string) error {
	return executeReopen("feature", featureID)
}

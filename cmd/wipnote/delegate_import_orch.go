package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/importorch"
)

// delegateImportOrchCmd is `wipnote delegate import-orch`: a one-shot,
// idempotent copy of orch's run history into the delegations ledger
// (plan-28b4a7f6 slice 18). It only reads orch's files.
func delegateImportOrchCmd() *cobra.Command {
	var mailRoot string
	cmd := &cobra.Command{
		Use:   "import-orch",
		Short: "Import orch run history into the delegations ledger (one-shot, idempotent)",
		Long: `Reads orch's registry ($ORCH_REGISTRY, else ~/.cache/orch/registry.json) and its
mailboxes, archived ones included, and writes one closed row per past orch run
(end_reason=imported) to .wipnote/delegations-ledger.html. A rerun adds nothing.

orch's default mail root moved twice: --mail-root names it; otherwise
$ORCH_MAIL_ROOT, $ORCH_SHARED_ROOT/.orch/mail, ~/work/.orch/mail and
~/work/adhoc/orch-mail are probed. Event format v1 and v2 are read; any other
version is refused before anything is written.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelegateImportOrch(cmd.OutOrStdout(), mailRoot)
		},
	}
	cmd.Flags().StringVar(&mailRoot, "mail-root", "", "orch's mail root (overrides $ORCH_MAIL_ROOT and the defaults)")
	return cmd
}

func runDelegateImportOrch(out io.Writer, mailRoot string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	store := delegationledger.NewStore(wipnoteDir)
	res, err := importorch.Import(store, importorch.Options{
		RegistryPath: importorch.RegistryPath(os.Getenv, home),
		MailRoots:    importorch.MailRoots(mailRoot, os.Getenv, home),
		Explicit:     mailRoot != "",
	})
	if err != nil {
		return fmt.Errorf("import-orch: %w (imported %d before stopping)", err, len(res.Imported))
	}
	fmt.Fprintf(out, "imported %d orch runs into %s (%d already present)\n",
		len(res.Imported), store.RelPath(), res.Existing)
	reasons := make([]string, 0, len(res.Skipped))
	for r := range res.Skipped {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		fmt.Fprintf(out, "skipped %d: %s\n", res.Skipped[r], r)
	}
	return nil
}

package statusall

import "testing"

func claimRow(session, item, harness string) Agent {
	return Agent{Source: "ledger", Session: session, WorkItem: item, Harness: harness}
}

func paneRow(pane, item, harness string) Agent {
	return Agent{Source: "herdr", Pane: pane, WorkItem: item, Harness: harness}
}

// A labelled pane's own claim must not be taken by an unlabelled pane that
// sorts first and reaches the harness fallback before the label match runs.
func TestJoinLedger_LabelMatchBeatsHarnessFallback(t *testing.T) {
	rows := []Agent{paneRow("w1:p1", "", "claude"), paneRow("w1:p2", "feat-aaaa1111", "claude")}
	ledger := []Agent{claimRow("s-1", "feat-aaaa1111", "claude")}
	got := joinLedger(rows, ledger)
	if len(got) != 2 {
		t.Fatalf("want the two panes and no leftover claim, got %+v", got)
	}
	if got[1].Pane != "w1:p2" || got[1].Session != "s-1" || got[1].Source != "herdr+ledger" {
		t.Fatalf("labelled pane must join its own claim: %+v", got[1])
	}
	if got[0].Session != "" || got[0].WorkItem != "" || got[0].Source != "herdr" {
		t.Fatalf("unlabelled pane must not take the labelled pane's claim: %+v", got[0])
	}
}

func TestJoinLedger_LoneUnlabelledPaneGetsHarnessFallback(t *testing.T) {
	rows := []Agent{paneRow("w1:p1", "", "claude")}
	ledger := []Agent{claimRow("s-1", "feat-aaaa1111", "claude")}
	got := joinLedger(rows, ledger)
	if len(got) != 1 || got[0].Session != "s-1" || got[0].WorkItem != "feat-aaaa1111" {
		t.Fatalf("lone unlabelled pane must join the only claim of its harness: %+v", got)
	}
}

// The fallback runs against the claims the label pass left: with the
// labelled pane's claim taken, the unlabelled pane joins the remaining one.
func TestJoinLedger_FallbackUsesRemainingClaims(t *testing.T) {
	rows := []Agent{paneRow("w1:p1", "", "claude"), paneRow("w1:p2", "feat-aaaa1111", "claude")}
	ledger := []Agent{claimRow("s-1", "feat-aaaa1111", "claude"), claimRow("s-2", "bug-bbbb2222", "claude")}
	got := joinLedger(rows, ledger)
	if len(got) != 2 || got[0].Session != "s-2" || got[1].Session != "s-1" {
		t.Fatalf("want p1→s-2 by fallback and p2→s-1 by label, got %+v", got)
	}
}

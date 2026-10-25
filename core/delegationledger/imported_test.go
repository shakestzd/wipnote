package delegationledger

import (
	"testing"
	"time"
)

// TestLedger_ImportedRowMayLackEnd pins the slice 18 contract: orch stored no
// end time, so an imported run with no events has end_reason=imported and an
// empty ended. Such a row is closed (never counted as running by the claim
// gate), round-trips with ended empty, and a later Close leaves it alone.
func TestLedger_ImportedRowMayLackEnd(t *testing.T) {
	s := testStore(t)
	r := interactiveRow("dlg-00000000c0de", "", t0)
	r.EndReason = EndImported
	mustOpen(t, s, r)

	got, ok, err := s.Get(r.ID)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if !got.Ended.IsZero() || got.EndReason != EndImported {
		t.Fatalf("round trip: ended=%v reason=%q, want empty ended and imported", got.Ended, got.EndReason)
	}
	if got.IsOpen() {
		t.Fatal("an imported row with no end must read as closed")
	}
	open, readable := s.ReadOpen()
	if !readable {
		t.Fatal("ReadOpen: ledger unreadable")
	}
	if _, found := open.ByRunID(r.ID); found {
		t.Fatal("ReadOpen returned an imported row as open")
	}
	if err := s.Close(r.ID, CloseInfo{Ended: t0.Add(time.Hour), Reason: EndStop}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	after, _, _ := s.Get(r.ID)
	if !after.Ended.IsZero() || after.EndReason != EndImported {
		t.Fatalf("Close changed an imported row: ended=%v reason=%q", after.Ended, after.EndReason)
	}
}

// TestLedger_EndReasonWithoutEndOnlyForImported is the other side: any reason
// but imported still needs an end time, and an end time still needs a reason.
func TestLedger_EndReasonWithoutEndOnlyForImported(t *testing.T) {
	for _, reason := range []string{EndExit, EndStop, EndPaneClosed, EndGone} {
		r := interactiveRow("dlg-00000000beef", "", t0)
		r.EndReason = reason
		if err := r.Validate(); err == nil {
			t.Errorf("end_reason=%s with no ended was accepted", reason)
		}
	}
	r := interactiveRow("dlg-00000000beef", "", t0)
	r.Ended = t0.Add(time.Minute)
	if err := r.Validate(); err == nil {
		t.Error("ended with no end_reason was accepted")
	}
}

package question

import "testing"

func TestAssume(t *testing.T) {
	q, _ := New("Which?", twoOpts(), "b", false, "", "", t0)
	if err := q.Assume(); err != nil {
		t.Fatal(err)
	}
	if q.Status != StatusAssumed || q.Assumed != "b" || !q.IsOpen() {
		t.Fatalf("assume: %+v", q)
	}
	if err := q.Assume(); err == nil {
		t.Fatal("assuming an assumed question must be refused")
	}

	noRec, _ := New("Which?", twoOpts(), "", false, "", "", t0)
	if err := noRec.Assume(); err == nil || noRec.Status != StatusOpen {
		t.Fatalf("no recommended option must stay open: %v %+v", err, noRec)
	}
	blocking, _ := New("Which?", twoOpts(), "a", true, "", "", t0)
	if err := blocking.Assume(); err == nil || blocking.Status != StatusOpen || blocking.Assumed != "" {
		t.Fatalf("blocking must not be assumed: %v %+v", err, blocking)
	}
}

func TestAssumeOpen(t *testing.T) {
	mk := func(rec string, blocking bool) Question {
		q, _ := New("Which?", twoOpts(), rec, blocking, "", "", t0)
		return *q
	}
	answered := mk("a", false)
	_ = answered.RecordAnswer("a", "", t0, false)
	qs := []Question{mk("b", false), mk("", false), mk("a", true), answered}

	r := AssumeOpen(qs)
	if len(r.Assumed) != 1 || r.Assumed[0].ID != qs[0].ID || qs[0].Status != StatusAssumed {
		t.Fatalf("assumed %+v, qs[0] %+v", r.Assumed, qs[0])
	}
	if len(r.Blocking) != 1 || r.Blocking[0].ID != qs[2].ID {
		t.Fatalf("blocking %+v", r.Blocking)
	}
	if len(r.Unassumed) != 1 || r.Unassumed[0].ID != qs[1].ID || qs[1].Status != StatusOpen {
		t.Fatalf("unassumed %+v", r.Unassumed)
	}
	if qs[3].Status != StatusConfirmed {
		t.Fatalf("answered question touched: %+v", qs[3])
	}
}

func TestRecordAnswer_AgainstAssumption(t *testing.T) {
	q, _ := New("Which?", twoOpts(), "b", false, "", "", t0)
	_ = q.Assume()
	if err := q.RecordAnswer("b", "", t0, false); err != nil || q.Status != StatusConfirmed {
		t.Fatalf("same answer must confirm: %v %+v", err, q)
	}

	q2, _ := New("Which?", twoOpts(), "b", false, "", "", t0)
	_ = q2.Assume()
	if err := q2.RecordAnswer("a", "", t0, false); err != nil || q2.Status != StatusOverturned || q2.Assumed != "b" {
		t.Fatalf("different answer must overturn: %v %+v", err, q2)
	}
	if q2.IsOpen() {
		t.Fatal("an overturned question is settled, not open")
	}
	// A forced re-answer re-runs the check against the assumption.
	if err := q2.RecordAnswer("b", "", t0, true); err != nil || q2.Status != StatusConfirmed {
		t.Fatalf("forced re-answer equal to assumed must confirm: %v %+v", err, q2)
	}
	if err := q2.RecordAnswer("a", "", t0, true); err != nil || q2.Status != StatusOverturned {
		t.Fatalf("forced re-answer different from assumed must overturn: %v %+v", err, q2)
	}
}

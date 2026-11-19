package session

import "testing"

func reviewWorker(t *testing.T, e *env, name string) Spawned {
	t.Helper()
	return e.workerWith(t, name, func(o *SpawnOptions) { o.Profile = ProfileReview })
}

// bug-9f3b580f: a review worker has no shell, so its Stop saves the final
// message as the task's result, and delegate result returns it.
func TestStopReply_ReviewWorkerFinalMessageBecomesResult(t *testing.T) {
	e := newEnv(t)
	s := reviewWorker(t, e, "w1")
	if err := StopReply(e.deps, s.Run, "Found two defects."); err != nil {
		t.Fatal(err)
	}
	r, err := Result(e.deps, "w1")
	if err != nil || r.Status != ResultFound || r.Body != "Found two defects." || r.Task != e.task("w1") {
		t.Fatalf("%+v err %v", r, err)
	}
}

// Another profile's worker is not touched: its Stop writes nothing.
func TestStopReply_OtherProfileWritesNothing(t *testing.T) {
	e := newEnv(t)
	s := e.workerWith(t, "w1", func(o *SpawnOptions) { o.Profile = "standard" })
	if err := StopReply(e.deps, s.Run, "an answer"); err != nil {
		t.Fatal(err)
	}
	if r, _ := Result(e.deps, "w1"); r.Status != ResultNone {
		t.Fatalf("%+v", r)
	}
}

// A result the worker wrote for the turn stays; a later task gets its own.
func TestStopReply_KeepsExistingResultForTheTask(t *testing.T) {
	e := newEnv(t)
	s := reviewWorker(t, e, "w1")
	e.writeResult("w1", e.task("w1"), "first answer")
	if err := StopReply(e.deps, s.Run, "second answer"); err != nil {
		t.Fatal(err)
	}
	if r, _ := Result(e.deps, "w1"); r.Body != "first answer" {
		t.Fatalf("result overwritten: %+v", r)
	}
}

// An empty message writes nothing.
func TestStopReply_EmptyMessageWritesNothing(t *testing.T) {
	e := newEnv(t)
	s := reviewWorker(t, e, "w1")
	if err := StopReply(e.deps, s.Run, "  \n"); err != nil {
		t.Fatal(err)
	}
	if r, _ := Result(e.deps, "w1"); r.Status != ResultNone {
		t.Fatalf("%+v", r)
	}
}

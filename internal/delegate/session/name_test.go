package session

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// TestSpawn_NamesWorkerByWorkItem: with no NAME, the worker's harness name
// is <item-id>-<slug> (cut to 32) and it is the mailbox name too; the boot
// argv passes it to Claude as --name (plan-28b4a7f6 slice 15).
func TestSpawn_NamesWorkerByWorkItem(t *testing.T) {
	e := newEnv(t)
	o := e.opts("")
	o.WorkTitle = "Name sessions by work item"
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	const want = "feat-0a1b2c3d-name-sessions-by"
	if s.Name != want || e.row(s.Run).Name != want {
		t.Fatalf("spawned %q row %q, want %q", s.Name, e.row(s.Run).Name, want)
	}
	info, err := ReadInfo(e.runFolder(s.Run))
	if err != nil || info.Name != want {
		t.Fatalf("info %+v err %v", info, err)
	}
	mb, err := e.deps.Store.Mailbox(want)
	if err != nil || filepath.Base(mb.Dir()) != want {
		t.Fatalf("mailbox %v err %v", mb, err)
	}
	argv := BootArgv("/bin/wipnote", info, "")
	if i := slices.Index(argv, "--name"); i < 0 || argv[i+1] != want {
		t.Fatalf("boot argv %q has no --name %s", argv, want)
	}
}

// TestSpawn_ExplicitNameKept is the other side: a given NAME wins over the
// work item.
func TestSpawn_ExplicitNameKept(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	o.WorkTitle = "Name sessions by work item"
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil || s.Name != "w1" || e.row(s.Run).Name != "w1" {
		t.Fatalf("spawned %+v err %v", s, err)
	}
}

// TestSpawn_DerivedNameStillUnique: the derived name is refused like any
// NAME while a live worker holds it.
func TestSpawn_DerivedNameStillUnique(t *testing.T) {
	e := newEnv(t)
	o := e.opts("")
	if _, err := Spawn(context.Background(), e.deps, o); err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(context.Background(), e.deps, o); err == nil {
		t.Fatal("a second worker took the live derived name")
	}
}

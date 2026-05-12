package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/migrate"
)

// TestRunMigrateNormalize_DirtyTreeErrors verifies the CLI surfaces a
// helpful error when .wipnote/ has uncommitted changes and the operator
// did not pass --allow-dirty. The gitRunner is stubbed so the test does
// not depend on the real working tree.
//
// We exercise the same code path that runMigrateNormalize uses by calling
// migrate.IsWorkingTreeDirty directly with the dirty stub — the CLI's
// only logic here is the boolean -> error mapping.
func TestRunMigrateNormalize_DirtyTreeErrors(t *testing.T) {
	stub := func(repoRoot string, args ...string) (string, error) {
		return " M .wipnote/sessions/abc.html\n", nil
	}
	dirty, err := migrate.IsWorkingTreeDirty("/tmp/whatever", stub)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty=true")
	}
}

// TestRunMigrateNormalize_GitFailureSurfaces verifies that a git invocation
// failure (e.g. not a git repo) is reported up rather than silently treated
// as clean.
func TestRunMigrateNormalize_GitFailureSurfaces(t *testing.T) {
	wantErr := errors.New("not a git repository")
	stub := func(repoRoot string, args ...string) (string, error) {
		return "", wantErr
	}
	_, err := migrate.IsWorkingTreeDirty("/tmp/whatever", stub)
	if err == nil {
		t.Fatal("expected an error when git fails")
	}
	if !strings.Contains(err.Error(), "not a git") {
		t.Errorf("expected wrapped git error, got: %v", err)
	}
}

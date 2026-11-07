package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// completionBaseFixture builds a project whose origin/main holds the initial
// state, with mode-specific local history, and returns the item's id.
// scenario: "ahead" (linked commit on unpushed local main), "equal" (pushed),
// "behind" (linked commit only on an unmerged branch, local main equal to origin),
// "side" (local ahead, but the linked commit is only on a side branch).
func completionBaseFixture(t *testing.T, scenario string) (hgDir, id string, tmpDir string) {
	t.Helper()
	tmpDir, hgDir = prepProject(t)
	provGit(t, tmpDir, "branch", "-M", "main")
	seedOriginRemote(t, tmpDir)
	trackID := testSetupTrack(t, hgDir)
	id = createItem(t, hgDir, "feature", "Local Work", trackID)
	seedImplementedIn(t, hgDir, id)
	switch scenario {
	case "ahead":
		seedProvCommit(t, tmpDir, id, "internal/local.go")
	case "equal":
		seedProvCommit(t, tmpDir, id, "internal/local.go")
		provGit(t, tmpDir, "push", "-q", "origin", "main")
	case "behind":
		commitOnUnmergedBranch(t, tmpDir, id)
	case "side":
		commitOnUnmergedBranch(t, tmpDir, id)
		seedProvCommit(t, tmpDir, "other", "internal/other.go") // local main ahead
	}
	return hgDir, id, tmpDir
}

func writeCompletionBaseConfig(t *testing.T, root, value string) {
	t.Helper()
	cfg := filepath.Join(root, ".wipnote", "config.json")
	if err := os.WriteFile(cfg, []byte(`{"completion_base":"`+value+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMergedUpstreamGate_CompletionBase(t *testing.T) {
	tests := []struct {
		name, scenario, config, env string
		wantErr                     bool
	}{
		{"local ahead + auto accepts", "ahead", "", "", false},
		{"local ahead + explicit auto accepts", "ahead", "auto", "", false},
		{"local ahead + upstream refuses", "ahead", "upstream", "", true},
		{"local ahead + local accepts", "ahead", "local", "", false},
		{"pushed commit (local equal) + auto accepts", "equal", "", "", false},
		{"local equal + auto refuses unmerged branch commit", "behind", "auto", "", true},
		{"local equal + local refuses unmerged branch commit", "behind", "local", "", true},
		{"local + commit only on side branch refuses", "side", "local", "", true},
		{"auto + side branch while local ahead refuses", "side", "auto", "", true},
		{"env overrides config (env upstream)", "ahead", "local", "upstream", true},
		{"env overrides config (env local)", "ahead", "upstream", "local", false},
		{"unknown value falls back to auto", "ahead", "bogus", "", false},
		{"unknown env falls back to auto", "ahead", "upstream", "bogus", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hgDir, id, tmpDir := completionBaseFixture(t, tc.scenario)
			if tc.config != "" {
				writeCompletionBaseConfig(t, tmpDir, tc.config)
			}
			t.Setenv(completionBaseEnv, tc.env)
			p, col := openFeatureCollection(t, hgDir)
			err := checkMergedUpstreamCompleteGate(col, evidenceFor(p, col, id), "feature", id, "")
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v, got: %v", tc.wantErr, err)
			}
			if err != nil {
				wants := []string{"origin/main", "--allow-orphan", "gh pr create"}
				if !strings.Contains(err.Error(), "refs/heads/main") {
					wants = append(wants, "completion_base")
				}
				for _, want := range wants {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal should contain %q, got: %v", want, err)
					}
				}
			}
		})
	}
}

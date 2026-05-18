package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLauncherDoctor_DivergedMain(t *testing.T) {
	repoRoot := t.TempDir()
	if err := exec.Command("git", "-C", repoRoot, "init", "-q", "-b", "main").Run(); err != nil {
		t.Skip("git init failed:", err)
	}
	_ = exec.Command("git", "-C", repoRoot, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", repoRoot, "config", "user.name", "Test").Run()
	readme := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readme, []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", repoRoot, "add", "README.md").Run()
	_ = exec.Command("git", "-C", repoRoot, "commit", "-q", "-m", "initial").Run()
	dirty := filepath.Join(repoRoot, "dirty.txt")
	if err := os.WriteFile(dirty, []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := runDoctorReport(repoRoot)
	if !strings.Contains(strings.ToLower(report), "dirty") &&
		!strings.Contains(strings.ToLower(report), "uncommitted") {
		t.Errorf("expected dirty/uncommitted in report, got:\n%s", report)
	}
	if !strings.Contains(report, "worktree") && !strings.Contains(report, "commit") {
		t.Errorf("expected remediation hint in report, got:\n%s", report)
	}
}

func TestLauncherDoctor_StaleWorktree(t *testing.T) {
	repoRoot := t.TempDir()
	if err := exec.Command("git", "-C", repoRoot, "init", "-q", "-b", "main").Run(); err != nil {
		t.Skip("git init failed:", err)
	}
	_ = exec.Command("git", "-C", repoRoot, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", repoRoot, "config", "user.name", "Test").Run()
	readme := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readme, []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", repoRoot, "add", "README.md").Run()
	_ = exec.Command("git", "-C", repoRoot, "commit", "-q", "-m", "initial").Run()
	worktreePath := filepath.Join(repoRoot, ".claude", "worktrees", "feat-testXX")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", repoRoot, "worktree", "add", "-q", worktreePath, "-b", "yolo-feat-testXX").Run()
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatal(err)
	}
	report := runDoctorReport(repoRoot)
	if !strings.Contains(report, "stale") && !strings.Contains(report, "feat-testXX") {
		t.Errorf("expected stale worktree mention in report, got:\n%s", report)
	}
	if strings.Contains(strings.ToLower(report), "pruned") || strings.Contains(report, "deleted") {
		t.Errorf("doctor should be non-destructive by default, got:\n%s", report)
	}
}

func TestLegacySessionFamilyFallback(t *testing.T) {
	legacySessions := []string{"legacy-session-001", "legacy-session-002"}
	for _, sid := range legacySessions {
		label := doctorSessionLabel(sid, map[string]string{})
		if label != "legacy" {
			t.Errorf("session %q without family should be labeled legacy, got %q", sid, label)
		}
	}
	familyMap := map[string]string{"session-with-family": "fam-abc123"}
	label := doctorSessionLabel("session-with-family", familyMap)
	if label == "legacy" {
		t.Errorf("session with family should not be labeled legacy")
	}
}

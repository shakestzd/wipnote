package pluginbuild

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestWorkerRulesDisposition checks that every section of the two skills the
// worker rules block replaces has a recorded disposition in
// docs/design/worker-rules.md (feat-0d9b57bc). Once slice 4 deletes a skill,
// its file is gone and its check is skipped.
func TestWorkerRulesDisposition(t *testing.T) {
	root := repositoryRootForTest(t)
	doc, err := os.ReadFile(filepath.Join(root, "docs", "design", "worker-rules.md"))
	if err != nil {
		t.Fatalf("disposition record: %v", err)
	}
	heading := regexp.MustCompile(`(?m)^#{2,3} (.+)$`)
	for _, skill := range []string{"agent-context", "code-quality-skill"} {
		src, err := os.ReadFile(filepath.Join(root, "plugin", "skills", skill, "SKILL.md"))
		if err != nil {
			continue
		}
		for _, m := range heading.FindAllSubmatch(src, -1) {
			row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(skill) + ` \| ` + regexp.QuoteMeta(string(m[1])) + ` \| (in the block|printed by [^|]+|dropped: [^|]+) \|`)
			if !row.Match(doc) {
				t.Errorf("no disposition row for %s / %q", skill, m[1])
			}
		}
	}
}

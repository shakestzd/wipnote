package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Claude Code's hooks loader follows `$` (the EngineInterface) only into a function declared in
// the same file, never across an import (bug-3dfbf293, #350). So every `$` call in the
// wipnote-orchestra mod stays in hooks/register.tsx, and the other files under hooks/ are pure.
func TestOrchestraHooksKeepDollarInRegister(t *testing.T) {
	dir := filepath.Join("..", "..", "mods", "wipnote-orchestra", "hooks")
	block := regexp.MustCompile(`(?s)/\*.*?\*/`)
	line := regexp.MustCompile(`(?m)^\s*//.*$`)
	engine := regexp.MustCompile(`\bEngineInterface\b`)
	param := regexp.MustCompile(`[(,]\s*\$\s*[,:)=?]|(?:^|[^\w$.])\$\s*=>`)
	call := regexp.MustCompile("(?:^|[^\\w`$])\\$\\.[a-z]")
	checked := 0
	for _, pat := range []string{"*.ts", "*.tsx"} {
		files, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			name := filepath.Base(file)
			if name == "register.tsx" {
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			src := line.ReplaceAllString(block.ReplaceAllString(string(raw), ""), "")
			checked++
			for _, c := range []struct {
				re   *regexp.Regexp
				what string
			}{
				{engine, "mentions EngineInterface"},
				{param, "declares a parameter named $"},
				{call, "calls into $"},
			} {
				if c.re.MatchString(src) {
					t.Errorf("hooks/%s %s; keep every $ use in register.tsx", name, c.what)
				}
			}
		}
	}
	if checked < 5 || strings.Contains(dir, "\x00") {
		t.Fatalf("checked %d helper files under %s, want the mod's hooks", checked, dir)
	}
}

package render

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

var pageGroupRe = regexp.MustCompile(`data-group="([a-z_]+)"`)

// TestGroups_BandListPageAgree: for the same runs, list, run.html and the
// fixture the orchestra band's runs.test.ts reads all carry one group (the
// band's side is asserted there, against GROUPS).
func TestGroups_BandListPageAgree(t *testing.T) {
	project := t.TempDir()
	groups := writeFixtureRuns(t, project)
	want := map[string]session.Group{
		"dlg-0a0000000001": session.GroupResolved,     // one-shot, exit 0
		"dlg-0a0000000002": session.GroupWorking,      // one-shot, live
		"dlg-0a0000000003": session.GroupWaitingOnYou, // worker asks a question
		"dlg-0a0000000004": session.GroupWaitingOnYou, // one-shot, exit 2
		"dlg-0a0000000005": session.GroupResolved,     // worker stopped
		"dlg-0a0000000007": session.GroupWorking,      // open row, folder swept
	}
	if !maps.Equal(groups, want) {
		t.Fatalf("list groups %v, want %v", groups, want)
	}
	pages := 0
	for id, g := range groups {
		html, err := os.ReadFile(filepath.Join(project, ".wipnote", "delegate", "runs", id, runstore.FileRunHTML))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if m := pageGroupRe.FindSubmatch(html); m == nil || session.Group(m[1]) != g {
			t.Fatalf("%s: run.html group %q, list says %s", id, m, g)
		}
	}
	if pages != 2 {
		t.Fatalf("checked %d pages, want the two finished one-shot runs", pages)
	}
	fixture, err := os.ReadFile(orchestraFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixture), "export const GROUPS: Record<string, string> = "+jsonOf(t, groups)+"\n") {
		t.Fatal("the band's fixture does not carry list's groups (WIPNOTE_UPDATE_FIXTURES=1 rewrites it)")
	}
}

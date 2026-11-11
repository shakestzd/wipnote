package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/graph"
)

// withQuestionStatus adds a questions property holding one question with the
// given status to a done feature written by writeDoneFeature.
func withQuestionStatus(t *testing.T, wipnoteDir, id, props string) {
	t.Helper()
	path := filepath.Join(wipnoteDir, "features", id+".html")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(raw), `data-status="done"`, `data-status="done" data-node-props="`+props+`"`, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func questionProps(status string) string {
	return `{&#34;questions&#34;:[{&#34;id&#34;:&#34;q-0123456789abcdef&#34;,&#34;text&#34;:&#34;t&#34;,&#34;options&#34;:[],&#34;status&#34;:&#34;` + status + `&#34;}]}`
}

// Finding 2: archive keeps live a done item with an open or assumed
// question, or one whose questions cannot be read, and says so; an item
// whose questions are all settled is archived as before.
func TestArchive_KeepsItemsWithUnsettledQuestions(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	age := 60 * 24 * time.Hour
	keep := map[string]string{
		"feat-open0001": questionProps("open"),
		"feat-asmd0001": questionProps("assumed"),
		"feat-corr0001": `{&#34;questions&#34;:&#34;not json&#34;}`,
	}
	for id, props := range keep {
		writeDoneFeature(t, wipnoteDir, id, "Kept", age, "")
		withQuestionStatus(t, wipnoteDir, id, props)
	}
	writeDoneFeature(t, wipnoteDir, "feat-conf0001", "Settled", age, "")
	withQuestionStatus(t, wipnoteDir, "feat-conf0001", questionProps("confirmed"))
	commitAll(t, repoRoot)

	out := captureStdout(t, func() {
		if err := runArchive(true, defaultArchiveAgeDays); err != nil {
			t.Fatalf("apply: %v", err)
		}
	})
	entries, err := graph.ReadLedger(graph.ArchiveLedgerPath(wipnoteDir, "features"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "feat-conf0001" {
		t.Fatalf("only the settled item should be archived, got %+v", entries)
	}
	for id := range keep {
		if !fileExists(t, filepath.Join(wipnoteDir, "features", id+".html")) {
			t.Errorf("%s was archived with an unsettled question", id)
		}
		if !strings.Contains(out, id) {
			t.Errorf("output does not name kept item %s:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "open question") {
		t.Errorf("output does not say why items were kept:\n%s", out)
	}
}

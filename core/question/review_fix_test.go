package question

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/models"
)

// A question id carries 64 bits of randomness: q- and 16 hex characters.
func TestNewID_SixtyFourBits(t *testing.T) {
	re := regexp.MustCompile(`^q-[0-9a-f]{16}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if !re.MatchString(id) {
			t.Fatalf("id %q is not q- and 16 hex", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

// Parse tells an absent property from one that cannot be read; FromNode
// stays lenient for read-only views.
func TestParse_AbsentVersusCorrupt(t *testing.T) {
	absent := &models.Node{ID: "feat-1", Properties: map[string]any{}}
	if qs, err := Parse(absent); err != nil || qs != nil {
		t.Fatalf("absent: %v %v", qs, err)
	}
	if qs, err := Parse(nil); err != nil || qs != nil {
		t.Fatalf("nil node: %v %v", qs, err)
	}
	for name, raw := range map[string]any{
		"bad json string": `[{"id":`,
		"wrong shape":     map[string]any{"id": "q-1"},
		"number":          42,
	} {
		n := &models.Node{ID: "feat-1", Properties: map[string]any{PropQuestions: raw}}
		if _, err := Parse(n); err == nil {
			t.Errorf("%s: Parse must fail", name)
		}
		if FromNode(n) != nil {
			t.Errorf("%s: FromNode must stay lenient", name)
		}
	}
	good := &models.Node{Properties: map[string]any{PropQuestions: Property([]Question{{ID: "q-1", Status: StatusOpen}})}}
	if qs, err := Parse(good); err != nil || len(qs) != 1 {
		t.Fatalf("good: %v %v", qs, err)
	}
}

// Option keys are [A-Za-z0-9_-]+ and do not start with '-', so the pane can
// pass them as one argument and the CLI never reads one as a flag.
func TestNew_RestrictsOptionKeys(t *testing.T) {
	for _, bad := range []string{"a b", "--force", "-x", "--text", "é", "a/b", "a\x1b"} {
		opts := []Option{{Key: bad, Label: "Bad"}, {Key: "ok", Label: "Ok"}}
		if _, err := New("Q?", opts, "", false, "agent", "", t0); err == nil || !strings.Contains(err.Error(), "key") {
			t.Errorf("key %q accepted (%v)", bad, err)
		}
	}
	opts := []Option{{Key: "a_1", Label: "A"}, {Key: "B-2", Label: "B"}}
	if _, err := New("Q?", opts, "", false, "agent", "", t0); err != nil {
		t.Fatalf("valid keys refused: %v", err)
	}
}

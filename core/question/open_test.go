package question

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const openNodeHTML = `<article id="feat-aaaa0001" data-type="feature" data-status="todo" data-node-props="{&#34;questions&#34;:[{&#34;id&#34;:&#34;q-1&#34;,&#34;text&#34;:&#34;t&#34;,&#34;options&#34;:[],&#34;status&#34;:&#34;%s&#34;}]}"><header><h1>T</h1></header></article>`

func writeItem(t *testing.T, dir, kind, id, body string) {
	t.Helper()
	d := filepath.Join(dir, ".wipnote", kind)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, id+".html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenIDsInProgress(t *testing.T) {
	dir := t.TempDir()
	if got := OpenIDsInProgress(dir); len(got) != 0 {
		t.Fatalf("empty project: %v", got)
	}
	writeItem(t, dir, "features", "feat-aaaa0001", itemWith("in-progress", "open"))
	writeItem(t, dir, "bugs", "bug-bbbb0002", `<article id="bug-bbbb0002" data-type="bug"><header><h1>B</h1></header></article>`)
	if got, want := OpenIDsInProgress(dir), []string{"q-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	cases := map[string][2]string{
		"confirmed":           {"in-progress", "confirmed"},
		"assumed on purpose":  {"in-progress", "assumed"},
		"open on a todo item": {"todo", "open"},
		"open on a done item": {"done", "open"},
		"open on blocked":     {"blocked", "open"},
	}
	for name, c := range cases {
		writeItem(t, dir, "features", "feat-aaaa0001", itemWith(c[0], c[1]))
		if got := OpenIDsInProgress(dir); len(got) != 0 {
			t.Errorf("%s counted: %v", name, got)
		}
	}
}

// itemWith is openNodeHTML with the item status and the question status set.
func itemWith(itemStatus, qStatus string) string {
	return strings.Replace(sprintfStatus(qStatus), `data-status="todo"`, `data-status="`+itemStatus+`"`, 1)
}

func sprintfStatus(s string) string {
	b := []byte(openNodeHTML)
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); i++ {
		if i+2 <= len(b) && string(b[i:i+2]) == "%s" {
			out = append(out, s...)
			i++
			continue
		}
		out = append(out, b[i])
	}
	return string(out)
}

package question

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestOpenIDs(t *testing.T) {
	dir := t.TempDir()
	if got := OpenIDs(dir); len(got) != 0 {
		t.Fatalf("empty project: %v", got)
	}
	writeItem(t, dir, "features", "feat-aaaa0001", sprintfStatus("open"))
	writeItem(t, dir, "bugs", "bug-bbbb0002", `<article id="bug-bbbb0002" data-type="bug"><header><h1>B</h1></header></article>`)
	if got, want := OpenIDs(dir), []string{"q-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	writeItem(t, dir, "features", "feat-aaaa0001", sprintfStatus("confirmed"))
	if got := OpenIDs(dir); len(got) != 0 {
		t.Fatalf("confirmed counted: %v", got)
	}
	writeItem(t, dir, "features", "feat-aaaa0001", sprintfStatus("assumed"))
	if got := OpenIDs(dir); len(got) != 1 {
		t.Fatalf("assumed not counted: %v", got)
	}
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

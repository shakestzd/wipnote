package question

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/htmlparse"
)

// itemDirs are the .wipnote folders whose items can carry questions.
var itemDirs = []string{"features", "bugs", "spikes"}

// propMarkers are the two spellings of the questions key in an item file: the
// attribute escape (&#34;) the writer emits and plain quotes.
var propMarkers = [][]byte{[]byte("questions&#34;"), []byte(`"questions"`)}

// OpenIDs returns the ids of every open or assumed question across the
// project's features, bugs and spikes. It is built for a hot path: a file that
// does not mention the questions key is skipped on a byte scan, without a
// parse, so a project with no question records costs one read per item.
func OpenIDs(projectDir string) []string {
	var ids []string
	for _, kind := range itemDirs {
		paths, _ := filepath.Glob(filepath.Join(projectDir, ".wipnote", kind, "*.html"))
		for _, p := range paths {
			ids = append(ids, openIDsInFile(p)...)
		}
	}
	return ids
}

func openIDsInFile(path string) []string {
	if !articleMentionsQuestions(path) {
		return nil
	}
	n, err := htmlparse.ParseFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	for _, q := range FromNode(n) {
		if q.IsOpen() {
			ids = append(ids, q.ID)
		}
	}
	return ids
}

func mentionsQuestions(data []byte) bool {
	for _, m := range propMarkers {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// articleMentionsQuestions reads only up to the end of the item's <article>
// open tag, where the properties attribute lives, so a file is not read whole
// to learn that it has no questions.
func articleMentionsQuestions(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head []byte
	buf := make([]byte, 4096)
	for len(head) < maxArticleHead {
		n, err := f.Read(buf)
		head = append(head, buf[:n]...)
		if i := bytes.Index(head, []byte("<article")); i >= 0 && bytes.IndexByte(head[i:], '>') >= 0 {
			break
		}
		if err != nil {
			break
		}
	}
	return mentionsQuestions(head)
}

// maxArticleHead bounds how much of a file is read looking for the tag end.
const maxArticleHead = 1 << 20

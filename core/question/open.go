package question

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// itemDirs are the .wipnote folders whose items can carry questions.
var itemDirs = []string{"features", "bugs", "spikes"}

// propMarkers are the two spellings of the questions key in an item file: the
// attribute escape (&#34;) the writer emits and plain quotes.
var propMarkers = [][]byte{[]byte("questions&#34;"), []byte(`"questions"`)}

// inProgressMarker is the item status attribute the writer emits for an
// in-progress item; the parse confirms it.
var inProgressMarker = []byte(`data-status="in-progress"`)

// OpenIDsInProgress returns the ids of the questions still awaiting the owner
// on in-progress features, bugs and spikes: status open only, since an
// assumed question is work that went ahead on purpose. It is built for a hot
// path: a file whose <article> tag does not mention the questions key and the
// in-progress status is skipped on a byte scan, without a parse, so a project
// with no such records costs one bounded read per item.
func OpenIDsInProgress(projectDir string) []string {
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
	if err != nil || n.Status != models.StatusInProgress {
		return nil
	}
	var ids []string
	for _, q := range FromNode(n) {
		if q.Status == StatusOpen {
			ids = append(ids, q.ID)
		}
	}
	return ids
}

func mentionsQuestions(data []byte) bool {
	if !bytes.Contains(data, inProgressMarker) {
		return false
	}
	for _, m := range propMarkers {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// articleMentionsQuestions reads only up to the end of the item's <article>
// open tag, where the status and properties attributes live, so a file is not
// read whole to learn that it has no questions or is not in progress.
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

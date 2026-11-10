package question

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// itemDirs maps the id prefixes of the items that can carry questions to
// their .wipnote folders.
var itemDirs = map[string]string{"feat-": "features", "bug-": "bugs", "spk-": "spikes"}

// propMarkers are the two spellings of the questions key in an item file: the
// attribute escape (&#34;) the writer emits and plain quotes.
var propMarkers = [][]byte{[]byte("questions&#34;"), []byte(`"questions"`)}

// inProgressMarker is the item status attribute the writer emits for an
// in-progress item; the parse confirms it.
var inProgressMarker = []byte(`data-status="in-progress"`)

// OpenIDsOnItems returns the ids of the questions still awaiting the owner on
// the named features, bugs and spikes that are in progress: status open only,
// since an assumed question is work that went ahead on purpose. Only the named
// items' files are read, and a file whose <article> tag does not mention the
// questions key and the in-progress status is skipped without a parse.
// complete is false when the deadline passed before every item was read; the
// ids found so far are returned with it.
func OpenIDsOnItems(projectDir string, itemIDs []string, deadline time.Time) (ids []string, complete bool) {
	for _, id := range itemIDs {
		if !time.Now().Before(deadline) {
			return ids, false
		}
		if p := itemPath(projectDir, id); p != "" {
			ids = append(ids, openIDsInFile(p)...)
		}
	}
	return ids, true
}

// itemPath is the file of a feature, bug or spike id, or "" for any other id.
func itemPath(projectDir, id string) string {
	if strings.ContainsAny(id, `/\`) {
		return ""
	}
	for prefix, dir := range itemDirs {
		if strings.HasPrefix(id, prefix) {
			return filepath.Join(projectDir, ".wipnote", dir, id+".html")
		}
	}
	return ""
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

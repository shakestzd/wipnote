package question

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// OpenIDsOnItems reads only the named items, counts open questions on the
// in-progress ones, and reports an incomplete scan when the deadline passed.
func TestOpenIDsOnItems(t *testing.T) {
	dir := t.TempDir()
	future := time.Now().Add(time.Minute)
	writeItem(t, dir, "features", "feat-aaaa0001", itemWith("in-progress", "open"))
	writeItem(t, dir, "bugs", "bug-bbbb0002", replaceIDs(itemWith("in-progress", "open"), "bug-bbbb0002", "q-2"))
	writeItem(t, dir, "features", "feat-cccc0003", replaceIDs(itemWith("done", "open"), "feat-cccc0003", "q-3"))

	got, complete := OpenIDsOnItems(dir, []string{"feat-aaaa0001"}, future)
	if !complete || !reflect.DeepEqual(got, []string{"q-1"}) {
		t.Fatalf("one item: %v %v", got, complete)
	}
	got, complete = OpenIDsOnItems(dir, []string{"bug-bbbb0002", "feat-cccc0003", "feat-missing", "trk-x"}, future)
	if !complete || !reflect.DeepEqual(got, []string{"q-2"}) {
		t.Fatalf("mixed items: %v %v", got, complete)
	}
	if got, complete = OpenIDsOnItems(dir, nil, future); !complete || got != nil {
		t.Fatalf("no items: %v %v", got, complete)
	}
	if _, complete = OpenIDsOnItems(dir, []string{"feat-aaaa0001"}, time.Now().Add(-time.Second)); complete {
		t.Fatal("a past deadline must report an incomplete scan")
	}
}

func replaceIDs(body, itemID, qid string) string {
	return strings.NewReplacer("feat-aaaa0001", itemID, "q-1", qid).Replace(body)
}

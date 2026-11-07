package hooks

import "testing"

// An app-owned store's path has .wipnote/ in it twice. The last element decides,
// so a runtime path stays writable and canonical content stays guarded.
func TestIsWipnoteStorePath_AppOwnedStore(t *testing.T) {
	const store = "/u/.wipnote/projects/-u-repo/.wipnote/"
	if isWipnoteStorePath(store + "logs/progress/feat-1.md") {
		t.Error("a runtime path in an app-owned store was treated as a store target")
	}
	if !isWipnoteStorePath(store + "features/feat-1.html") {
		t.Error("a work item in an app-owned store was not guarded")
	}
	if !isWipnoteStorePath("/u/.wipnote/projects/-u-repo/project.json") {
		t.Error("project.json was not guarded")
	}
	if !isWipnoteStorePath("/repo/.wipnote/features/feat-1.html") {
		t.Error("an in-repo work item was not guarded")
	}
	if isWipnoteStorePath("/repo/.wipnote/logs/progress/feat-1.md") {
		t.Error("an in-repo runtime path was treated as a store target")
	}
}

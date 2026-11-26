package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func madeHereStore(t *testing.T) (owner, project string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	project = filepath.Join(home, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	// Its own repository: a temp folder inside another work tree would
	// otherwise key the store to that work tree's root.
	if _, err := gitOutput(project, "init", "--quiet"); err != nil {
		t.Fatal(err)
	}
	owner, _, err := CreateExternalStore(project, OriginLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}
	return owner, project
}

func setMachineID(t *testing.T, id string) {
	t.Helper()
	old := machineIDFn
	machineIDFn = func() string { return id }
	t.Cleanup(func() { machineIDFn = old })
}

// A store restored to another machine with the same user name and project path
// passes the path check. The machine mark tells it apart.
func TestStoreMadeHere_AnotherMachinesStoreIsNotMadeHere(t *testing.T) {
	setMachineID(t, "machine-a")
	owner, _ := madeHereStore(t)
	if pf, _ := readProjectFile(owner); pf.MadeOn == "" || pf.MadeOn == "machine-a" {
		t.Fatalf("made_on %q: want a hash of the machine id", pf.MadeOn)
	}
	if !StoreMadeHere(owner) {
		t.Fatal("a store made here was not seen as made here")
	}
	setMachineID(t, "machine-b")
	if StoreMadeHere(owner) {
		t.Fatal("a store made on another machine passed as made here")
	}
}

// A store with no mark, made before the mark existed or where the system gives
// no machine id, is judged by the path check alone.
func TestStoreMadeHere_NoMarkFallsBackToThePathCheck(t *testing.T) {
	setMachineID(t, "")
	owner, project := madeHereStore(t)
	if pf, _ := readProjectFile(owner); pf.MadeOn != "" {
		t.Fatalf("made_on %q with no machine id", pf.MadeOn)
	}
	setMachineID(t, "machine-a")
	if !StoreMadeHere(owner) {
		t.Fatal("a store with no mark failed the path check")
	}
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if StoreMadeHere(owner) {
		t.Fatal("a store whose code root is gone passed as made here")
	}
}

// MarkStoreExplicit rewrites project.json and must keep the mark.
func TestStoreMadeHere_MarkSurvivesMarkStoreExplicit(t *testing.T) {
	setMachineID(t, "machine-a")
	owner, _ := madeHereStore(t)
	before, _ := readProjectFile(owner)
	if _, err := MarkStoreExplicit(owner); err != nil {
		t.Fatal(err)
	}
	if after, _ := readProjectFile(owner); after.MadeOn != before.MadeOn || after.Origin != OriginInit {
		t.Fatalf("project file after: %+v", after)
	}
}

// Each failed check has its own reason, and a store made here has none.
func TestStoreNotMadeHere_NamesTheFailedCheck(t *testing.T) {
	setMachineID(t, "machine-a")
	owner, project := madeHereStore(t)
	if why := StoreNotMadeHere(owner); why != "" {
		t.Fatalf("a store made here: %q", why)
	}
	setMachineID(t, "machine-b")
	if why := StoreNotMadeHere(owner); !strings.Contains(why, "machine mark") {
		t.Fatalf("another machine's mark: %q", why)
	}
	setMachineID(t, "machine-a")
	moved := filepath.Join(t.TempDir(), "moved-store")
	if err := os.Rename(owner, moved); err != nil {
		t.Fatal(err)
	}
	if why := StoreNotMadeHere(moved); !strings.Contains(why, "not at the path") {
		t.Fatalf("a moved store: %q", why)
	}
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if why := StoreNotMadeHere(moved); !strings.Contains(why, "not a project folder") {
		t.Fatalf("a missing project folder: %q", why)
	}
	if why := StoreNotMadeHere(t.TempDir()); !strings.Contains(why, "project.json") {
		t.Fatalf("no project file: %q", why)
	}
}

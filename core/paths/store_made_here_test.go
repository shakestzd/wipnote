package paths

import (
	"os"
	"path/filepath"
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

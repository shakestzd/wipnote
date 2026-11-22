package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// Every folder the port-drift guard watches must exist, so a deleted source
// folder cannot linger in the list.
func TestGeneratorInputDirsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, generatorInputManifest)); err != nil {
		t.Skipf("not inside the repo tree: %v", err)
	}
	for _, d := range generatorInputDirs {
		if info, err := os.Stat(filepath.Join(root, d)); err != nil || !info.IsDir() {
			t.Errorf("generatorInputDirs names %s, which is not a folder: %v", d, err)
		}
	}
}

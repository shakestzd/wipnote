package paths

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProjectConfigPath is the project config file, <projectDir>/.wipnote/config.json.
func ProjectConfigPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", "config.json")
}

// ReadProjectConfig decodes the project config file into v, which is
// usually a struct holding only the keys the caller reads. It reports false
// when the file is missing, unreadable or not valid JSON; v may then be
// partly filled and the caller should use its defaults. An empty projectDir
// is not special-cased: callers that must not read the cwd check it first.
func ReadProjectConfig(projectDir string, v any) bool {
	data, err := os.ReadFile(ProjectConfigPath(projectDir))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

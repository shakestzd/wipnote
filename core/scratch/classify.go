package scratch

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// kindByExt maps a lower-case file extension to its kind. The document
// extensions are the authored kinds (owner decision, plan-13a9e0c8).
var kindByExt = map[string]string{
	".md": KindDocument, ".txt": KindDocument, ".html": KindDocument, ".qmd": KindDocument, ".rst": KindDocument,

	".sh": KindScript, ".bash": KindScript, ".zsh": KindScript, ".py": KindScript, ".go": KindScript,
	".js": KindScript, ".mjs": KindScript, ".cjs": KindScript, ".ts": KindScript, ".tsx": KindScript,
	".jsx": KindScript, ".rb": KindScript, ".pl": KindScript, ".lua": KindScript, ".sql": KindScript,
	".ps1": KindScript, ".rs": KindScript, ".c": KindScript, ".h": KindScript, ".cpp": KindScript,
	".java": KindScript, ".swift": KindScript, ".kt": KindScript, ".r": KindScript,

	".json": KindData, ".jsonl": KindData, ".ndjson": KindData, ".csv": KindData, ".tsv": KindData,
	".yaml": KindData, ".yml": KindData, ".toml": KindData, ".xml": KindData, ".parquet": KindData,
	".sqlite": KindData, ".db": KindData,

	".log": KindLog,

	".png": KindImage, ".jpg": KindImage, ".jpeg": KindImage, ".gif": KindImage, ".svg": KindImage,
	".webp": KindImage, ".bmp": KindImage, ".ico": KindImage, ".heic": KindImage, ".tiff": KindImage,
}

// kindOf is the kind of the file at the slash-separated path rel.
func kindOf(rel string) string {
	if k, ok := kindByExt[strings.ToLower(path.Ext(rel))]; ok {
		return k
	}
	return KindOther
}

// treeMarkers are the entries that make a folder a tree: a clone, a build, a
// copied wipnote store or a tool's home folder, never an authored draft. The
// store and home markers come from the first live pass, which saved a copied
// store's pages and a test Codex home's skill files as drafts.
var treeMarkers = []string{
	".git", "go.mod", "package.json", "pyproject.toml", "node_modules",
	// a project with a store, and a store itself
	".wipnote", "sessions-ledger.html", "delegations-ledger.html", "gate-ledger.html",
	// a home folder of Codex or Claude Code, or a folder used as HOME
	".codex", ".claude", ".claude.json", "installation_id", ".codex-global-state.json",
}

// treeNames are folders that are a tree by name alone.
var treeNames = map[string]bool{".git": true, "node_modules": true, ".wipnote": true, ".codex": true, ".claude": true}

// isTree reports whether the folder dir, named name, is a tree: it holds a
// marker, or its name is one.
func isTree(dir, name string) bool {
	if treeNames[name] {
		return true
	}
	for _, m := range treeMarkers {
		if _, err := os.Lstat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// isBinary reports whether data holds a NUL byte, the same test git uses.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

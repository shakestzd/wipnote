package main

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/slug"
)

// traceFeatureJSON is the structured JSON output schema for feature trace.
type traceFeatureJSON struct {
	Feature  string   `json:"feature"`
	Commits  []string `json:"commits"`
	Sessions []string `json:"sessions"`
	Files    []string `json:"files"`
}

// uniqueSessions returns the union of session IDs drawn from both git_commits
// and feature_files, preserving insertion order and dropping empties. Sessions
// that touched a feature through files without producing a commit are included.
func uniqueSessions(commits []models.GitCommit, files []models.FeatureFile) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range commits {
		if c.SessionID != "" && !seen[c.SessionID] {
			seen[c.SessionID] = true
			out = append(out, c.SessionID)
		}
	}
	for _, f := range files {
		if f.SessionID != "" && !seen[f.SessionID] {
			seen[f.SessionID] = true
			out = append(out, f.SessionID)
		}
	}
	return out
}

// filesByFeature loads a feature's feature_files rows under RetryOnBusy.
// bug-7dbaf552: ListFilesByFeature fully materialises its slice and closes
// its own *sql.Rows before returning, so re-running the whole call on a
// transient SQLITE_BUSY leaks no read lock. Shared by traceFeatureData and
// the --tests-only feature trace, so both paths see the same rows.
func filesByFeature(database *sql.DB, featureID string) ([]models.FeatureFile, error) {
	var files []models.FeatureFile
	if err := dbpkg.RetryOnBusy(dbpkg.DefaultBusyBackoff, func() error {
		f, derr := dbpkg.ListFilesByFeature(database, featureID)
		if derr != nil {
			return derr
		}
		files = f
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

// traceFeatureData loads a feature's commits and files, each fetch wrapped in
// RetryOnBusy. bug-7dbaf552: GetCommitsByFeature fully materialises its slice
// and closes its own *sql.Rows before returning, so re-running it on a
// transient SQLITE_BUSY leaks no read lock. Shared by the text and JSON
// feature-trace renderers (DRY).
func traceFeatureData(database *sql.DB, featureID string) ([]models.GitCommit, []models.FeatureFile, error) {
	var commits []models.GitCommit
	if err := dbpkg.RetryOnBusy(dbpkg.DefaultBusyBackoff, func() error {
		c, derr := dbpkg.GetCommitsByFeature(database, featureID)
		if derr != nil {
			return derr
		}
		commits = c
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("get commits: %w", err)
	}

	files, err := filesByFeature(database, featureID)
	if err != nil {
		return nil, nil, err
	}
	return commits, files, nil
}

// filterTestFiles returns the subset of files whose path looks like a test
// file (see isTestFilePath). Order is preserved from the input.
func filterTestFiles(files []models.FeatureFile) []models.FeatureFile {
	out := make([]models.FeatureFile, 0, len(files))
	for _, f := range files {
		if isTestFilePath(f.FilePath) {
			out = append(out, f)
		}
	}
	return out
}

// testFilesSemanticsNote documents that feature_files — and therefore this
// view — is touched-by, not owned-by. UNIQUE(feature_id, file_path) makes
// the table many-to-many by design (cmd/wipnote/sqlite_write_boundary_test.go
// carries a single file under 10 distinct feature IDs), so the same test
// file can legitimately be linked to several work items. This answers
// "which test files did this item touch," never "which tests verify this
// item" — there is no per-test-function or pass/fail data in this table.
const testFilesSemanticsNote = "Note: touched-by, not owned-by. feature_files links a file to every work " +
	"item whose session/commit touched it (many-to-many), so this lists test files this item touched — " +
	"not tests that verify this item's behavior."

// runTraceFeature writes a human-readable text tree for a feature's forward trace.
func runTraceFeature(w io.Writer, database *sql.DB, featureID string) error {
	commits, files, err := traceFeatureData(database, featureID)
	if err != nil {
		return err
	}

	sessions := uniqueSessions(commits, files)

	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Trace: %s\n", featureID)
	fmt.Fprintln(w, sep)

	fmt.Fprintf(w, "\n  Commits (%d):\n", len(commits))
	for _, c := range commits {
		fmt.Fprintf(w, "    %s", slug.Truncate(c.CommitHash, 10))
		if c.Message != "" {
			fmt.Fprintf(w, "  %s", slug.Truncate(c.Message, 48))
		}
		fmt.Fprintln(w)
		if c.SessionID != "" {
			fmt.Fprintf(w, "      Session: %s\n", c.SessionID)
		}
	}

	fmt.Fprintf(w, "\n  Sessions (%d):\n", len(sessions))
	for _, sid := range sessions {
		fmt.Fprintf(w, "    %s\n", sid)
	}

	fmt.Fprintf(w, "\n  Files (%d):\n", len(files))
	for _, f := range files {
		fmt.Fprintf(w, "    %s  [%s]\n", f.FilePath, f.Operation)
	}

	return nil
}

// runTraceFeatureJSON writes structured JSON for a feature's forward trace.
func runTraceFeatureJSON(w io.Writer, database *sql.DB, featureID string) error {
	commits, files, err := traceFeatureData(database, featureID)
	if err != nil {
		return err
	}

	sessions := uniqueSessions(commits, files)

	commitHashes := make([]string, 0, len(commits))
	for _, c := range commits {
		commitHashes = append(commitHashes, c.CommitHash)
	}

	filePaths := make([]string, 0, len(files))
	for _, f := range files {
		filePaths = append(filePaths, f.FilePath)
	}

	out := traceFeatureJSON{
		Feature:  featureID,
		Commits:  commitHashes,
		Sessions: sessions,
		Files:    filePaths,
	}

	return writeJSON(w, out)
}

// runTraceFeatureTests writes a human-readable list of the test files a work
// item touched — the same feature_files rows runTraceFeature lists under
// "Files", filtered to test-looking paths. See testFilesSemanticsNote for why
// this is touched-by, not owned-by.
func runTraceFeatureTests(w io.Writer, database *sql.DB, featureID string) error {
	files, err := filesByFeature(database, featureID)
	if err != nil {
		return err
	}
	testFiles := filterTestFiles(files)

	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Trace: %s (test files)\n", featureID)
	fmt.Fprintln(w, sep)

	fmt.Fprintf(w, "\n  Test files (%d):\n", len(testFiles))
	for _, f := range testFiles {
		fmt.Fprintf(w, "    %s  [%s]\n", f.FilePath, f.Operation)
	}

	fmt.Fprintf(w, "\n  %s\n", testFilesSemanticsNote)
	return nil
}

// traceFeatureTestsJSON is the structured JSON output schema for a
// work item's --tests trace.
type traceFeatureTestsJSON struct {
	Feature   string             `json:"feature"`
	TestFiles []traceTestFileHit `json:"test_files"`
	Semantics string             `json:"semantics"`
}

type traceTestFileHit struct {
	FilePath  string `json:"file_path"`
	Operation string `json:"operation"`
	SessionID string `json:"session_id,omitempty"`
}

// runTraceFeatureTestsJSON writes structured JSON for a work item's touched
// test files. The semantics field carries testFilesSemanticsNote so
// automation consuming --json can't miss the touched-by-not-owned-by caveat
// the way it could if the note were print-only.
func runTraceFeatureTestsJSON(w io.Writer, database *sql.DB, featureID string) error {
	files, err := filesByFeature(database, featureID)
	if err != nil {
		return err
	}
	testFiles := filterTestFiles(files)

	out := traceFeatureTestsJSON{
		Feature:   featureID,
		TestFiles: make([]traceTestFileHit, 0, len(testFiles)),
		Semantics: testFilesSemanticsNote,
	}
	for _, f := range testFiles {
		out.TestFiles = append(out.TestFiles, traceTestFileHit{
			FilePath:  f.FilePath,
			Operation: f.Operation,
			SessionID: f.SessionID,
		})
	}

	return writeJSON(w, out)
}

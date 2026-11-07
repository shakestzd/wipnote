package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/spf13/cobra"
)

// importHomeSkipTop are top-level entries of ~/.wipnote that are not project
// data: the app's own files (config, the stores folder, logs) and runtime state
// a store rebuilds (sockets, pid and lock files, session markers, the managed
// gitignore and stylesheet the new store already has, delegate run folders).
var importHomeSkipTop = map[string]bool{
	"config.json": true, "config.local.json": true, "projects": true, "logs": true,
	"writer.sock": true, "writer.lease.pid": true, ".serve-children.pid": true,
	".launch-mode": true, ".active-session": true, ".otel-notice-shown": true,
	"debug.log": true, ".gitignore": true, "styles.css": true, "delegate": true, ".git": true,
}

// importHomeSkipSuffix are runtime files skipped at any depth. "pid" covers
// .pid files and the per-session .session-pid marker: a stale PID copied over
// could make an old session look alive.
var importHomeSkipSuffix = []string{".lock", "pid", ".sock", ".tmp"}

func storeCmd() *cobra.Command {
	// Hidden: a one-off migration step, run on request, that should not cost a
	// line of the compact help agents read. `wipnote store --help` still works.
	cmd := &cobra.Command{Use: "store", Short: "Manage wipnote's app-owned project stores", Hidden: true}
	cmd.AddCommand(storeImportHomeCmd())
	return cmd
}

func storeImportHomeCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import-home",
		Short: "Copy the old home-folder project store in ~/.wipnote into $HOME's app store",
		Long: `Before stores moved to ~/.wipnote/projects, running wipnote in your home folder
kept that project's work items in ~/.wipnote itself. This copies that content
(work items, sessions, claims, ledgers and other store folders) into the app
store for $HOME, ~/.wipnote/projects/<encoded home>/.wipnote/, so it is reachable
again from your home folder.

It only copies: nothing in ~/.wipnote is moved, changed or deleted. The app's own
files (config.json, projects/, logs/, writer.sock) and runtime files (locks,
pids, sockets, session markers) are skipped. A file already in the destination
is never overwritten, so running it again copies nothing new.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStoreImportHome(cmd.OutOrStdout(), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be copied and change nothing")
	return cmd
}

// importItem is one file to copy, relative to the source store.
type importItem struct {
	rel    string
	exists bool // already in the destination: left as it is
}

func runStoreImportHome(w io.Writer, dryRun bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("store import-home: %w", err)
	}
	src := paths.DataDir()
	if fi, serr := os.Stat(src); serr != nil || !fi.IsDir() {
		return fmt.Errorf("store import-home: %s does not exist; there is nothing to import", src)
	}
	destRoot := paths.ShadowRootFor(home)
	items, err := importHomePlan(src, filepath.Join(destRoot, ".wipnote"))
	if err != nil {
		return err
	}
	if dryRun {
		return printImportPlan(w, items, filepath.Join(destRoot, ".wipnote"))
	}
	if !paths.IsStoreOwner(destRoot) {
		if destRoot, _, err = paths.CreateExternalStore(home, paths.OriginLauncher, seedStore); err != nil {
			return fmt.Errorf("store import-home: create the store for %s: %w", home, err)
		}
	}
	return copyImportHome(w, src, destRoot, items)
}

// importHomePlan lists the files under src to import into dest.
func importHomePlan(src, dest string) ([]importItem, error) {
	var items []importItem
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		if skipImportEntry(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			_, serr := os.Lstat(filepath.Join(dest, rel))
			items = append(items, importItem{rel: rel, exists: serr == nil})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store import-home: read %s: %w", src, err)
	}
	return items, nil
}

// skipImportEntry: app files and runtime state, and anything that is not a
// plain file or folder (symlinks, sockets).
func skipImportEntry(rel string, d fs.DirEntry) bool {
	if !strings.Contains(rel, string(filepath.Separator)) && importHomeSkipTop[rel] {
		return true
	}
	for _, s := range importHomeSkipSuffix {
		if !d.IsDir() && strings.HasSuffix(d.Name(), s) {
			return true
		}
	}
	return !d.IsDir() && !d.Type().IsRegular()
}

func printImportPlan(w io.Writer, items []importItem, dest string) error {
	copies := 0
	for _, it := range items {
		if it.exists {
			fmt.Fprintf(w, "already there  %s\n", it.rel)
			continue
		}
		copies++
		fmt.Fprintf(w, "would copy     %s\n", it.rel)
	}
	fmt.Fprintf(w, "Dry run: would copy %d file(s) into %s; %d already there. Nothing was changed.\n", copies, dest, len(items)-copies)
	return nil
}

func copyImportHome(w io.Writer, src, destRoot string, items []importItem) error {
	dest := filepath.Join(destRoot, ".wipnote")
	copied, kept := 0, 0
	for _, it := range items {
		ok, err := copyNewFile(filepath.Join(src, it.rel), filepath.Join(dest, it.rel))
		if err != nil {
			return fmt.Errorf("store import-home: copy %s: %w (copied %d before it)", it.rel, err, copied)
		}
		if !ok {
			kept++
			continue
		}
		copied++
		fmt.Fprintf(w, "copied  %s\n", it.rel)
	}
	if copied > 0 {
		if err := commitImport(destRoot); err != nil {
			fmt.Fprintf(w, "warning: the files are copied but the store commit failed: %v\n", err)
		}
	}
	fmt.Fprintf(w, "Copied %d file(s) into %s; %d already there and left as they are. ~/.wipnote was not changed.\n", copied, dest, kept)
	return nil
}

// copyNewFile copies src to dst unless dst exists, which it never overwrites.
func copyNewFile(src, dst string) (bool, error) {
	info, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		return false, err
	}
	defer in.Close()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return false, err
	}
	return true, out.Close()
}

// commitImport records the imported files in the store's own repo. Files its
// managed gitignore marks as runtime stay untracked, as in any store.
func commitImport(destRoot string) error {
	if out, err := exec.Command("git", "-C", destRoot, "add", "-A", "--", ".wipnote").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if exec.Command("git", "-C", destRoot, "diff", "--cached", "--quiet").Run() == nil {
		return nil // everything copied is gitignored runtime state
	}
	out, err := exec.Command("git", "-C", destRoot, "commit", "--quiet", "--no-verify", "-m", "Import the old home-folder store from ~/.wipnote").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

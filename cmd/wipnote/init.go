package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var inRepo bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create this project's wipnote store",
		Long: `Creates the store for the current project under ~/.wipnote/projects, with
nothing added to the project folder. A git repo's store is keyed by its main
checkout, so linked worktrees share it; any other folder's by the folder.

A project that already has a store keeps it, in-repo stores included.

With --in-repo, creates a .wipnote/ directory in the current folder instead.
Safe to run on an existing project: only missing pieces are created.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inRepo {
				return runInitInRepo()
			}
			return runInit()
		},
	}
	cmd.Flags().BoolVar(&inRepo, "in-repo", false, "create .wipnote/ in the current folder instead of under ~/.wipnote/projects")
	return cmd
}

// runInit makes the project's app-owned store, unless a store already serves it.
func runInit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if owner := paths.StoreOwnerFor(cwd); owner != "" {
		fmt.Printf("This project already has a store at %s\n", filepath.Join(owner, ".wipnote"))
		// A launcher's store becomes an explicit one, which non-git folders
		// below this one then use too.
		if _, err := paths.MarkStoreExplicit(owner); err != nil {
			return fmt.Errorf("mark store as made by init: %w", err)
		}
		return nil
	}
	if _, err := ensureExternalStore(cwd, paths.OriginInit); err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	return nil
}

// runInitInRepo makes .wipnote/ in the current folder. $HOME and the app home are
// refused, and so is a project an app-owned store already serves.
func runInitInRepo() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if paths.IsReservedDir(cwd) {
		return fmt.Errorf("--in-repo cannot be used in your home folder or in wipnote's own home (%s); run plain `wipnote init`", paths.DataDir())
	}
	// An in-repo .wipnote/ would win over the project's app-owned store and hide
	// every item in it.
	if owner := paths.StoreOwnerFor(cwd); paths.IsExternalStore(owner) {
		return fmt.Errorf("this project's store is already at %s; an in-repo .wipnote/ would hide it, so --in-repo is refused", filepath.Join(owner, ".wipnote"))
	}

	graphDir := filepath.Join(cwd, ".wipnote")

	if err := createSubdirs(graphDir); err != nil {
		return err
	}

	if err := writeStylesCSS(graphDir); err != nil {
		return err
	}

	// GH#172: an over-broad repo-level .gitignore rule silently defeats
	// every artifact commit; say so now rather than at the first stuck gate.
	warnIfRepoGitignoreIgnoresWipnote(os.Stderr, cwd)

	fmt.Printf("Initialized wipnote in %s\n", graphDir)
	fmt.Println()
	fmt.Println("  .wipnote/features/")
	fmt.Println("  .wipnote/bugs/")
	fmt.Println("  .wipnote/spikes/")
	fmt.Println("  .wipnote/tracks/")
	fmt.Println("  .wipnote/sessions/")
	fmt.Println("  .wipnote/styles.css")
	fmt.Println()
	fmt.Println("Run 'wipnote status' to verify.")
	return nil
}

var subdirs = []string{"features", "bugs", "spikes", "tracks", "sessions"}

func createSubdirs(graphDir string) error {
	for _, sub := range subdirs {
		path := filepath.Join(graphDir, sub)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", path, err)
		}
	}
	return nil
}

func writeStylesCSS(graphDir string) error {
	return writeFileIfAbsent(filepath.Join(graphDir, "styles.css"), defaultStylesCSS)
}

// writeFileIfAbsent writes content to path only when the file does not yet exist.
func writeFileIfAbsent(path string, content []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists — leave it alone
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// defaultStylesCSS is a minimal stylesheet for wipnote HTML nodes.
var defaultStylesCSS = []byte(`/**
 * wipnote Default Stylesheet
 *
 * Provides sensible defaults for wipnote nodes.
 * Customize as needed for your project.
 */

:root {
    --color-primary: #2563eb;
    --color-success: #16a34a;
    --color-warning: #d97706;
    --color-danger:  #dc2626;

    --status-todo:        #6b7280;
    --status-in-progress: #2563eb;
    --status-blocked:     #dc2626;
    --status-done:        #16a34a;

    --priority-low:      #9ca3af;
    --priority-medium:   #3b82f6;
    --priority-high:     #f59e0b;
    --priority-critical: #dc2626;

    --color-bg:           #ffffff;
    --color-bg-secondary: #f9fafb;
    --color-border:       #e5e7eb;
    --color-text:         #1f2937;
    --color-text-secondary: #6b7280;

    --space-xs: 0.25rem;
    --space-sm: 0.5rem;
    --space-md: 1rem;
    --space-lg: 1.5rem;

    --font-sans: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    --font-mono: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
}

@media (prefers-color-scheme: dark) {
    :root {
        --color-bg:           #111827;
        --color-bg-secondary: #1f2937;
        --color-border:       #374151;
        --color-text:         #f9fafb;
        --color-text-secondary: #9ca3af;
    }
}

*, *::before, *::after { box-sizing: border-box; }

body {
    font-family: var(--font-sans);
    font-size: 16px;
    line-height: 1.6;
    color: var(--color-text);
    background: var(--color-bg);
    margin: 0;
    padding: var(--space-lg);
    max-width: 800px;
    margin-inline: auto;
}

article {
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: var(--space-lg);
    margin-bottom: var(--space-lg);
}

.badge {
    display: inline-flex;
    align-items: center;
    padding: var(--space-xs) var(--space-sm);
    font-size: 0.75rem;
    font-weight: 500;
    border-radius: 9999px;
    text-transform: uppercase;
    letter-spacing: 0.025em;
}

.status-todo        { background: color-mix(in srgb, var(--status-todo) 15%, transparent);        color: var(--status-todo); }
.status-in-progress { background: color-mix(in srgb, var(--status-in-progress) 15%, transparent); color: var(--status-in-progress); }
.status-blocked     { background: color-mix(in srgb, var(--status-blocked) 15%, transparent);     color: var(--status-blocked); }
.status-done        { background: color-mix(in srgb, var(--status-done) 15%, transparent);        color: var(--status-done); }

.priority-low      { background: color-mix(in srgb, var(--priority-low) 15%, transparent);      color: var(--priority-low); }
.priority-medium   { background: color-mix(in srgb, var(--priority-medium) 15%, transparent);   color: var(--priority-medium); }
.priority-high     { background: color-mix(in srgb, var(--priority-high) 15%, transparent);     color: var(--priority-high); }
.priority-critical { background: color-mix(in srgb, var(--priority-critical) 15%, transparent); color: var(--priority-critical); }
`)

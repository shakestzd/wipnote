// Package herdrapp installs wipnote's layout and mod into the Herdr app's home
// (feat-190b388d). The files are embedded so every install path has them, and
// follow the folder format of herdr.dev/layouts: layouts/<name>.json, its
// mods/, a RECIPE.md.
package herdrapp

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// LayoutName is the layout this package installs.
const LayoutName = "wipnote-main"

//go:embed all:assets/wipnote-main
var assets embed.FS

const assetRoot = "assets/" + LayoutName

// Home is the Herdr app's home: HERDR_APP_HOME, else ~/.config/herdr-app, as
// its manual says.
func Home(getenv func(string) string, userHome string) string {
	if h := getenv("HERDR_APP_HOME"); h != "" {
		return h
	}
	return filepath.Join(userHome, ".config", "herdr-app")
}

// HeaderMod is the pane header mod the layout names.
const HeaderMod = "wipnote-pane-header"

// AgentsMod is the agents list the layout names: Herdr's own, keeping only the
// nested row of a worker that has both a pane and a parent.
const AgentsMod = "wipnote-agents"

// Paths are the home's entries Install writes, relative to the home. Only
// these three: the recipe describes the build and stays out, as Herdr's own guide
// for adding a layout says.
func Paths() []string {
	return []string{
		path.Join("layouts", LayoutName+".json"),
		path.Join("mods", AgentsMod),
		path.Join("mods", HeaderMod),
	}
}

// Conflict reports an entry that already exists, so nothing was written.
type Conflict struct{ Path string }

func (c *Conflict) Error() string {
	return fmt.Sprintf("%s already exists: nothing was written. It may hold your edits; use --replace to move it aside and install again", c.Path)
}

// Install copies the layout and its mods into home. It never overwrites: if any
// target exists it returns a *Conflict and writes nothing, because the home's
// files are the person's. It returns the paths it wrote.
func Install(home string) ([]string, error) {
	var wrote []string
	for _, rel := range Paths() {
		target := filepath.Join(home, filepath.FromSlash(rel))
		if _, err := os.Lstat(target); err == nil {
			return nil, &Conflict{Path: target}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		wrote = append(wrote, target)
	}
	for _, rel := range Paths() {
		if err := copyTree(path.Join(assetRoot, rel), filepath.Join(home, filepath.FromSlash(rel))); err != nil {
			return nil, err
		}
	}
	return wrote, nil
}

// Replace moves the home's existing entries into a new folder named after stamp,
// then installs. It returns the paths written and the folder, "" when nothing
// had to move. The folder sits outside mods/ and layouts/, so the app does not
// load what was moved. A folder that already exists is refused before anything
// moves, so an earlier backup is never overwritten.
func Replace(home, stamp string) ([]string, string, error) {
	var existing []string
	for _, rel := range Paths() {
		_, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel)))
		if err == nil {
			existing = append(existing, rel)
		} else if !os.IsNotExist(err) {
			return nil, "", err
		}
	}
	if len(existing) == 0 {
		wrote, err := Install(home)
		return wrote, "", err
	}
	aside := filepath.Join(home, "wipnote-replaced-"+stamp)
	if err := os.Mkdir(aside, 0o755); err != nil {
		return nil, "", fmt.Errorf("backup folder: %w", err)
	}
	for _, rel := range existing {
		to := filepath.Join(aside, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return nil, aside, err
		}
		if err := os.Rename(filepath.Join(home, filepath.FromSlash(rel)), to); err != nil {
			return nil, aside, err
		}
	}
	wrote, err := Install(home)
	return wrote, aside, err
}

// copyTree copies one embedded file, or a folder and all it holds.
func copyTree(src, dst string) error {
	return fs.WalkDir(assets, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.FromSlash(src), filepath.FromSlash(p))
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

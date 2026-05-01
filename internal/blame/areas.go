// Package blame — areas aggregator.
//
// WalkAreas walks every tracked source file under root, runs blame.Query for
// each, and returns a grouped per-track inventory.  It reuses Query() from
// this package rather than re-implementing the SQL logic.
package blame

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// WalkOptions controls the WalkAreas traversal.
type WalkOptions struct {
	// ByFile reverses the grouping: instead of tracks→files, return files→tracks.
	ByFile bool
	// IncludeUntracked includes files with zero feature_files rows in Untracked.
	// Defaults to true when the zero value of WalkOptions is used.
	IncludeUntracked *bool
}

// includeUntracked returns the effective IncludeUntracked setting.
// When the pointer is nil the default is true.
func (o WalkOptions) includeUntracked() bool {
	if o.IncludeUntracked == nil {
		return true
	}
	return *o.IncludeUntracked
}

// FileEntry describes one file within a track group.
type FileEntry struct {
	Path     string `json:"path"`
	Features int    `json:"features"`
	Touches  int    `json:"touches"`
}

// TrackArea is a single track's file inventory.
type TrackArea struct {
	TrackID    string      `json:"track_id"`
	TrackTitle string      `json:"track_title"`
	Files      []FileEntry `json:"files"`
	// Aggregate counts
	FeatureCount int `json:"feature_count"`
	TouchCount   int `json:"touch_count"`
}

// FileArea is one file with its associated tracks (used in ByFile mode).
type FileArea struct {
	Path   string        `json:"path"`
	Tracks []TrackRollup `json:"tracks"`
}

// AreasResult holds the complete WalkAreas output.
type AreasResult struct {
	// ByTrack groups files per track (populated when WalkOptions.ByFile == false).
	ByTrack []TrackArea `json:"by_track,omitempty"`
	// ByFile lists each file with its tracks (populated when WalkOptions.ByFile == true).
	ByFile []FileArea `json:"by_file,omitempty"`
	// Untracked holds files with no feature attribution (when includeUntracked == true).
	Untracked []string `json:"untracked,omitempty"`
}

// untrackedTrackID is the synthetic track ID used by blame.rollupTracks for
// features whose TrackID is empty. WalkAreas mirrors that grouping when
// computing distinct-feature counts so the "(untracked)" track row reports a
// truthful FeatureCount.
const untrackedTrackID = "(untracked)"

// excludedDir reports whether a path component should keep its file out of
// the inventory. .htmlgraph is skipped because work-item HTML is its own
// attribution domain, not source code; build outputs and vendored deps are
// noise for this particular doc.
func excludedDir(name string) bool {
	switch name {
	case ".htmlgraph", "node_modules", "vendor", "dist", "out", "build", "target":
		return true
	}
	return false
}

func excludedPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if excludedDir(part) {
			return true
		}
	}
	return false
}

// gitListFiles returns the tracked files at root, root-relative with forward
// slashes. Using `git ls-files` (instead of walking the working tree) keeps
// the inventory deterministic across environments — build artifacts, untracked
// editor scratch files, and the local cache directory are all invisible to it.
func gitListFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	raw := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	files := make([]string, 0, len(raw))
	for _, p := range raw {
		if p == "" || excludedPath(p) {
			continue
		}
		files = append(files, p)
	}
	return files, nil
}

// WalkAreas enumerates tracked files under root, runs blame.Query for each,
// and groups the results as requested by opts.
func WalkAreas(ctx context.Context, database *sql.DB, root string, opts WalkOptions) (*AreasResult, error) {
	files, err := gitListFiles(ctx, root)
	if err != nil {
		return nil, err
	}

	// trackMap accumulates per-track aggregates (ByTrack mode).
	trackMap := make(map[string]*TrackArea)
	// trackFeatures holds the set of distinct feature IDs touching each track,
	// so FeatureCount reflects unique features rather than the per-file rollup
	// summed across files (which double-counted features touching multiple files).
	trackFeatures := make(map[string]map[string]struct{})
	var byFile []FileArea
	var untracked []string

	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Skip directories that may have been listed as gitlinks (submodules).
		if info, statErr := os.Stat(filepath.Join(root, rel)); statErr == nil && info.IsDir() {
			continue
		}

		result, queryErr := Query(ctx, database, rel, QueryOptions{})
		if queryErr != nil {
			return nil, fmt.Errorf("blame %s: %w", rel, queryErr)
		}

		if len(result.Features) == 0 {
			if opts.includeUntracked() {
				untracked = append(untracked, rel)
			}
			continue
		}

		if opts.ByFile {
			byFile = append(byFile, FileArea{
				Path:   rel,
				Tracks: result.Tracks,
			})
			continue
		}

		// ByTrack grouping: fan out to each track that touched this file.
		for _, tr := range result.Tracks {
			ta, ok := trackMap[tr.ID]
			if !ok {
				ta = &TrackArea{
					TrackID:    tr.ID,
					TrackTitle: tr.Title,
				}
				trackMap[tr.ID] = ta
				trackFeatures[tr.ID] = make(map[string]struct{})
			}
			ta.Files = append(ta.Files, FileEntry{
				Path:     rel,
				Features: tr.FeatureCount,
				Touches:  tr.TouchCount,
			})
			ta.TouchCount += tr.TouchCount
		}
		// Record distinct feature IDs per track from this file's features.
		// Empty TrackID is normalised to the same synthetic key blame uses,
		// so the "(untracked)" row's FeatureCount stays truthful.
		for _, fr := range result.Features {
			tid := fr.TrackID
			if tid == "" {
				tid = untrackedTrackID
			}
			if set, ok := trackFeatures[tid]; ok {
				set[fr.ID] = struct{}{}
			}
		}
	}

	// Resolve distinct-feature counts per track now that the walk is complete.
	for id, ta := range trackMap {
		ta.FeatureCount = len(trackFeatures[id])
	}

	res := &AreasResult{}

	if opts.ByFile {
		// Sort by path for deterministic output.
		sort.Slice(byFile, func(i, j int) bool { return byFile[i].Path < byFile[j].Path })
		res.ByFile = byFile
	} else {
		// Flatten map, sort tracks by file count desc, then alphabetically.
		tracks := make([]TrackArea, 0, len(trackMap))
		for _, ta := range trackMap {
			// Sort files within track alphabetically.
			sort.Slice(ta.Files, func(i, j int) bool { return ta.Files[i].Path < ta.Files[j].Path })
			tracks = append(tracks, *ta)
		}
		sort.Slice(tracks, func(i, j int) bool {
			if len(tracks[i].Files) != len(tracks[j].Files) {
				return len(tracks[i].Files) > len(tracks[j].Files)
			}
			return tracks[i].TrackID < tracks[j].TrackID
		})
		res.ByTrack = tracks
	}

	if opts.includeUntracked() {
		sort.Strings(untracked)
		res.Untracked = untracked
	}

	return res, nil
}

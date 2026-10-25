package main

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/blame"
)

// contextPackHistoryScan bounds how many linked commits feed the file list.
const contextPackHistoryScan = 200

// trackFeatureIDs returns the features that belong to trackID, by track_id
// or by a part_of edge.
func trackFeatureIDs(proj *workitem.Project, trackID string) []string {
	nodes, err := proj.Features.Collection.List()
	if err != nil {
		return nil
	}
	var ids []string
	for _, n := range nodes {
		if n.TrackID == trackID || hasEdgeTo(n, "part_of", trackID) {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

func hasEdgeTo(n *models.Node, rel, target string) bool {
	for _, e := range n.Edges[rel] {
		if e.TargetID == target {
			return true
		}
	}
	return false
}

// trackGitHistory reads, in one git call, the newest commits whose message
// names one of featureIDs, and the files those commits touched. Commits come
// back newest first; files sort by touches, then path. Paths under .wipnote/
// are skipped. It returns nil, nil outside a git repo or with no features.
func trackGitHistory(repoRoot string, featureIDs []string, limit int) ([]models.GitCommit, []blame.FileEntry) {
	if len(featureIDs) == 0 {
		return nil, nil
	}
	args := []string{"-C", repoRoot, "log", "--all", "--fixed-strings", "--name-only",
		"--max-count=" + strconv.Itoa(limit), "--format=%x1e%H%x1f%ct%x1f%s%x1f%b%x1d"}
	for _, id := range featureIDs {
		args = append(args, "--grep="+id)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, nil
	}
	want := make(map[string]bool, len(featureIDs))
	for _, id := range featureIDs {
		want[id] = true
	}
	return parseLinkedHistory(string(out), want)
}

// fileTally counts how often a path was touched and by which features.
type fileTally struct {
	touches  map[string]int
	features map[string]map[string]bool
}

func (t *fileTally) add(path, featureID string) {
	t.touches[path]++
	if t.features[path] == nil {
		t.features[path] = map[string]bool{}
	}
	t.features[path][featureID] = true
}

func parseLinkedHistory(out string, want map[string]bool) ([]models.GitCommit, []blame.FileEntry) {
	var commits []models.GitCommit
	tally := &fileTally{touches: map[string]int{}, features: map[string]map[string]bool{}}
	for _, record := range strings.Split(out, "\x1e") {
		head, names, ok := strings.Cut(record, "\x1d")
		fields := strings.SplitN(head, "\x1f", 4)
		if !ok || len(fields) < 4 {
			continue
		}
		featureID := firstLinkedID(fields[2]+"\n"+fields[3], want)
		if featureID == "" {
			continue // --grep matched a longer ID or prose, not a link
		}
		secs, _ := strconv.ParseInt(fields[1], 10, 64)
		commits = append(commits, models.GitCommit{
			CommitHash: fields[0], FeatureID: featureID, Message: fields[2], Timestamp: time.Unix(secs, 0).UTC(),
		})
		for _, path := range strings.Fields(names) {
			if !strings.HasPrefix(path, ".wipnote/") {
				tally.add(path, featureID)
			}
		}
	}
	if len(commits) == 0 {
		return nil, nil
	}
	return commits, tally.ranked()
}

func firstLinkedID(message string, want map[string]bool) string {
	for _, id := range parseTrailers(message) {
		if want[id] {
			return id
		}
	}
	return ""
}

func (t *fileTally) ranked() []blame.FileEntry {
	files := make([]blame.FileEntry, 0, len(t.touches))
	for path, n := range t.touches {
		files = append(files, blame.FileEntry{Path: path, Touches: n, Features: len(t.features[path])})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Touches != files[j].Touches {
			return files[i].Touches > files[j].Touches
		}
		return files[i].Path < files[j].Path
	})
	return files
}

func trackTitle(proj *workitem.Project, trackID string) string {
	if n, err := proj.Tracks.Collection.Get(trackID); err == nil {
		return n.Title
	}
	return trackID
}

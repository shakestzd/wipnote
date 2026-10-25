package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/projection"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

func statuslineCmd() *cobra.Command {
	var sessionID string
	var cacheMode bool

	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Print the active work item for a harness status line",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cacheMode {
				return runStatuslineCache()
			}
			return runStatusline(sessionID)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID to scope the active work item lookup")
	cmd.Flags().BoolVar(&cacheMode, "cache", false, "Render the project-scoped active work item from the launch cache (session-independent; used by harnesses like Antigravity that do not key statusline commands to a wipnote session)")
	return cmd
}

// runStatuslineCache renders the project-scoped active work item from the
// launch cache written by `wipnote feature start`. It is session-independent:
// the cache is keyed by a hash of the project's .wipnote/ dir, so the value is
// correct for whichever project the current working directory belongs to and
// never bleeds across projects. Any piped stdin (harness agent-state JSON) is
// drained and ignored — the work item comes from the cache, not the session.
func runStatuslineCache() error {
	// Drain any piped agent-state JSON in the BACKGROUND so a large writer never
	// blocks on a full pipe buffer — but never block our own exit waiting for
	// EOF. A harness that holds the status-line command's stdin open (agy
	// streams and does not close it) would otherwise hang a synchronous read
	// forever and leave the status line blank. We print the cached work item and
	// exit immediately; the goroutine is reaped on process exit.
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		go func() { _, _ = io.Copy(io.Discard, os.Stdin) }()
	}
	dir, err := findWipnoteDir()
	if err != nil {
		return nil
	}
	if line := ReadStatuslineCache(dir); line != "" {
		fmt.Println(line)
	}
	return nil
}

func runStatusline(sessionID string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return nil
	}

	// If a session ID is provided, look up its open claim episode.
	if sessionID != "" {
		return statuslineFromSession(dir, sessionID)
	}

	// No session ID: return nothing. The global HTML scan has no session context and
	// would leak cross-session state (e.g. show a bug from session B when session A
	// is calling). An empty statusline is correct when no session is scoped.
	return nil
}

func statuslineFromSession(dir, sessionID string) error {
	featureID, err := activeClaimForStatusline(dir, sessionID, os.Getenv("WIPNOTE_AGENT_ID"))
	if err != nil {
		return nil
	}
	if featureID == "" {
		return nil
	}

	// Look up the title from the HTML file.
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		// We have the ID but can't get title — still show the ID.
		fmt.Println(featureID)
		return nil
	}
	defer p.Close()

	// Find the feature node.
	var featureType string
	var featureTitle string
	var item *models.Node
	for _, typeName := range []string{"bug", "feature", "spike"} {
		col := collectionFor(p, typeName)
		node, err := col.Get(featureID)
		if err == nil && node != nil {
			if node.Status == "done" || node.Status == "completed" {
				return nil // Feature was completed — don't show it
			}
			featureType = typeName
			featureTitle = node.Title
			item = node
			break
		}
	}
	if featureTitle == "" {
		return nil
	}

	// Check if feature belongs to a track.
	trackLine := resolveTrackContext(dir, item)

	if trackLine != "" {
		fmt.Printf("%s → %s %s\n", trackLine, iconFor(featureType), truncate(featureTitle, 25))
	} else {
		fmt.Printf("%s %s\n", iconFor(featureType), truncate(featureTitle, 30))
	}
	return nil
}

func activeClaimForStatusline(wipnoteDir, sessionID, agentID string) (string, error) {
	agentID = normaliseStatuslineAgentID(agentID)
	episodes, err := claimledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return "", err
	}
	var latest claimledger.Episode
	for _, e := range episodes {
		if !e.IsOpen() || e.SessionID != sessionID || e.AgentID != agentID {
			continue
		}
		if latest.WorkItemID == "" || e.StartedAt.After(latest.StartedAt) {
			latest = e
		}
	}
	return latest.WorkItemID, nil
}

func normaliseStatuslineAgentID(agentID string) string {
	if agentID == "" {
		return "__root__"
	}
	return agentID
}

// resolveTrackContext returns a formatted track summary if the item belongs to a track.
// Format: "track_icon Track Title [done/total]"
// Returns empty string if no track.
//
// The track is resolved from the item's own artifact and the track's file
// (feat-c30d0792) — the same answer projection.Load's edges give, without
// loading every artifact in the project, which made an open claim's status
// line take ~0.45 s on this repo. When the item points at a track that is not
// a plain tracks/<id>.html here (archived, or another layout), only the
// projection knows its title, so that case still loads it.
func resolveTrackContext(dir string, item *models.Node) string {
	trackID, title, ok := trackFromItem(dir, item)
	if !ok {
		trackID, title = trackFromProjection(dir, item.ID)
	}
	if trackID == "" {
		return ""
	}

	// Count done/total by reading HTML files directly — same source that
	// `wipnote track show` uses. SQLite features rows are often incomplete
	// (features indexed in graph_edges but absent from the features table),
	// which caused [0/0] to appear in the status line.
	features := loadLinkedByType(dir, "features", trackID)
	total := len(features)
	done := 0
	for _, f := range features {
		if f.Status == "done" || f.Status == "completed" {
			done++
		}
	}
	if title == "" {
		title = trackID
	} else {
		title = truncate(title, 25)
	}
	return fmt.Sprintf("%s %s [%d/%d]", iconFor("track"), title, done, total)
}

// trackFromItem resolves the item's track as projection.Load's first
// part_of → trk-* out-edge would: a declared part_of edge to a track that
// exists, else the item's TrackID field. ok is false when the answer depends
// on a track this cheap lookup cannot see, and the caller must ask the
// projection instead.
func trackFromItem(dir string, item *models.Node) (trackID, title string, ok bool) {
	for _, e := range item.Edges[string(models.RelPartOf)] {
		rel := string(e.Relationship)
		if (rel != "" && rel != string(models.RelPartOf)) || !strings.HasPrefix(e.TargetID, "trk-") {
			continue
		}
		t, found := trackFile(dir, e.TargetID)
		if !found {
			return "", "", false // archived (live) or dangling (skipped): projection decides
		}
		return e.TargetID, t, true
	}
	if !strings.HasPrefix(item.TrackID, "trk-") {
		return "", "", true
	}
	t, found := trackFile(dir, item.TrackID)
	if !found {
		return "", "", false
	}
	return item.TrackID, t, true
}

// trackFile returns the title of tracks/<id>.html when that file parses as
// the track id.
func trackFile(dir, id string) (string, bool) {
	n, err := htmlparse.ParseFile(filepath.Join(dir, "tracks", id+".html"))
	if err != nil || n == nil || n.ID != id {
		return "", false
	}
	return n.Title, true
}

// trackFromProjection is the whole-project lookup trackFromItem avoids.
func trackFromProjection(dir, featureID string) (trackID, title string) {
	snap, err := loadProjectionFn(dir)
	if err != nil {
		return "", ""
	}
	for _, e := range snap.Out[featureID] {
		if e.Relationship == "part_of" && strings.HasPrefix(e.ToID, "trk-") {
			trackID = e.ToID
			break
		}
	}
	if n, ok := snap.Nodes[trackID]; ok && trackID != "" {
		title = n.Title
	}
	return trackID, title
}

// loadProjectionFn is the status line's entry to the whole-project projection
// load. It is a seam so a test can assert the open-claim path stays off it
// (feat-c30d0792).
var loadProjectionFn = projection.Load

func iconFor(typeName string) string {
	switch typeName {
	case "bug":
		return "\uf188" //  bug
	case "feature":
		return "\uf0eb" //  lightbulb
	case "spike":
		return "\uf0e7" //  bolt
	case "track":
		return "\uf018" //  road
	default:
		return "\uf111" //  circle
	}
}

// WriteStatuslineCache writes the active work item summary to a project-scoped
// cache file. The filename includes a hash of the wipnoteDir so different
// projects never overwrite each other's cache (bug-95dc78ba).
// Pass empty featureID to clear the cache (on complete).
//
// Writes are atomic (write-to-temp + rename) so parallel agents calling
// feature start cannot produce a torn cache file (bug-d2d3fb3f).
func WriteStatuslineCache(wipnoteDir, featureID string) {
	cachePath := statuslineCachePath(wipnoteDir)
	if cachePath == "" {
		return
	}

	var payload []byte
	if featureID != "" {
		payload = []byte(buildCacheLine(wipnoteDir, featureID))
	}
	_ = atomicWriteFile(cachePath, payload, 0o644) // best-effort cache write
}

// atomicWriteFile writes data to path via a temp file in the same directory
// followed by os.Rename, and returns any error. Best-effort callers (e.g. cache
// writes) may ignore the result; callers that report success to the user should
// check it so a permission/disk-full/rename failure is not mistaken for success.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Chmod(tmpPath, mode)
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// buildCacheLine produces the display string for a work item, including
// its track context if available. Format: "Track [done/total] -> Feature"
func buildCacheLine(wipnoteDir, featureID string) string {
	p, err := workitem.Open(wipnoteDir, "claude-code")
	if err != nil {
		return featureID
	}
	defer p.Close()

	var featureType, featureTitle string
	var item *models.Node
	for _, typeName := range []string{"bug", "feature", "spike"} {
		col := collectionFor(p, typeName)
		node, nodeErr := col.Get(featureID)
		if nodeErr == nil && node != nil {
			featureType = typeName
			featureTitle = node.Title
			item = node
			break
		}
	}
	if featureTitle == "" {
		return featureID
	}

	trackLine := resolveTrackContext(wipnoteDir, item)
	if trackLine != "" {
		return fmt.Sprintf("%s -> %s %s",
			trackLine, iconFor(featureType), truncate(featureTitle, 25))
	}
	return fmt.Sprintf("%s %s", iconFor(featureType), truncate(featureTitle, 30))
}

// ReadStatuslineCache reads the project-scoped cached status line from disk.
// Returns empty string if the cache file doesn't exist or is empty.
// wipnoteDir is required to scope the lookup to the correct project.
func ReadStatuslineCache(wipnoteDir string) string {
	cachePath := statuslineCachePath(wipnoteDir)
	if cachePath == "" {
		return ""
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// statuslineCachePath returns the project-scoped cache file path.
// Format: <cacheDir>/.wipnote-statusline-<hash8>
func statuslineCachePath(wipnoteDir string) string {
	cacheDir := os.Getenv("WIPNOTE_CACHE_DIR")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cacheDir = home
	}
	if wipnoteDir == "" {
		return ""
	}
	h := sha256.Sum256([]byte(filepath.Clean(wipnoteDir)))
	suffix := hex.EncodeToString(h[:4]) // 8 hex chars
	return filepath.Join(cacheDir, ".wipnote-statusline-"+suffix)
}

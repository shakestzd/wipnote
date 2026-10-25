package workitem

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/models"
)

// nodeWriteMu serialises concurrent writes that touch the same work item HTML
// file in a single process. Keyed by node path (string) → *sync.Mutex.
// This prevents lost-update races between writers — canonical work item
// mutations, `compliance auto`'s findings writer, and `spec generate --insert`'s
// spec writer all acquire the same per-item lock via LockFeatureForWrite.
var nodeWriteMu sync.Map

// LockFeatureForWrite acquires both an in-process mutex AND a cross-process
// advisory file lock so multiple writers cannot race on the same work item
// HTML. The file lock guards a sidecar at `<featurePath>.lock` (created on first
// use, never deleted — flocks survive removal anyway, and an always-present
// sidecar means we never re-create a contested file).
// Callers MUST defer the returned release function.
//
// The acquire-read-modify-write window must be inside the lock; the
// underlying atomic temp+rename keeps single writes safe on its own. This
// closes the lost-update race when `compliance auto`, `spec generate
// --insert`, and `WriteNodeHTML` (status transitions) target the same
// feature concurrently — including from separate `wipnote` CLI processes.
//
// On flock acquisition errors, falls back to in-process-only locking and
// logs nothing; this preserves single-process behavior for tests on file
// systems that don't support flock.
func LockFeatureForWrite(featurePath string) (release func()) {
	muVal, _ := nodeWriteMu.LoadOrStore(featurePath, &sync.Mutex{})
	mu := muVal.(*sync.Mutex)
	mu.Lock()

	lockPath := featurePath + ".lock"
	f, ferr := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if ferr != nil {
		// In-process lock only — degrade gracefully on filesystem errors.
		return mu.Unlock
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return mu.Unlock
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		mu.Unlock()
	}
}

// --- ID generation -----------------------------------------------------------

// prefixes maps node types to their short ID prefix.
// Matches Python wipnote.ids.PREFIXES.
var prefixes = map[string]string{
	"feature": "feat",
	"bug":     "bug",
	"chore":   "chr",
	"spike":   "spk",
	"epic":    "epc",
	"session": "sess",
	"track":   "trk",
	"phase":   "phs",
	"agent":   "agt",
	"spec":    "spec",
	"plan":    "plan",
	"event":   "evt",
}

// GenerateID creates a collision-resistant ID matching the Python format.
//
// Format: {prefix}-{hex8} (e.g., feat-a1b2c3d4)
//
// The hash combines: title + UTC timestamp (nanosecond) + 4 random bytes.
func GenerateID(nodeType, title string) string {
	prefix, ok := prefixes[nodeType]
	if !ok && len(nodeType) >= 4 {
		prefix = nodeType[:4]
	} else if !ok {
		prefix = nodeType
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	entropy := make([]byte, 4)
	_, _ = rand.Read(entropy) // crypto/rand never errors on supported platforms

	content := append([]byte(fmt.Sprintf("%s:%s", title, ts)), entropy...)
	hash := sha256.Sum256(content)

	return fmt.Sprintf("%s-%x", prefix, hash[:4])
}

// --- HTML writing ------------------------------------------------------------

//go:embed templates/node.gohtml
var templateFS embed.FS

var nodeTmpl = template.Must(
	template.ParseFS(templateFS, "templates/node.gohtml"),
)

// WriteNodeHTML serialises a Node to the canonical wipnote HTML format and
// writes it to the collection directory. The output MUST be parseable by
// htmlparse.ParseFile to ensure round-trip fidelity.
//
// Writes are atomic: the content is rendered to a temp file, fsynced, then
// renamed over the target path (POSIX rename is atomic). A per-node lock also
// serialises concurrent cross-process writes to the same node file.
//
// Supplemental sections (`<section class="spec">` and
// `<section class="compliance-findings">`) are preserved across writes:
// callers like `compliance auto` and `spec generate --insert` append these
// outside the templated render, and we don't want a status transition (which
// re-renders the whole file from the Node) to silently delete them.
//
// Returns the absolute path of the written file.
func WriteNodeHTML(dir string, node *models.Node) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create dir %s: %w", dir, err)
	}

	path := filepath.Join(dir, node.ID+".html")
	defer LockFeatureForWrite(path)()
	return writeNodeHTMLUnlocked(dir, node)
}

// writeNodeHTMLUnlocked writes node HTML without acquiring the per-item lock.
// Callers use this when a broader canonical read-modify-write operation already
// owns LockFeatureForWrite for the target path.
func writeNodeHTMLUnlocked(dir string, node *models.Node) (string, error) {
	path := filepath.Join(dir, node.ID+".html")
	html, err := renderNodeHTML(node)
	if err != nil {
		return "", fmt.Errorf("render %s: %w", node.ID, err)
	}

	// Extract supplemental sections from the existing file (if any) and
	// splice them back into the freshly-rendered template output. Skip
	// silently when the file doesn't exist or has no supplemental sections.
	if existing, rerr := os.ReadFile(path); rerr == nil {
		if merged, ok := preserveSupplementalSections(string(existing), html); ok {
			html = merged
		}
	}

	if err := filelock.WriteFileAtomic(path, []byte(html), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// preserveSupplementalSections finds known supplemental sections in the prior
// file content (sections that the node template does not emit — currently
// `<section class="spec">` and `<section class="compliance-findings">`) and
// re-inserts them into the freshly-rendered template just before `</body>`.
// Returns the merged HTML and ok=true when at least one section was carried
// over; otherwise returns ("", false) so the caller writes the raw render.
func preserveSupplementalSections(existing, rendered string) (string, bool) {
	classes := []string{"spec", "compliance-findings"}
	var preserved []string
	for _, class := range classes {
		if section, ok := extractSection(existing, class); ok {
			preserved = append(preserved, section)
		}
	}
	if len(preserved) == 0 {
		return "", false
	}
	insert := "\n" + strings.Join(preserved, "\n") + "\n"
	bodyClose := strings.LastIndex(rendered, "</body>")
	if bodyClose == -1 {
		return rendered + insert, true
	}
	return rendered[:bodyClose] + insert + rendered[bodyClose:], true
}

// extractSection returns the first `<section class="<class>"...></section>`
// block in html along with ok=true. Match is by leading attribute prefix so
// extra attributes (e.g. data-* on the compliance-findings section) are
// retained verbatim.
func extractSection(html, class string) (string, bool) {
	openPrefix := `<section class="` + class + `"`
	const closeTag = `</section>`
	start := strings.Index(html, openPrefix)
	if start == -1 {
		return "", false
	}
	end := strings.Index(html[start:], closeTag)
	if end == -1 {
		return "", false
	}
	end += start + len(closeTag)
	return html[start:end], true
}

// renderNodeHTML produces the full HTML document for a node using
// html/template with an embedded .gohtml template.
func renderNodeHTML(n *models.Node) (string, error) {
	data := newNodeTemplateData(n)
	var buf bytes.Buffer
	if err := nodeTmpl.ExecuteTemplate(&buf, "node.gohtml", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

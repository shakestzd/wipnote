package oneshot

import (
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// Settings are the mod's userConfig (plugin.json) as wipnote settings: the
// "delegate" object of the layered wipnote config, with the mod's names and
// defaults. The Textual viewer setting is gone (q-viewer: the Go viewer).
type Settings struct {
	Layout          string // auto | tile | right | down
	Direction       string // legacy: right | down, honoured only when layout is auto or unset
	MaxPanes        int    // tile: live one-shot panes before a run goes headless
	ClosePane       string // always | success | never
	CloseDelay      int    // seconds before the pane closes
	TailLines       int    // clean.log lines returned after a failure
	MaxReturnLines  int    // lines per returned section; 0 returns all
	LeanProfile     bool   // add -p <LeanProfileName> to codex exec when the profile file exists
	LeanProfileName string
	CcxRunsDir      string // ccx's run-log folder; no default (CCX_RUNS_DIR wins)
	CcxWaitSeconds  int    // how long a ccx run waits for its log
	KeepDays        int    // finished runs and ccx claims older than this are swept
}

// DefaultSettings are the mod's defaults (register.ts).
func DefaultSettings() Settings {
	return Settings{Layout: "auto", Direction: "right", MaxPanes: 6, ClosePane: "always", CloseDelay: 3,
		TailLines: 200, MaxReturnLines: 60, LeanProfile: true, LeanProfileName: "lean", CcxWaitSeconds: 30, KeepDays: 7}
}

// rawSettings is the "delegate" object, merged over the config layers.
type rawSettings struct {
	Layout          *string `json:"layout"`
	Direction       *string `json:"direction"`
	MaxPanes        *int    `json:"maxPanes"`
	ClosePane       *string `json:"closePane"`
	CloseDelay      *int    `json:"closeDelay"`
	TailLines       *int    `json:"tailLines"`
	MaxReturnLines  *int    `json:"maxReturnLines"`
	LeanProfile     *bool   `json:"leanProfile"`
	LeanProfileName *string `json:"leanProfileName"`
	CcxRunsDir      *string `json:"ccxRunsDir"`
	CcxWaitSeconds  *int    `json:"ccxWaitSeconds"`
	KeepDays        *int    `json:"keepDays"`
}

// pathKeys are the delegate settings that hold host paths: read only from
// ~/.wipnote/config.json and .wipnote/config.local.json, never from the
// committed project file.
var pathKeys = []string{"ccxRunsDir"}

var profileChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// LoadSettings reads the "delegate" settings layered user < project <
// local (paths.ReadLayeredConfig); CCX_RUNS_DIR beats them all. A missing
// or bad value keeps its default, as the mod's register.ts does. warn gets
// one line for each host-path key found in the committed project file.
func LoadSettings(projectDir string, warn io.Writer) Settings {
	s := DefaultSettings()
	var d rawSettings
	paths.ReadLayeredConfig(projectDir, paths.LayeredOptions{Section: "delegate", PathKeys: pathKeys, Warn: warn}, &d)
	pick(&s.Layout, d.Layout, "auto", "tile", "right", "down")
	pick(&s.Direction, d.Direction, "right", "down")
	pick(&s.ClosePane, d.ClosePane, "always", "success", "never")
	atLeast(&s.MaxPanes, d.MaxPanes, 1)
	atLeast(&s.CloseDelay, d.CloseDelay, 0)
	atLeast(&s.TailLines, d.TailLines, 1)
	atLeast(&s.MaxReturnLines, d.MaxReturnLines, 0)
	atLeast(&s.CcxWaitSeconds, d.CcxWaitSeconds, 1)
	atLeast(&s.KeepDays, d.KeepDays, 1)
	if d.LeanProfile != nil {
		s.LeanProfile = *d.LeanProfile
	}
	if d.LeanProfileName != nil {
		if name := profileChars.ReplaceAllString(strings.TrimSpace(*d.LeanProfileName), ""); name != "" {
			s.LeanProfileName = name
		}
	}
	if d.CcxRunsDir != nil {
		s.CcxRunsDir = strings.TrimSpace(*d.CcxRunsDir)
	}
	s.CcxRunsDir = ccxRunsDir(s.CcxRunsDir)
	return s
}

func pick(dst *string, v *string, allowed ...string) {
	if v == nil {
		return
	}
	for _, a := range allowed {
		if *v == a {
			*dst = a
		}
	}
}

func atLeast(dst *int, v *int, floor int) {
	if v != nil && *v >= floor {
		*dst = *v
	}
}

// Placement is where a run's pane opens: fixed splits the caller's pane in
// dir; otherwise the pane tiles beside the caller (herdr.TileTarget).
func (s Settings) Placement() (fixed bool, dir herdr.Direction) {
	layout := s.Layout
	if layout == "auto" {
		layout = ""
	}
	switch {
	case layout == "right":
		return true, herdr.Right
	case layout == "down" || (layout == "" && s.Direction == "down"):
		return true, herdr.Down
	}
	return false, ""
}

// ccxWait is how long a ccx run waits for its log.
func (s Settings) ccxWait() time.Duration {
	return time.Duration(max(1, s.CcxWaitSeconds)) * time.Second
}

// keepAge is the sweep age for finished runs and ccx claims.
func (s Settings) keepAge() time.Duration {
	return time.Duration(max(1, s.KeepDays)) * 24 * time.Hour
}

// leanProfile is the profile name codex exec gets, empty when off.
func (s Settings) leanProfile() string {
	if !s.LeanProfile {
		return ""
	}
	return s.LeanProfileName
}

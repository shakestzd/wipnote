package research

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
)

// Mode is the gate's enforcement level, after ESLint's off | warn | error
// ("error" is accepted as an alias for block).
type Mode string

const (
	ModeOff   Mode = "off"
	ModeWarn  Mode = "warn"
	ModeBlock Mode = "block"
)

// Config is the "research" section of .wipnote/config.json.
type Config struct {
	Mode          Mode     `json:"mode"`
	RequiredKinds []string `json:"required_kinds"`

	// Notice reports config values that were ignored (an unknown mode or
	// kind), so the CLI can say so instead of silently not enforcing.
	Notice string `json:"-"`
}

// Rule is the one statement of the research-first rule. CLI help, gate
// messages and the dispatch hook print it; docs link to `wipnote research --help`.
const Rule = "Research-first: work that introduces new behaviour (change kinds " +
	"design, ui, capability, dependency) needs a research record before an " +
	"implementer is dispatched and before completion: at least one source " +
	"(prefer primary), what already exists (from `wipnote relevant` and code " +
	"search, or \"none found\" with the queries used), the options considered " +
	"and the chosen one. Waive with --research-waiver \"<reason>\"."

// LoadConfig reads the research section of <projectRoot>/.wipnote/config.json.
// A missing file, key or unknown mode yields the defaults (warn, DefaultRequiredKinds).
func LoadConfig(projectRoot string) Config {
	cfg := Config{Mode: ModeWarn, RequiredKinds: DefaultRequiredKinds}
	data, err := os.ReadFile(filepath.Join(projectRoot, ".wipnote", "config.json"))
	if err != nil {
		return cfg
	}
	var file struct {
		Research *Config `json:"research"`
	}
	if json.Unmarshal(data, &file) != nil || file.Research == nil {
		return cfg
	}
	var notices []string
	switch m := Mode(strings.ToLower(strings.TrimSpace(string(file.Research.Mode)))); m {
	case ModeOff, ModeWarn, ModeBlock:
		cfg.Mode = m
	case "error":
		cfg.Mode = ModeBlock
	case "":
	default:
		notices = append(notices, fmt.Sprintf("unknown research.mode %q (want off, warn or block); using warn", m))
	}
	if file.Research.RequiredKinds != nil {
		cfg.RequiredKinds = nil
		for _, k := range file.Research.RequiredKinds {
			if ValidKind(k) {
				cfg.RequiredKinds = append(cfg.RequiredKinds, k)
			} else {
				notices = append(notices, fmt.Sprintf("unknown research.required_kinds entry %q ignored", k))
			}
		}
	}
	cfg.Notice = strings.Join(notices, "; ")
	return cfg
}

// Gated reports whether an item of change kind k requires research.
// Items with no kind (pre-gate artifacts, spikes) are never gated.
func (c Config) Gated(k string) bool {
	return c.Mode != ModeOff && k != "" && slices.Contains(c.RequiredKinds, k)
}

func (c Config) gates(n *models.Node) bool {
	return n != nil && c.Gated(KindOf(n.Properties))
}

// FixCommand is the one-line remedy for a gated item lacking research.
func FixCommand(n *models.Node) string {
	return fmt.Sprintf(`wipnote research start "<question>" --for %s  (or: wipnote %s research add %s ...; or --research-waiver "<reason>")`,
		n.ID, typeName(n), n.ID)
}

func typeName(n *models.Node) string {
	if n.Type != "" {
		return n.Type
	}
	return "feature"
}

// missingFor returns what a gated node's record still lacks, or nil when the
// node is not gated or its record (or waiver) is complete. A stub record — a
// question with no sources or decision — counts as no research.
func missingFor(c Config, n *models.Node) []string {
	if !c.gates(n) {
		return nil
	}
	missing, _ := Assess(FromNode(n))
	return missing
}

// CheckStart returns a warning for starting a gated item without a complete
// research record, or "". Starting never fails.
func CheckStart(c Config, n *models.Node) string {
	missing := missingFor(c, n)
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("research advisory: %s %s is change kind %q and its research record is missing %s.\n  fix: %s",
		typeName(n), n.ID, KindOf(n.Properties), strings.Join(missing, "; "), FixCommand(n))
}

// CheckDispatch decides on dispatching an implementer for n: an advisory
// message, and whether to deny (block mode only).
func CheckDispatch(c Config, n *models.Node) (msg string, deny bool) {
	missing := missingFor(c, n)
	if len(missing) == 0 {
		return "", false
	}
	msg = fmt.Sprintf("wipnote research gate: %s is change kind %q and its research record is missing %s. Fix: %s",
		n.ID, KindOf(n.Properties), strings.Join(missing, "; "), FixCommand(n))
	return msg, c.Mode == ModeBlock
}

// CheckComplete evaluates the record at check --gate / complete time. In block
// mode an incomplete record is an error; in warn mode it is a warning.
func CheckComplete(c Config, n *models.Node) (warnings []string, err error) {
	if c.Notice != "" {
		warnings = append(warnings, "research config: "+c.Notice)
	}
	if !c.gates(n) {
		return warnings, nil
	}
	missing, warns := Assess(FromNode(n))
	for _, w := range warns {
		warnings = append(warnings, fmt.Sprintf("research warning: %s: %s", n.ID, w))
	}
	if len(missing) == 0 {
		return warnings, nil
	}
	msg := fmt.Sprintf("research gate: %s (change kind %q) is missing %s.\n  fix: %s",
		n.ID, KindOf(n.Properties), strings.Join(missing, "; "), FixCommand(n))
	if c.Mode == ModeBlock {
		return warnings, fmt.Errorf("%s", msg)
	}
	return append(warnings, msg), nil
}

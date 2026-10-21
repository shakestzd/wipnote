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

// Mode is the gate's enforcement level, after ESLint's off | warn | error.
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
	switch Mode(strings.ToLower(string(file.Research.Mode))) {
	case ModeOff, ModeWarn, ModeBlock:
		cfg.Mode = Mode(strings.ToLower(string(file.Research.Mode)))
	}
	if file.Research.RequiredKinds != nil {
		cfg.RequiredKinds = file.Research.RequiredKinds
	}
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

// CheckStart returns a warning for starting a gated item with no record, or "".
// Starting never fails.
func CheckStart(c Config, n *models.Node) string {
	if !c.gates(n) || FromNode(n) != nil {
		return ""
	}
	return fmt.Sprintf("research advisory: %s %s is change kind %q and has no research record.\n  fix: %s",
		typeName(n), n.ID, KindOf(n.Properties), FixCommand(n))
}

// CheckDispatch decides on dispatching an implementer for n: an advisory
// message, and whether to deny (block mode only).
func CheckDispatch(c Config, n *models.Node) (msg string, deny bool) {
	if !c.gates(n) || FromNode(n) != nil {
		return "", false
	}
	msg = fmt.Sprintf("wipnote research gate: %s is change kind %q with no research record. Fix: %s",
		n.ID, KindOf(n.Properties), FixCommand(n))
	return msg, c.Mode == ModeBlock
}

// CheckComplete evaluates the record at check --gate / complete time. In block
// mode an incomplete record is an error; in warn mode it is a warning.
func CheckComplete(c Config, n *models.Node) (warnings []string, err error) {
	if !c.gates(n) {
		return nil, nil
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

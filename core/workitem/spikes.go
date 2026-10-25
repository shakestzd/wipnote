package workitem

import (
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// SpikeOption configures a new spike during creation.
type SpikeOption func(*spikeConfig)

type spikeConfig struct {
	priority  string
	status    string
	trackID   string
	steps     []string
	content   string
	spikeType string
	findings  string
}

// SpikeWithPriority sets the spike's priority.
func SpikeWithPriority(p string) SpikeOption {
	return func(c *spikeConfig) { c.priority = p }
}

// SpikeWithTrack links the spike to a track.
func SpikeWithTrack(trackID string) SpikeOption {
	return func(c *spikeConfig) { c.trackID = trackID }
}

// SpikeWithType sets the spike investigation type.
func SpikeWithType(t string) SpikeOption {
	return func(c *spikeConfig) { c.spikeType = t }
}

// SpikeWithContent sets the description body.
func SpikeWithContent(content string) SpikeOption {
	return func(c *spikeConfig) { c.content = content }
}

// SpikeCollection provides CRUD operations for spikes.
type SpikeCollection struct {
	*Collection
}

// NewSpikeCollection creates a SpikeCollection bound to the given Base.
func NewSpikeCollection(base *Base) *SpikeCollection {
	return &SpikeCollection{Collection: newCollection(base, "spikes", "spike")}
}

// Create builds a new spike and writes the canonical HTML file.
//
// POLICY: Spikes must only be created via CLI commands or orchestrator actions.
// Hook handlers MUST NOT create spikes — use session events
// instead. Hooks do not import this package; that package-boundary constraint is
// verified by TestHooksPackageDoesNotImportWorkitem in the hooks package.
// See feat-84052b5e for rationale.
func (sc *SpikeCollection) Create(title string, opts ...SpikeOption) (*models.Node, error) {
	if title == "" {
		return nil, fmt.Errorf("spike title must not be empty")
	}

	cfg := &spikeConfig{
		priority: "medium",
		status:   "todo",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	now := time.Now().UTC()
	id := GenerateID("spike", title)

	var steps []models.Step
	for i, desc := range cfg.steps {
		steps = append(steps, models.Step{
			StepID:      fmt.Sprintf("step-%s-%d", id, i),
			Description: desc,
		})
	}

	// Build content from findings and description
	content := cfg.content
	if cfg.findings != "" {
		content = fmt.Sprintf("<p>%s</p>", cfg.findings)
	}

	node := &models.Node{
		ID:            id,
		Title:         title,
		Type:          "spike",
		Status:        models.NodeStatus(cfg.status),
		Priority:      models.Priority(cfg.priority),
		CreatedAt:     now,
		UpdatedAt:     now,
		AgentAssigned: sc.base.Agent,
		TrackID:       cfg.trackID,
		SpikeSubtype:  cfg.spikeType,
		Steps:         steps,
		Content:       content,
	}

	if _, err := sc.writeNode(node); err != nil {
		return nil, fmt.Errorf("create spike: %w", err)
	}

	return node, nil
}

package hooks

import "github.com/shakestzd/wipnote/core/paths"

// SpecEnforcement holds opt-in spec-presence gate flags. Both default to
// false; existing projects keep their current behavior unchanged until they
// explicitly opt in.
type SpecEnforcement struct {
	PromoteSlice    bool `json:"promote_slice"`
	FeatureComplete bool `json:"feature_complete"`
}

// specEnforcementConfig is the slice of .wipnote/config.json that carries
// the spec_enforcement block.
type specEnforcementConfig struct {
	SpecEnforcement SpecEnforcement `json:"spec_enforcement"`
}

// ReadSpecEnforcement returns the opt-in spec_enforcement settings from
// .wipnote/config.json. Returns the zero value (both gates disabled) when
// the file is missing, unreadable, or the key is absent — preserving
// backward-compatible default-off behavior.
func ReadSpecEnforcement(projectDir string) SpecEnforcement {
	var cfg specEnforcementConfig
	if !paths.ReadProjectConfig(projectDir, &cfg) {
		return SpecEnforcement{}
	}
	return cfg.SpecEnforcement
}

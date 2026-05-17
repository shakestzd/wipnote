// Package profile defines the four deployment profiles for wipnote launchers
// and provides per-profile defaults for dashboard bind, plugin source, cache
// location, cleanup behavior, and rollout mode.
//
// Profiles are derived from the runtime mode (slice-1) and plugin mode (slice-1)
// rather than duplicated. Callers should treat profiles as read-only descriptors.
//
// Rollout strategy (staged-rollout design decision):
//   - host-production: warn-only until slice-9 migration tooling exists.
//   - devcontainer-dev: config-gated opt-in (EnforceIsolation driven by config).
//   - ci-test: config-gated (mirrors devcontainer).
//   - local-plugin-dev: warn-only (same as host; plugin port output is the goal).
package profile

import "github.com/shakestzd/wipnote/internal/launcher/mode"

// RolloutMode describes the isolation enforcement posture for this profile.
type RolloutMode string

const (
	// RolloutWarnOnly emits a recommendation warning but never blocks launch.
	// Host production stays here until slice-9 migration tooling is done.
	RolloutWarnOnly RolloutMode = "warn-only"
	// RolloutConfigGated allows enforcement when the config key is set.
	// Devcontainer and CI profiles default here.
	RolloutConfigGated RolloutMode = "config-gated"
)

// CleanupPolicy describes how worktree / temp artifacts are removed.
type CleanupPolicy string

const (
	// CleanupManual leaves cleanup to the user; no automatic deletion.
	CleanupManual CleanupPolicy = "manual"
	// CleanupAutoWorktree removes the managed worktree after the session ends.
	CleanupAutoWorktree CleanupPolicy = "auto-worktree"
)

// CacheLocation describes where the wipnote SQLite index is written.
type CacheLocation string

const (
	// CacheUser uses ~/.cache/wipnote/<hash>/ (XDG, shared across sessions).
	CacheUser CacheLocation = "user"
	// CacheLocal uses a container-local or runner-local path (isolated per session).
	CacheLocal CacheLocation = "local"
)

// DeploymentProfile bundles per-profile defaults for a wipnote launcher.
// It is a value type: copy freely, never mutate.
type DeploymentProfile struct {
	// Label is a short human-readable identifier for logging and docs.
	Label string
	// Runtime is the underlying runtime mode (slice-1).
	Runtime mode.RuntimeMode
	// DashboardHost is the default bind address for wipnote serve.
	DashboardHost string
	// DashboardPort is the default port for wipnote serve.
	DashboardPort int
	// Rollout is the isolation enforcement posture (staged-rollout).
	Rollout RolloutMode
	// EnforceIsolation is false for host-production (warn-only) and may be
	// set by config for devcontainer/CI. NEVER set to true here by default.
	EnforceIsolation bool
	// Cleanup is the worktree/temp artifact cleanup policy.
	Cleanup CleanupPolicy
	// Cache is where the wipnote SQLite index is stored.
	Cache CacheLocation
	// PluginRebuildOnLaunch controls whether generated port trees are rebuilt
	// automatically before the harness is started.
	PluginRebuildOnLaunch bool
}

// ForRuntime returns the DeploymentProfile for the given runtime and plugin mode.
// It reuses DashboardBindDefaults from slice-1 (mode.DashboardBindDefaults) to
// stay consistent with plan-ae0c37b2 slice-4 wipnote status output.
// Profile does NOT duplicate status diagnostics; it only provides launcher defaults.
func ForRuntime(runtime mode.RuntimeMode, plugin mode.PluginMode) DeploymentProfile {
	host, port := mode.DashboardBindDefaults(runtime)

	switch runtime {
	case mode.RuntimeDevcontainer:
		return DeploymentProfile{
			Label:                 "devcontainer-dev",
			Runtime:               runtime,
			DashboardHost:         host,
			DashboardPort:         port,
			Rollout:               RolloutConfigGated,
			EnforceIsolation:      false,
			Cleanup:               CleanupAutoWorktree,
			Cache:                 CacheLocal,
			PluginRebuildOnLaunch: plugin == mode.PluginGeneratedPort,
		}

	case mode.RuntimeCI:
		return DeploymentProfile{
			Label:                 "ci-test",
			Runtime:               runtime,
			DashboardHost:         host,
			DashboardPort:         port,
			Rollout:               RolloutConfigGated,
			EnforceIsolation:      false,
			Cleanup:               CleanupAutoWorktree,
			Cache:                 CacheLocal,
			PluginRebuildOnLaunch: false,
		}

	default:
		label := "host-production"
		if plugin == mode.PluginGeneratedPort {
			label = "local-plugin-dev"
		}
		return DeploymentProfile{
			Label:                 label,
			Runtime:               runtime,
			DashboardHost:         host,
			DashboardPort:         port,
			Rollout:               RolloutWarnOnly,
			EnforceIsolation:      false,
			Cleanup:               CleanupManual,
			Cache:                 CacheUser,
			PluginRebuildOnLaunch: plugin == mode.PluginGeneratedPort,
		}
	}
}

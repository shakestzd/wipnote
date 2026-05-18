package mode_test

import (
	"os"
	"testing"

	"github.com/shakestzd/wipnote/internal/launcher/mode"
)

// TestDetectRuntimeMode_HostDevcontainerCI verifies runtime mode detection using
// marker files and environment variables, reusing the same signals as
// defaultDevcontainerDetector (/.dockerenv, CODESPACES, REMOTE_CONTAINERS).
func TestDetectRuntimeMode_HostDevcontainerCI(t *testing.T) {
	t.Run("host when no markers set", func(t *testing.T) {
		t.Setenv("CODESPACES", "")
		t.Setenv("REMOTE_CONTAINERS", "")
		t.Setenv("CI", "")
		t.Setenv("GITHUB_ACTIONS", "")

		got := mode.DetectRuntimeModeWith(func() bool { return false }, func() bool { return false })
		if got != mode.RuntimeHost {
			t.Errorf("want RuntimeHost, got %v", got)
		}
	})

	t.Run("devcontainer when CODESPACES=true", func(t *testing.T) {
		t.Setenv("CODESPACES", "true")
		t.Setenv("CI", "")
		t.Setenv("GITHUB_ACTIONS", "")

		got := mode.DetectRuntimeModeWith(
			func() bool { return os.Getenv("CODESPACES") == "true" },
			func() bool { return false },
		)
		if got != mode.RuntimeDevcontainer {
			t.Errorf("want RuntimeDevcontainer, got %v", got)
		}
	})

	t.Run("devcontainer when REMOTE_CONTAINERS=true", func(t *testing.T) {
		t.Setenv("REMOTE_CONTAINERS", "true")
		t.Setenv("CI", "")
		t.Setenv("GITHUB_ACTIONS", "")

		got := mode.DetectRuntimeModeWith(
			func() bool { return os.Getenv("REMOTE_CONTAINERS") == "true" },
			func() bool { return false },
		)
		if got != mode.RuntimeDevcontainer {
			t.Errorf("want RuntimeDevcontainer, got %v", got)
		}
	})

	t.Run("ci when GITHUB_ACTIONS=true", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "true")
		t.Setenv("CODESPACES", "")
		t.Setenv("REMOTE_CONTAINERS", "")

		got := mode.DetectRuntimeModeWith(
			func() bool { return false },
			func() bool { return os.Getenv("GITHUB_ACTIONS") == "true" },
		)
		if got != mode.RuntimeCI {
			t.Errorf("want RuntimeCI, got %v", got)
		}
	})

	t.Run("ci when CI=true", func(t *testing.T) {
		t.Setenv("CI", "true")
		t.Setenv("CODESPACES", "")
		t.Setenv("REMOTE_CONTAINERS", "")
		t.Setenv("GITHUB_ACTIONS", "")

		got := mode.DetectRuntimeModeWith(
			func() bool { return false },
			func() bool { return os.Getenv("CI") == "true" },
		)
		if got != mode.RuntimeCI {
			t.Errorf("want RuntimeCI, got %v", got)
		}
	})

	t.Run("ci takes priority over devcontainer", func(t *testing.T) {
		got := mode.DetectRuntimeModeWith(
			func() bool { return true },
			func() bool { return true },
		)
		if got != mode.RuntimeCI {
			t.Errorf("want RuntimeCI (CI takes priority), got %v", got)
		}
	})
}

// TestDashboardBindDefaults verifies host=127.0.0.1:8080, devcontainer=0.0.0.0:8088.
func TestDashboardBindDefaults(t *testing.T) {
	tests := []struct {
		name     string
		runtime  mode.RuntimeMode
		wantHost string
		wantPort int
	}{
		{"host runtime", mode.RuntimeHost, "127.0.0.1", 8080},
		{"devcontainer runtime", mode.RuntimeDevcontainer, "0.0.0.0", 8088},
		{"ci runtime", mode.RuntimeCI, "127.0.0.1", 8080},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, port := mode.DashboardBindDefaults(tc.runtime)
			if host != tc.wantHost {
				t.Errorf("host: got %q, want %q", host, tc.wantHost)
			}
			if port != tc.wantPort {
				t.Errorf("port: got %d, want %d", port, tc.wantPort)
			}
		})
	}
}

// TestDetectExecutionMode verifies execution mode detection.
func TestDetectExecutionMode(t *testing.T) {
	t.Run("in-place when no worktree path", func(t *testing.T) {
		got := mode.DetectExecutionModeWith("", false)
		if got != mode.ExecInPlace {
			t.Errorf("want ExecInPlace, got %v", got)
		}
	})

	t.Run("isolated-worktree when worktree path set", func(t *testing.T) {
		got := mode.DetectExecutionModeWith("/some/worktree", false)
		if got != mode.ExecIsolatedWorktree {
			t.Errorf("want ExecIsolatedWorktree, got %v", got)
		}
	})

	t.Run("read-only when readonly flag", func(t *testing.T) {
		got := mode.DetectExecutionModeWith("", true)
		if got != mode.ExecReadOnly {
			t.Errorf("want ExecReadOnly, got %v", got)
		}
	})
}

// TestDetectPluginMode verifies plugin mode detection.
func TestDetectPluginMode(t *testing.T) {
	t.Run("local-dev when dev plugin", func(t *testing.T) {
		got := mode.DetectPluginMode(true, false)
		if got != mode.PluginLocalDev {
			t.Errorf("want PluginLocalDev, got %v", got)
		}
	})

	t.Run("generated-port when generated port", func(t *testing.T) {
		got := mode.DetectPluginMode(false, true)
		if got != mode.PluginGeneratedPort {
			t.Errorf("want PluginGeneratedPort, got %v", got)
		}
	})

	t.Run("installed when neither", func(t *testing.T) {
		got := mode.DetectPluginMode(false, false)
		if got != mode.PluginInstalled {
			t.Errorf("want PluginInstalled, got %v", got)
		}
	})
}

// TestComputeLauncherMode verifies the full mode object on host runtime.
func TestComputeLauncherMode(t *testing.T) {
	m := mode.ComputeWith(mode.ComputeInput{
		IsDevcontainer: func() bool { return false },
		IsCI:           func() bool { return false },
		WorktreePath:   "",
		ReadOnly:       false,
		DevPlugin:      false,
		GeneratedPort:  false,
	})

	if m.Runtime != mode.RuntimeHost {
		t.Errorf("Runtime: want %v, got %v", mode.RuntimeHost, m.Runtime)
	}
	if m.Execution != mode.ExecInPlace {
		t.Errorf("Execution: want %v, got %v", mode.ExecInPlace, m.Execution)
	}
	if m.Plugin != mode.PluginInstalled {
		t.Errorf("Plugin: want %v, got %v", mode.PluginInstalled, m.Plugin)
	}
	if m.DashboardHost != "127.0.0.1" {
		t.Errorf("DashboardHost: want 127.0.0.1, got %q", m.DashboardHost)
	}
	if m.DashboardPort != 8080 {
		t.Errorf("DashboardPort: want 8080, got %d", m.DashboardPort)
	}
}

// TestComputeLauncherMode_Devcontainer verifies devcontainer bind defaults.
func TestComputeLauncherMode_Devcontainer(t *testing.T) {
	m := mode.ComputeWith(mode.ComputeInput{
		IsDevcontainer: func() bool { return true },
		IsCI:           func() bool { return false },
		WorktreePath:   "/some/path",
		ReadOnly:       false,
		DevPlugin:      true,
		GeneratedPort:  false,
	})

	if m.Runtime != mode.RuntimeDevcontainer {
		t.Errorf("Runtime: want %v, got %v", mode.RuntimeDevcontainer, m.Runtime)
	}
	if m.Execution != mode.ExecIsolatedWorktree {
		t.Errorf("Execution: want %v, got %v", mode.ExecIsolatedWorktree, m.Execution)
	}
	if m.Plugin != mode.PluginLocalDev {
		t.Errorf("Plugin: want %v, got %v", mode.PluginLocalDev, m.Plugin)
	}
	if m.DashboardHost != "0.0.0.0" {
		t.Errorf("DashboardHost: want 0.0.0.0, got %q", m.DashboardHost)
	}
	if m.DashboardPort != 8088 {
		t.Errorf("DashboardPort: want 8088, got %d", m.DashboardPort)
	}
}

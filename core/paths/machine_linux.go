package paths

import (
	"os"
	"strings"
)

// machineID is the systemd or D-Bus machine id. Containers built from one image
// can share it, so there it tells machines apart less well.
func machineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

package paths

import (
	"context"
	"os/exec"
	"regexp"
	"time"
)

var platformUUIDRe = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

// machineID is the hardware UUID (IOPlatformUUID), read from ioreg. Why not the
// kern.uuid sysctl: that names the kernel build. Two Macs on one macOS build
// share it, and a macOS update changes it (bug-65bf54aa).
func machineID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	if m := platformUUIDRe.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

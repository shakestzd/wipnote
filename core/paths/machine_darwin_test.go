package paths

import (
	"regexp"
	"syscall"
	"testing"
)

// The real macOS id is a UUID, is the same on each read, and is not the kernel
// build id that kern.uuid reports.
func TestMachineID_IsTheHardwareUUID(t *testing.T) {
	id := machineID()
	if !regexp.MustCompile(`^[0-9A-Fa-f-]{36}$`).MatchString(id) {
		t.Fatalf("machine id is not a UUID (length %d)", len(id))
	}
	if machineID() != id {
		t.Fatal("machine id changed between two reads")
	}
	if kern, err := syscall.Sysctl("kern.uuid"); err == nil && kern == id {
		t.Fatal("machine id equals kern.uuid, the kernel build id")
	}
}

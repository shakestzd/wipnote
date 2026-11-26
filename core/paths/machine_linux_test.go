package paths

import (
	"os"
	"testing"
)

// Where the system has a machine id file, machineID returns its value.
func TestMachineID_ReadsTheMachineIDFile(t *testing.T) {
	if _, err := os.Stat("/etc/machine-id"); err != nil {
		t.Skip("no /etc/machine-id here")
	}
	if id := machineID(); id == "" || machineID() != id {
		t.Fatal("machine id is empty or changed between two reads")
	}
}

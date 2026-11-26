package paths

import "syscall"

// machineID is the hardware UUID macOS reports as kern.uuid.
func machineID() string {
	id, err := syscall.Sysctl("kern.uuid")
	if err != nil {
		return ""
	}
	return id
}

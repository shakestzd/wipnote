package paths

import (
	"crypto/sha256"
	"encoding/hex"
)

// machineIDFn reads this machine's own id from the operating system. Tests
// replace it.
var machineIDFn = machineID

// machineMark is a short hash of this machine's id, or "" when the system gives
// none. A new store records it in project.json, so a launch can tell a store
// made here from one restored or synced from another machine with the same user
// name and project path (bug-65bf54aa). The hash keeps the raw id out of a file
// that the store's git history holds.
func machineMark() string {
	id := machineIDFn()
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("wipnote-machine:" + id))
	return hex.EncodeToString(sum[:8])
}

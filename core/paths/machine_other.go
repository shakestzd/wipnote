//go:build !darwin && !linux

package paths

// machineID is empty where wipnote reads no machine id. A store then records no
// mark, and StoreMadeHere falls back to its path check.
func machineID() string { return "" }

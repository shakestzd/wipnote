package db

// Test-only views of the migration registry for the external db_test package.

// CurrentSchemaVersion returns the highest migration step version.
func CurrentSchemaVersion() int { return currentSchemaVersion }

// MigrationStepNames returns the ordered migration step names.
func MigrationStepNames() []string {
	out := make([]string, len(migrations))
	for i, m := range migrations {
		out[i] = m.name
	}
	return out
}

// MigrationStepVersions returns the ordered migration step versions.
func MigrationStepVersions() []int {
	out := make([]int, len(migrations))
	for i, m := range migrations {
		out[i] = m.version
	}
	return out
}

// CopySwapStepName returns the name of the agent_events copy-and-swap step.
func CopySwapStepName() string { return copySwapStepName }

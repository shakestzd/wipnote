package gitwatch

// Ported from cli/cli pkg/cmd/pr/checks/aggregate.go (trunk at commit
// 4f2cfe92e3499f8581bf39db77e0a1431fa7b55c, fetched 2026-10-26).
// Copyright (c) 2019 GitHub Inc. Licensed under the MIT License; see
// THIRD-PARTY-NOTICES.md. Adapted to gitwatch's rawCheck type; the one
// behavioural change is that STARTUP_FAILURE maps to fail, not pending.

import (
	"fmt"
	"sort"
)

// bucketFor maps a check state to its bucket, as aggregateChecks does.
func bucketFor(state string) string {
	switch state {
	case "SUCCESS":
		return BucketPass
	case "SKIPPED", "NEUTRAL":
		return BucketSkipping
	case "ERROR", "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return BucketFail
	case "CANCELLED":
		return BucketCancel
	default: // "EXPECTED", "REQUESTED", "WAITING", "QUEUED", "PENDING", "IN_PROGRESS", "STALE"
		return BucketPending
	}
}

// eliminateDuplicates keeps only the most recent run of each check when the
// set includes repeated runs. gh's JSON does not separate status contexts
// from check runs; a status context has no workflow or event, so its name
// alone keys it, which matches upstream's context branch.
func eliminateDuplicates(checks []rawCheck) []rawCheck {
	sorted := append([]rawCheck(nil), checks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartedAt.After(sorted[j].StartedAt) })

	seen := make(map[string]struct{})
	unique := make([]rawCheck, 0, len(sorted))
	for _, c := range sorted {
		key := fmt.Sprintf("%s/%s/%s", c.Name, c.Workflow, c.Event)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, c)
	}
	return unique
}

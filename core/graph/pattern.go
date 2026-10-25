package graph

// Edge origin tags. Mechanical writers that synthesize graph_edges rows from
// some other source of truth (a plan YAML's slice.deps, a batch-apply spec,
// …) stamp one of these into the edge's metadata JSON under the "origin" key
// so that consumers can tell a derived edge apart from a human-asserted one
// (e.g. via `wipnote link add --rel blocks`, which leaves metadata NULL).
//
// EdgeOriginPlanSlice specifically marks blocked_by edges that encode a plan
// slice's authoring order (slice N declared "deps: [M]" in the plan YAML).
// That ordering is not an asserted cross-project dependency, so it must be
// excluded from FindBottlenecks — see bug-d0489158. It is written by both
// reindex_plan_edges.go (rebuilt on every reindex) and plan_wire.go (written
// once when a plan is wired to a track), since both derive from the same
// slice.deps field.
const EdgeOriginPlanSlice = "plan_slice_deps"

// EdgeOriginBatchApply marks blocked_by edges created by `wipnote batch
// apply` from a batch spec's inline `blocked_by:` declarations. Unlike
// EdgeOriginPlanSlice these represent a genuine asserted dependency between
// sibling features in the batch (equivalent in intent to `link add`, just
// authored in bulk) — so they are NOT excluded from FindBottlenecks. The tag
// exists purely for measurability: see bug-f55532ba.
const EdgeOriginBatchApply = "batch_apply"

// BottleneckResult represents a node that blocks other nodes.
type BottleneckResult struct {
	ID         string
	Title      string
	Status     string
	BlockCount int
}

package hooks

import (
	"context"
	"database/sql"
	"testing"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/daemon/apply"
)

// stubDaemonApplyInto stands in for a reachable writer daemon that applies
// every routed statement to database, through the same Encode → Decode →
// apply.NewApplier path the real daemon uses. Hooks have no fallback
// (feat-174dc958), so a test that reads back what a hook wrote must supply
// this daemon; without it every routed write is dropped.
func stubDaemonApplyInto(t *testing.T, database *sql.DB) {
	t.Helper()
	applier := apply.NewApplier(database)
	route := func(_ string, sqlStmt string, args ...any) bool {
		payload, err := apply.Encode(apply.DerivedOp{Type: apply.OpTypeSQL, SQL: sqlStmt, Args: args})
		if err != nil {
			return false
		}
		op, err := applier(daemon.Envelope{OpType: apply.OpTypeSQL, Payload: payload})
		if err != nil {
			return false
		}
		return op(context.Background()) == nil
	}
	prevAsync, prevApplied := routeSQLAsync, routeSQLApplied
	t.Cleanup(func() { routeSQLAsync, routeSQLApplied = prevAsync, prevApplied })
	routeSQLAsync, routeSQLApplied = route, route
}

package db

import (
	"time"
)

// The arch_cards mirror is retired and nothing in wipnote calls into this file
// any more. Architecture cards are read from their canonical HTML store via
// core/arch.Store; the mirror was a second copy of the same data with a
// reindex-time sync obligation and no reader that needed it (spk-e6e82b5a).
//
// The table, its migration, and these functions are kept so that restoring the
// mirror is re-adding the UpsertArchCard call in reindexArchCards and nothing
// else. Delete this file and the table once the change has held.

// ArchCardRow is the SQLite representation of an architectural memory card.
// It mirrors the (retired) arch_cards table schema.
type ArchCardRow struct {
	Slug         string
	Kind         string
	PathsJSON    string
	VerifiedAt   string
	LinksJSON    string
	CreatedBy    string
	SupersededBy string
	Retired      bool
	Body         string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

package db

// allTablesDDL is the CREATE TABLE IF NOT EXISTS statement for every wipnote
// table, in the order CreateAllTables executes them.
// Mirrors create_all_tables() from Python ddl.py.
var allTablesDDL = []string{
	// 1. agent_events
	`CREATE TABLE IF NOT EXISTS agent_events (
			event_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			event_type TEXT NOT NULL CHECK(
				event_type IN ('tool_call','tool_result','error','delegation',
				               'completion','start','end','check_point','task_delegation',
				               'teammate_idle','task_created','task_completed','quality_gate',
				               'claim.proposed','claim.claimed','claim.heartbeat','claim.blocked',
				               'claim.completed','claim.abandoned','claim.expired','claim.handoff')
			),
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			tool_name TEXT,
			input_summary TEXT,
			tool_input JSON,
			output_summary TEXT,
			context JSON,
			session_id TEXT NOT NULL,
			feature_id TEXT,
			parent_agent_id TEXT,
			parent_event_id TEXT,
			subagent_type TEXT,
			child_spike_count INTEGER DEFAULT 0,
			cost_tokens INTEGER DEFAULT 0,
			execution_duration_seconds REAL DEFAULT 0.0,
			status TEXT DEFAULT 'recorded',
			model TEXT,
			claude_task_id TEXT,
			source TEXT DEFAULT 'hook',
			step_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			CHECK (NOT (event_type = 'tool_call' AND agent_id = 'human' AND (tool_name IS NULL OR tool_name != 'UserQuery'))),
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE ON UPDATE CASCADE,
			FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE SET NULL ON UPDATE CASCADE
		)`,

	// 2. features
	`CREATE TABLE IF NOT EXISTS features (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK(
				type IN ('feature','bug','spike','chore','epic','task','plan','spec')
			),
			title TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'todo' CHECK(
				status IN ('todo','in-progress','blocked','done','active','ended','stale')
			),
			priority TEXT DEFAULT 'medium' CHECK(
				priority IN ('low','medium','high','critical')
			),
			assigned_to TEXT,
			assignee TEXT DEFAULT NULL,
			track_id TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			steps_total INTEGER DEFAULT 0,
			steps_completed INTEGER DEFAULT 0,
			parent_feature_id TEXT,
			tags JSON,
			metadata JSON,
			FOREIGN KEY (track_id) REFERENCES tracks(id),
			FOREIGN KEY (parent_feature_id) REFERENCES features(id)
		)`,

	// 3. sessions
	`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			agent_assigned TEXT NOT NULL,
			parent_session_id TEXT,
			parent_event_id TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			total_events INTEGER DEFAULT 0,
			total_tokens_used INTEGER DEFAULT 0,
			context_drift REAL DEFAULT 0.0,
			status TEXT NOT NULL DEFAULT 'active' CHECK(
				status IN ('active','completed','paused','failed')
			),
			transcript_id TEXT,
			transcript_path TEXT,
			transcript_synced DATETIME,
			start_commit TEXT,
			end_commit TEXT,
			is_subagent BOOLEAN DEFAULT FALSE,
			features_worked_on JSON,
			metadata JSON,
			last_user_query_at DATETIME,
			last_user_query TEXT,
			handoff_notes TEXT,
			recommended_next TEXT,
			blockers JSON,
			recommended_context JSON,
			continued_from TEXT,
			session_family_id TEXT,
			cost_budget REAL,
			cost_threshold_breached INTEGER DEFAULT 0,
			predicted_cost REAL DEFAULT 0.0,
			model TEXT,
			active_feature_id TEXT,
			updated_at DATETIME,
			exec_worktree_path TEXT,
			branch TEXT,
			harness TEXT,
			FOREIGN KEY (parent_session_id) REFERENCES sessions(session_id) ON DELETE SET NULL ON UPDATE CASCADE,
			FOREIGN KEY (parent_event_id) REFERENCES agent_events(event_id) ON DELETE SET NULL ON UPDATE CASCADE,
			FOREIGN KEY (continued_from) REFERENCES sessions(session_id) ON DELETE SET NULL ON UPDATE CASCADE
		)`,

	// 4. tracks
	`CREATE TABLE IF NOT EXISTS tracks (
			id TEXT PRIMARY KEY,
			type TEXT DEFAULT 'track',
			title TEXT NOT NULL,
			description TEXT,
			priority TEXT DEFAULT 'medium' CHECK(
				priority IN ('low','medium','high','critical')
			),
			status TEXT NOT NULL DEFAULT 'todo' CHECK(
				status IN ('todo','in-progress','blocked','done','active','ended','stale')
			),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			features JSON,
			metadata JSON
		)`,

	// 5. claims
	`CREATE TABLE IF NOT EXISTS claims (
			claim_id TEXT PRIMARY KEY,
			work_item_id TEXT NOT NULL,
			track_id TEXT,
			owner_session_id TEXT NOT NULL,
			owner_agent TEXT NOT NULL DEFAULT 'claude-code',
			status TEXT NOT NULL DEFAULT 'proposed' CHECK(
				status IN ('proposed','claimed','in_progress','blocked',
				           'handoff_pending','completed','abandoned','expired','rejected')
			),
			intended_output TEXT,
			write_scope JSON,
			leased_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			lease_expires_at DATETIME NOT NULL,
			last_heartbeat_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			dependencies JSON,
			progress_notes TEXT,
			blocker_reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (work_item_id) REFERENCES features(id) ON DELETE CASCADE,
			FOREIGN KEY (owner_session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		)`,

	// 6. graph_edges
	`CREATE TABLE IF NOT EXISTS graph_edges (
			edge_id TEXT PRIMARY KEY,
			from_node_id TEXT NOT NULL,
			from_node_type TEXT NOT NULL,
			to_node_id TEXT NOT NULL,
			to_node_type TEXT NOT NULL,
			relationship_type TEXT NOT NULL,
			weight REAL DEFAULT 1.0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			metadata JSON
		)`,

	// 7. git_commits
	// feature_id is NOT NULL DEFAULT '' (never NULL) because it is part of
	// the primary key: two NULLs are never equal under a UNIQUE index, so a
	// nullable PK column would silently defeat INSERT OR IGNORE
	// de-duplication for unattributed commits. See migration 018 (bug-3bf05d49)
	// for why feature_id had to join the key: a single commit can
	// legitimately name multiple work items (e.g. "Refs: feat-a, feat-b"),
	// and the old two-column key silently dropped every ID after the first.
	`CREATE TABLE IF NOT EXISTS git_commits (
			commit_hash TEXT NOT NULL,
			session_id TEXT NOT NULL,
			feature_id TEXT NOT NULL DEFAULT '',
			tool_event_id TEXT,
			message TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (commit_hash, session_id, feature_id)
		)`,

	// 8. live_events
	`CREATE TABLE IF NOT EXISTS live_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			event_data TEXT NOT NULL,
			parent_event_id TEXT,
			session_id TEXT,
			spawner_type TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			broadcast_at TIMESTAMP
		)`,

	// 9. agent_lineage_trace
	`CREATE TABLE IF NOT EXISTS agent_lineage_trace (
			trace_id TEXT PRIMARY KEY,
			root_session_id TEXT NOT NULL,
			session_id TEXT,
			agent_name TEXT,
			depth INTEGER DEFAULT 0,
			path TEXT,
			feature_id TEXT,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			status TEXT DEFAULT 'active'
		)`,

	// 10. messages (transcript data from Claude Code JSONL)
	`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user','assistant')),
			content TEXT NOT NULL DEFAULT '',
			timestamp DATETIME,
			has_thinking INTEGER DEFAULT 0,
			has_tool_use INTEGER DEFAULT 0,
			content_length INTEGER DEFAULT 0,
			model TEXT,
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0,
			stop_reason TEXT,
			uuid TEXT,
			parent_uuid TEXT,
			UNIQUE(session_id, ordinal)
		)`,

	// 11. tool_calls (extracted from assistant messages)
	`CREATE TABLE IF NOT EXISTS tool_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'Other',
			tool_use_id TEXT,
			input_json TEXT,
			result_content_length INTEGER DEFAULT 0,
			subagent_session_id TEXT,
			feature_id TEXT
		)`,

	// 12. feature_files — file paths touched by each feature
	`CREATE TABLE IF NOT EXISTS feature_files (
			id TEXT PRIMARY KEY,
			feature_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			operation TEXT NOT NULL DEFAULT 'unknown',
			session_id TEXT,
			first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(feature_id, file_path)
		)`,

	// 12b. session_files — file paths touched by a session WITHOUT an active
	// claim/feature (claimless visibility, feat-793844bd slice-4). Distinct
	// from feature_files because feature_files.feature_id is NOT NULL with
	// UNIQUE(feature_id,file_path) and cannot hold empty-feature_id rows.
	// Keyed on (session_id, file_path) so a session's claimless touches are
	// upserted idempotently with a single statement and zero hot-path write
	// amplification.
	`CREATE TABLE IF NOT EXISTS session_files (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			operation TEXT NOT NULL DEFAULT 'unknown',
			first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(session_id, file_path)
		)`,

	// 13. agent_presence
	`CREATE TABLE IF NOT EXISTS agent_presence (
			agent_id TEXT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'offline' CHECK(
				status IN ('active','idle','offline')
			),
			current_feature_id TEXT,
			last_tool_name TEXT,
			last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			total_tools_executed INTEGER DEFAULT 0,
			total_cost_tokens INTEGER DEFAULT 0,
			session_id TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (current_feature_id) REFERENCES features(id) ON DELETE SET NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE SET NULL
		)`,

	// 14. metadata — key-value store for operational state (e.g. last_indexed_commit)
	`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		)`,

	// 15. plan_feedback — structured feedback from CRISPI plan review.
	// anchor / consumed / resolved / resolution_target (migration 016)
	// carry block-level annotation state: anchor pins a note to a specific
	// plan block, and consumed vs resolved are two independent axes routed
	// to agent|human via resolution_target. Non-annotation rows leave them
	// at their zero defaults.
	`CREATE TABLE IF NOT EXISTS plan_feedback (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			plan_id TEXT NOT NULL,
			section TEXT NOT NULL,
			action TEXT NOT NULL,
			value TEXT,
			question_id TEXT NOT NULL DEFAULT '',
			anchor TEXT NOT NULL DEFAULT '',
			consumed INTEGER NOT NULL DEFAULT 0,
			resolved INTEGER NOT NULL DEFAULT 0,
			resolution_target TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(plan_id, section, action, question_id)
		)`,

	// 16. arch_cards — read index for architectural memory cards (.wipnote/arch/*.md)
	// Canonical store is the .md files; this table is derived and never authoritative.
	`CREATE TABLE IF NOT EXISTS arch_cards (
			slug           TEXT PRIMARY KEY,
			kind           TEXT NOT NULL,
			paths_json     TEXT NOT NULL DEFAULT '[]',
			verified_at    TEXT NOT NULL DEFAULT '',
			links_json     TEXT NOT NULL DEFAULT '[]',
			created_by     TEXT NOT NULL DEFAULT '',
			superseded_by  TEXT NOT NULL DEFAULT '',
			retired        INTEGER NOT NULL DEFAULT 0,
			body           TEXT NOT NULL DEFAULT '',
			created_at     DATETIME,
			updated_at     DATETIME,
			indexed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

	// 16b. claim_episodes — read index for the claim ledger
	// (.wipnote/claims/*.html). Canonical store is the HTML; this table is
	// derived and never authoritative, and a cache wipe rebuilds it whole.
	//
	// Unlike claims/active_work_items — which are single-mutable-slot CURRENT
	// state — a row here is a closed or open INTERVAL, so a signal emitted at
	// time T by agent A has something to join against.
	//
	// started_at/ended_at are TEXT in claimledger.TimeFormat: RFC3339 UTC with
	// fixed nine-digit fractions, so TEXT comparison IS chronological ordering.
	// ended_at is '' (never NULL) while the episode is open, which keeps the
	// interval predicate a plain string comparison with no COALESCE.
	`CREATE TABLE IF NOT EXISTS claim_episodes (
			episode_id      TEXT PRIMARY KEY,
			work_item_id    TEXT NOT NULL,
			session_id      TEXT NOT NULL,
			root_session_id TEXT NOT NULL DEFAULT '',
			agent_id        TEXT NOT NULL,
			started_at      TEXT NOT NULL,
			ended_at        TEXT NOT NULL DEFAULT '',
			outcome         TEXT NOT NULL DEFAULT '',
			source_file     TEXT NOT NULL DEFAULT '',
			indexed_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

	// 17. gate_records — DERIVED quality-gate runs, projected from the
	// canonical ledger at .wipnote/gate-ledger.html (feat-0e5ca43e). record_id
	// is that ledger row's id and the key the projection is idempotent on; ""
	// marks a legacy row written before the ledger existed.
	`CREATE TABLE IF NOT EXISTS gate_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			record_id TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL,
			work_item_id TEXT,
			harness TEXT,
			project_type TEXT NOT NULL,
			gate_command TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pass','fail')),
			checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			signature TEXT NOT NULL,
			allowlist_hits_json TEXT NOT NULL DEFAULT '[]',
			allowlist_hit_count INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'check',
			output_summary TEXT,
			profile_signature TEXT NOT NULL DEFAULT '',
			guards_run TEXT NOT NULL DEFAULT '[]'
		)`,

	// 18. recaps — read index for committed recap artifacts (.wipnote/recaps/*.html).
	// Canonical store is the HTML files; this table is derived and never authoritative.
	`CREATE TABLE IF NOT EXISTS recaps (
			id            TEXT PRIMARY KEY,
			kind          TEXT NOT NULL DEFAULT '',
			input         TEXT NOT NULL DEFAULT '',
			git_range     TEXT NOT NULL DEFAULT '',
			grounded      INTEGER NOT NULL DEFAULT 0,
			title         TEXT NOT NULL DEFAULT '',
			outcome       TEXT NOT NULL DEFAULT '',
			work_item_id  TEXT NOT NULL DEFAULT '',
			created_at    DATETIME,
			updated_at    DATETIME,
			indexed_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
}

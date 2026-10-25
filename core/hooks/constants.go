package hooks

const (
	// --- Project Directory Resolution ---
	// Default walk-up levels when resolving .wipnote/ directory
	defaultProjectDirWalkLevels = 10

	// --- Text Truncation Limits ---
	// Maximum characters for prompt summaries in UserPrompt hook
	promptSummaryMaxLen = 300
	// Maximum characters for output summaries in SubagentStop hook
	outputSummaryMaxLen = 500
	// Maximum characters for session last_user_query field
	sessionQueryMaxLen = 200
	// Maximum characters for debug log messages in missing_events
	debugMsgMaxLen = 200

	// --- Code Quality Limits ---
	// Module size warning threshold (lines of code)
	moduleWarnLines = 300
	// Module size hard limit for new code (lines of code)
	moduleLimitLines = 500
	// Function length warning threshold (lines of code)
	funcWarnLines = 30
	// Function length hard limit for new code (lines of code)
	funcLimitLines = 50
	// Minimum lines per duplicate block for duplication detection
	dupBlockSize = 5

	// --- YOLO Mode Hard Limits ---
	// Maximum file count in a single YOLO commit
	yoloBudgetMaxFiles = 20
	// Maximum lines added in a single YOLO commit
	yoloBudgetMaxLines = 600
	// Code health threshold for YOLO mode (file size limit)
	yoloCodeHealthMaxLines = 500
)

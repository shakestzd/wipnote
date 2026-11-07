package hooks

const (
	// --- Project Directory Resolution ---
	// Default walk-up levels when resolving .wipnote/ directory
	defaultProjectDirWalkLevels = 10

	// --- Code Quality Limits ---
	// The size rule (GitHub #312) has one trigger per unit, and these are the
	// only copy of it: the post-edit hook, `wipnote health`, the changed-file
	// size gate in `wipnote check --gate` and `wipnote review` all read them.
	// They are review triggers, not measures: crossing one means "check for
	// more than one reason to change", not "split". The numbers are
	// conventions; no study found tests a length cap against defects with
	// other factors controlled.
	//
	// ModuleLimitLines is the module (file) size review trigger, in lines.
	ModuleLimitLines = 500
	// FuncLimitLines is the function length review trigger, in lines.
	FuncLimitLines = 50
	// SizeOKMarker, followed by ": <one-line reason>" in a comment, records why
	// code over a trigger stays whole. Above a function it covers that
	// function; in a file's first SizeOKFileWindow lines it covers the file.
	SizeOKMarker = "wipnote:size-ok"
	// SizeOKFileWindow is how many leading lines may carry the file marker.
	SizeOKFileWindow = 20
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

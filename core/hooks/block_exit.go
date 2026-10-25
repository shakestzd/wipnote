package hooks

import "fmt"

// BlockExit2Error is a sentinel error that signals the hook runner to exit
// with code 2 (block) after writing the message to stderr. Claude Code
// interprets exit code 2 as "hook blocks this action".
type BlockExit2Error struct {
	Message string
}

func (e *BlockExit2Error) Error() string {
	return fmt.Sprintf("hook blocked: %s", e.Message)
}

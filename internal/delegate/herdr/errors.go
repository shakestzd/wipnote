package herdr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors. Test them with errors.Is.
var (
	// ErrNotInHerdr: HERDR_ENV is not 1, so herdr was not run at all.
	ErrNotInHerdr = errors.New("herdr: not inside a herdr pane (HERDR_ENV is not 1)")
	// ErrAbsent: no herdr binary on PATH.
	ErrAbsent = errors.New("herdr: binary not found on PATH")
	// ErrTimeout: the client's own deadline expired and the call was killed.
	// herdr's server-side "timeout" error code is an *Error, not this.
	ErrTimeout = errors.New("herdr: call timed out")
	// ErrUsage: herdr exited 2, a CLI syntax error. Never a timeout.
	ErrUsage = errors.New("herdr: usage error")
	// ErrBadReply: herdr succeeded but printed something we cannot parse.
	ErrBadReply = errors.New("herdr: unreadable reply")
	// ErrPin: the pinned agent is not in its pane any more, or herdr cannot
	// say who is; nothing was sent.
	ErrPin = errors.New("herdr: pinned agent mismatch")
	// ErrNoCallerPane: HERDR_PANE_ID is not set inside herdr, so the caller's
	// workspace is unknown and no tab may be created.
	ErrNoCallerPane = errors.New("herdr: HERDR_PANE_ID is not set")
)

// Codes the client assigns itself when herdr gave none.
const (
	CodeUsage = "usage"
	CodeOther = "herdr_error"
)

// absentCodes are herdr error codes meaning "no such pane or tab".
var absentCodes = map[string]bool{"not_found": true, "pane_not_found": true, "tab_not_found": true}

// Error is a failed herdr call: its exit status and herdr's error code and
// message (from the JSON on stderr for exit 1).
type Error struct {
	Args    string
	Exit    int
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("herdr %s: exit %d: %s: %s", e.Args, e.Exit, e.Code, e.Message)
}

// Is makes errors.Is(err, ErrUsage) true for exit 2.
func (e *Error) Is(target error) bool { return target == ErrUsage && e.Exit == 2 }

// IsAbsent reports whether herdr said the pane or tab does not exist. Any
// other failure is not proof of absence.
func IsAbsent(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.Exit == 1 && absentCodes[he.Code]
}

// classify turns a non-zero exit into an *Error. Exit 2 is decided by the exit
// code before any text is read, so "unknown option: --timeout" stays a usage
// error. Exit 1 carries {"error":{"code","message"}} on stderr (api-schema
// ErrorResponse); without it the code is CodeOther. herdr's text is never
// scanned for codes.
func classify(args []string, exit int, stderr []byte) *Error {
	e := &Error{Args: strings.Join(args, " "), Exit: exit, Message: firstLine(stderr)}
	switch {
	case exit == 2:
		e.Code = CodeUsage
	case exit == 1:
		e.Code = CodeOther
		var body struct {
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(stderr), &body) == nil && body.Error != nil && body.Error.Code != "" {
			e.Code, e.Message = body.Error.Code, body.Error.Message
		}
	default:
		e.Code = fmt.Sprintf("exit_%d", exit)
	}
	return e
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

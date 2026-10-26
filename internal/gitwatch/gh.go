package gitwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ghExitAuth is gh's documented exit status for "authentication required".
const ghExitAuth = 4

// authHints are stderr fragments gh prints when it is not signed in or its
// token is rejected. Any of them makes the call ErrNotAvailable.
var authHints = []string{"gh auth login", "not logged in", "HTTP 401", "Bad credentials"}

// ghJSON runs gh and decodes its stdout into out. stdout is parsed first,
// whatever the exit code, because gh exits 8 (pending) and 1 (failed) with
// valid JSON. When stdout does not parse, the error carries the first line
// of stderr.
func ghJSON(ctx context.Context, r Runner, out any, args ...string) error {
	stdout, stderr, code, err := r.Run(ctx, "gh", args...)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(stdout))) > 0 && json.Unmarshal(stdout, out) == nil {
		return nil
	}
	first := firstLine(stderr)
	if code == ghExitAuth || hasAuthHint(first) {
		return fmt.Errorf("gh is not signed in (%s): %w", first, ErrNotAvailable)
	}
	if first == "" {
		first = fmt.Sprintf("exit %d with unreadable output", code)
	}
	return fmt.Errorf("gh %s: %s", strings.Join(args[:min(2, len(args))], " "), first)
}

func firstLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(line)
}

func hasAuthHint(line string) bool {
	for _, h := range authHints {
		if strings.Contains(line, h) {
			return true
		}
	}
	return false
}

package container

import (
	"encoding/json"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

// loginResult turns the output of a successful `claude auth status` into the
// check result. Newer CLIs print JSON ({"loggedIn": true, "authMethod": ...,
// "email": ...}): pass or fail on loggedIn and show only the auth method,
// never the email or any other field. Older CLIs print plain text and exit 0
// when logged in.
func loginResult(c codex.Check, out string) codex.Check {
	var st struct {
		LoggedIn   *bool  `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &st) != nil || st.LoggedIn == nil {
		c.Status, c.Detail = codex.CheckPass, "logged in"
		return c
	}
	if !*st.LoggedIn {
		c.Status = codex.CheckFail
		c.Detail = "Claude Code is not logged in inside the container; log in once inside: open a shell in the devcontainer, start claude and type /login (the named ~/.claude volume keeps the login)"
		return c
	}
	c.Status, c.Detail = codex.CheckPass, "logged in"
	if st.AuthMethod != "" {
		c.Detail += " (" + st.AuthMethod + ")"
	}
	return c
}

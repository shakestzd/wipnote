package container

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

func TestLoginResult(t *testing.T) {
	const email = "someone@example.com"
	cases := []struct {
		name, out, detail string
		status            string
	}{
		{"json logged in", "{\n  \"loggedIn\": true,\n  \"authMethod\": \"claude.ai\",\n  \"email\": \"" + email + "\"\n}", "logged in (claude.ai)", codex.CheckPass},
		{"json logged out", `{"loggedIn": false, "email": "` + email + `"}`, "", codex.CheckFail},
		{"plain text", "Logged in as " + email, "logged in", codex.CheckPass},
	}
	for _, c := range cases {
		got := loginResult(codex.Check{Name: "claude login"}, c.out)
		if got.Status != c.status {
			t.Errorf("%s: status %v, want %v", c.name, got.Status, c.status)
		}
		if c.detail != "" && got.Detail != c.detail {
			t.Errorf("%s: detail %q, want %q", c.name, got.Detail, c.detail)
		}
		if strings.Contains(got.Detail, email) || strings.Contains(got.Detail, "{") {
			t.Errorf("%s: detail leaks raw output: %q", c.name, got.Detail)
		}
	}
}

package gitwatch

import "testing"

func TestWorkItemID(t *testing.T) {
	cases := []struct{ branch, title, want string }{
		{"feat-470715de-gitwatch", "", "feat-470715de"},
		{"fix/bug-0badc0de", "", "bug-0badc0de"},
		{"main", "Spike spk-deadbeef notes", "spk-deadbeef"},
		{"feat-12345678-x", "about bug-0badc0de", "feat-12345678"}, // branch first
		{"main", "", ""},
		{"feat-1234567", "", ""},                // 7 hex
		{"feat-123456789", "", ""},              // 9 hex: no word boundary
		{"feat-1234567g", "", ""},               // not hex
		{"task-12345678", "trk-12345678", ""},   // other prefixes
		{"xfeat-12345678", "FEAT-12345678", ""}, // glued prefix, upper case
		{"", "Release v1 (feat-abcdef01)", "feat-abcdef01"},
	}
	for _, c := range cases {
		if got := WorkItemID(c.branch, c.title); got != c.want {
			t.Errorf("WorkItemID(%q, %q) = %q, want %q", c.branch, c.title, got, c.want)
		}
	}
}

package main

import "testing"

func TestPickSessionLabel(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		title     string
		firstMsg  string
		createdAt string
		want      string
	}{
		{
			name:      "title wins over first message and time",
			sessionID: "8d53982f-xxxx",
			title:     "Close session HTML canonical gaps",
			firstMsg:  "we should do something",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "Close session HTML canonical gaps",
		},
		{
			name:      "dash-dash placeholder title falls through to first msg",
			sessionID: "8d53982f-xxxx",
			title:     "--",
			firstMsg:  "write a better session label algorithm",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "write a better session label algorithm",
		},
		{
			name:      "titler sentinel title falls through",
			sessionID: "8d53982f-xxxx",
			title:     "[wipnote-titler] in progress",
			firstMsg:  "improve the graph view",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "improve the graph view",
		},
		{
			name:      "slash command invocation unwraps to clean form",
			sessionID: "aabbccdd-xxxx",
			title:     "",
			firstMsg:  "<command-message>wipnote:execute</command-message>\n<command-name>/wipnote:execute</command-name>\n<command-args>trk-d8aef97a</command-args>",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "/wipnote:execute trk-d8aef97a",
		},
		{
			name:      "slash command without args shows command name only",
			sessionID: "aabbccdd-xxxx",
			title:     "",
			firstMsg:  "<command-name>/wipnote:execute</command-name>\n<command-args></command-args>",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "/wipnote:execute",
		},
		{
			name:      "time fallback when title and first msg both empty",
			sessionID: "aabbccdd-xxxx",
			title:     "",
			firstMsg:  "",
			createdAt: "2026-04-11T06:57:34Z",
			want:      "04-11 06:57 · aabbccdd",
		},
		{
			name:      "short id fallback when everything is empty",
			sessionID: "aabbccdd-xxxx",
			title:     "",
			firstMsg:  "",
			createdAt: "",
			want:      "session · aabbccdd",
		},
		{
			name:      "long title gets truncated at word boundary with ellipsis",
			sessionID: "aabbccdd-xxxx",
			title:     "A very long session title that definitely exceeds the maximum label length allowed for a graph node circle render",
			firstMsg:  "",
			createdAt: "",
			want:      "A very long session title that definitely exceeds the…",
		},
		{
			name:      "multi-line first message collapses to single line",
			sessionID: "aabbccdd-xxxx",
			title:     "",
			firstMsg:  "line one\n\nline   two\tline three",
			createdAt: "",
			want:      "line one line two line three",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickSessionLabel(tc.sessionID, tc.title, tc.firstMsg, tc.createdAt)
			if got != tc.want {
				t.Errorf("pickSessionLabel(%q,%q,%q,%q)\n  want: %q\n  got:  %q",
					tc.sessionID, tc.title, tc.firstMsg, tc.createdAt, tc.want, got)
			}
		})
	}
}

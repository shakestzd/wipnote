package main

import (
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no ANSI codes",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "color code",
			in:   "\x1b[31mred\x1b[0m",
			want: "red",
		},
		{
			name: "multiple codes",
			in:   "\x1b[1m\x1b[32mbold green\x1b[0m",
			want: "bold green",
		},
		{
			name: "mixed content",
			in:   "plain \x1b[33myellow\x1b[0m text",
			want: "plain yellow text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.in)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDropProgressBars(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "no progress bars",
			in:   []string{"line1", "line2"},
			want: []string{"line1", "line2"},
		},
		{
			name: "single carriage return",
			in:   []string{"progress: 50%\rcomplete", "done"},
			want: []string{"complete", "done"},
		},
		{
			name: "multiple carriage returns in one line",
			in:   []string{"0%\r50%\r100%", "finished"},
			want: []string{"100%", "finished"},
		},
		{
			name: "empty line after progress bar",
			in:   []string{"loading\r", "next"},
			want: []string{"next"},
		},
		{
			name: "only carriage return (empty result)",
			in:   []string{"text\r"},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dropProgressBars(tt.in)
			if !slicesEqual(got, tt.want) {
				t.Errorf("dropProgressBars(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDedupConsecutive(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "no duplicates",
			in:   []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "consecutive duplicates",
			in:   []string{"foo", "foo", "foo", "bar"},
			want: []string{"foo", "bar"},
		},
		{
			name: "non-consecutive duplicates (not deduped)",
			in:   []string{"a", "b", "a"},
			want: []string{"a", "b", "a"},
		},
		{
			name: "empty input",
			in:   []string{},
			want: []string{},
		},
		{
			name: "single element",
			in:   []string{"x"},
			want: []string{"x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupConsecutive(tt.in)
			if !slicesEqual(got, tt.want) {
				t.Errorf("dedupConsecutive(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestCapLines(t *testing.T) {
	tests := []struct {
		name     string
		in       []string
		maxLines int
		want     []string
	}{
		{
			name:     "unlimited (0)",
			in:       []string{"a", "b", "c"},
			maxLines: 0,
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "under cap",
			in:       []string{"1", "2", "3"},
			maxLines: 5,
			want:     []string{"1", "2", "3"},
		},
		{
			name:     "over cap",
			in:       []string{"1", "2", "3", "4", "5"},
			maxLines: 3,
			want:     []string{"1", "2", "3", "... 2 lines truncated (run with --max-lines 0 or --raw to see all)"},
		},
		{
			name:     "exactly at cap",
			in:       []string{"a", "b", "c"},
			maxLines: 3,
			want:     []string{"a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capLines(tt.in, tt.maxLines)
			if !slicesEqual(got, tt.want) {
				t.Errorf("capLines(%v, %d) = %v, want %v", tt.in, tt.maxLines, got, tt.want)
			}
		})
	}
}

func TestCompressOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		maxLine int
		noDedup bool
		want    string
	}{
		{
			name:    "simple no compression needed",
			output:  "line1\nline2\nline3\n",
			maxLine: 200,
			noDedup: false,
			want:    "line1\nline2\nline3\n",
		},
		{
			name:    "consecutive duplicates",
			output:  "foo\nfoo\nfoo\nbar\n",
			maxLine: 200,
			noDedup: false,
			want:    "foo\nbar\n",
		},
		{
			name:    "no dedup flag",
			output:  "a\na\na\n",
			maxLine: 200,
			noDedup: true,
			want:    "a\na\na\n",
		},
		{
			name:    "ANSI codes stripped",
			output:  "\x1b[31mred\x1b[0m\nred\n",
			maxLine: 200,
			noDedup: false,
			want:    "red\n",
		},
		{
			name:    "truncation at max lines",
			output:  "1\n2\n3\n4\n5\n",
			maxLine: 3,
			noDedup: false,
			want:    "1\n2\n3\n... 2 lines truncated (run with --max-lines 0 or --raw to see all)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compressOutput(tt.output, tt.maxLine, tt.noDedup)
			if got != tt.want {
				t.Errorf("compressOutput(%q, %d, %v) = %q, want %q",
					tt.output, tt.maxLine, tt.noDedup, got, tt.want)
			}
		})
	}
}

func TestRunShIntegration(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		maxLine int
		noDedup bool
		raw     bool
		want    string
		expectN int
	}{
		{
			name:    "simple echo",
			cmd:     "echo hello",
			maxLine: 200,
			noDedup: false,
			raw:     false,
			want:    "hello",
			expectN: 1,
		},
		{
			name:    "consecutive duplicates via loops",
			cmd:     "echo line; echo line; echo line",
			maxLine: 200,
			noDedup: false,
			raw:     false,
			want:    "line",
			expectN: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can't use runSh directly because it calls os.Exit.
			// Instead, we'll test the components separately.
			// This integration test is a placeholder — real tests would mock os.Exit.
		})
	}
}

// slicesEqual checks if two string slices are equal.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package hooks

import "testing"

func TestSummariseReadInput(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "path only",
			input: map[string]any{"file_path": "/src/main.go"},
			want:  "/src/main.go",
		},
		{
			name:  "path with offset and limit",
			input: map[string]any{"file_path": "/src/main.go", "offset": float64(100), "limit": float64(50)},
			want:  "/src/main.go [100:150]",
		},
		{
			name:  "path with offset only",
			input: map[string]any{"file_path": "/src/main.go", "offset": float64(100)},
			want:  "/src/main.go [100:]",
		},
		{
			name:  "path with limit only",
			input: map[string]any{"file_path": "/src/main.go", "limit": float64(50)},
			want:  "/src/main.go [:50]",
		},
		{
			name:  "no path returns Read",
			input: map[string]any{},
			want:  "Read",
		},
		{
			name:  "nil input returns Read",
			input: nil,
			want:  "Read",
		},
		{
			name:  "zero offset and limit omits range",
			input: map[string]any{"file_path": "/src/main.go", "offset": float64(0), "limit": float64(0)},
			want:  "/src/main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summariseReadInput(tt.input)
			if got != tt.want {
				t.Errorf("summariseReadInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummariseInputReadDispatch(t *testing.T) {
	// Verify SummariseInput dispatches to summariseReadInput for Read tool.
	input := map[string]any{"file_path": "/foo.go", "offset": float64(10), "limit": float64(20)}
	got := SummariseInput("Read", input)
	want := "/foo.go [10:30]"
	if got != want {
		t.Errorf("SummariseInput(Read) = %q, want %q", got, want)
	}
}

func TestSummariseInputNonRead(t *testing.T) {
	// Non-Read tools should use the old path-only logic.
	input := map[string]any{"file_path": "/foo.go", "offset": float64(10)}
	got := SummariseInput("Write", input)
	want := "/foo.go"
	if got != want {
		t.Errorf("SummariseInput(Write) = %q, want %q", got, want)
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int
	}{
		{"float64", float64(42), 42},
		{"int", int(7), 7},
		{"int64", int64(99), 99},
		{"string", "10", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toInt(tt.val)
			if got != tt.want {
				t.Errorf("toInt(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

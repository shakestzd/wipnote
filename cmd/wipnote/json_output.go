package main

import (
	"encoding/json"
	"io"
	"os"
)

// writeJSON writes v to w as two-space indented JSON followed by a newline.
// The bytes match json.MarshalIndent(v, "", "  ") plus "\n", which is what
// the --json outputs printed before they shared this helper.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printJSON writes v to stdout as indented JSON (see writeJSON).
func printJSON(v any) error {
	return writeJSON(os.Stdout, v)
}

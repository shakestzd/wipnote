package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestAssetURLConstruction(t *testing.T) {
	cases := []struct {
		goos, goarch     string
		wantOS, wantArch string
		wantErr          bool
	}{
		{"linux", "amd64", "linux", "amd64", false},
		{"linux", "arm64", "linux", "arm64", false},
		{"darwin", "amd64", "darwin", "amd64", false},
		{"darwin", "arm64", "darwin", "arm64", false},
		{"windows", "amd64", "", "", true},
		{"linux", "386", "", "", true},
	}
	for _, tc := range cases {
		gotOS, gotArch, err := mapPlatform(tc.goos, tc.goarch)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s/%s: expected error, got none", tc.goos, tc.goarch)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: unexpected error: %v", tc.goos, tc.goarch, err)
			continue
		}
		if gotOS != tc.wantOS || gotArch != tc.wantArch {
			t.Errorf("%s/%s: got %s/%s, want %s/%s", tc.goos, tc.goarch, gotOS, gotArch, tc.wantOS, tc.wantArch)
		}

		// Verify the full asset URL format.
		ver := "0.54.9"
		archive := archiveName(ver, gotOS, gotArch)
		url := fmt.Sprintf("%s/v%s/%s", downloadBaseURL, ver, archive)
		if !strings.HasPrefix(url, "https://github.com") {
			t.Errorf("unexpected URL prefix: %s", url)
		}
		if !strings.Contains(url, ver) {
			t.Errorf("URL missing version %s: %s", ver, url)
		}
	}
}

// TestArchiveName verifies that the archive naming logic produces the exact format
// that goreleaser's name_template generates: "wipnote_{{.Version}}_{{.Os}}_{{.Arch}}"
func TestArchiveName(t *testing.T) {
	tests := []struct {
		version string
		os      string
		arch    string
		want    string
	}{
		{"0.55.1", "darwin", "arm64", "wipnote_0.55.1_darwin_arm64.tar.gz"},
		{"0.55.1", "darwin", "amd64", "wipnote_0.55.1_darwin_amd64.tar.gz"},
		{"0.55.1", "linux", "amd64", "wipnote_0.55.1_linux_amd64.tar.gz"},
		{"0.55.1", "linux", "arm64", "wipnote_0.55.1_linux_arm64.tar.gz"},
		{"0.54.0", "darwin", "arm64", "wipnote_0.54.0_darwin_arm64.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.version+"_"+tt.os+"_"+tt.arch, func(t *testing.T) {
			got := archiveName(tt.version, tt.os, tt.arch)
			if got != tt.want {
				t.Errorf("archiveName(%q, %q, %q) = %q, want %q", tt.version, tt.os, tt.arch, got, tt.want)
			}
		})
	}
}

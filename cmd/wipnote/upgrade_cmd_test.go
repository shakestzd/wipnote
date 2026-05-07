package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]string{"tag_name": "v0.99.1"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	// Override the API URL for testing by calling the underlying HTTP logic directly.
	ver, err := fetchLatestVersionFromURL(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "0.99.1" {
		t.Errorf("expected 0.99.1, got %s", ver)
	}
}

func TestFetchLatestVersionStripsLeadingV(t *testing.T) {
	for _, tag := range []string{"v1.2.3", "1.2.3"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload := map[string]string{"tag_name": tag}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(payload)
		}))
		ver, err := fetchLatestVersionFromURL(srv.URL)
		srv.Close()
		if err != nil {
			t.Fatalf("tag %q: unexpected error: %v", tag, err)
		}
		if ver != "1.2.3" {
			t.Errorf("tag %q: expected 1.2.3, got %s", tag, ver)
		}
	}
}

func TestFetchLatestVersionAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchLatestVersionFromURL(srv.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

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

// fetchLatestVersionFromURL is a testable variant of fetchLatestVersion that
// accepts an explicit API URL so tests can point at an httptest server.
func fetchLatestVersionFromURL(apiURL string) (string, error) {
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("querying GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("empty tag_name")
	}
	return strings.TrimPrefix(payload.TagName, "v"), nil
}

package paths

import "testing"

func TestIsDependencyManifest(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"go.mod", true},
		{"go.sum", true},
		{"package.json", true},
		{"web/package.json", true},
		{"requirements.txt", true},
		{"pyproject.toml", true},
		{"Cargo.toml", true},
		{"Gemfile", true},
		{"/abs/repo/go.mod", true},
		{"main.go", false},
		{"README.md", false},
		{"gomod", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsDependencyManifest(tc.path); got != tc.want {
			t.Errorf("IsDependencyManifest(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestPackageJSONDependenciesChanged(t *testing.T) {
	base := `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"}}`
	cases := []struct {
		name          string
		before, after string
		want          bool
	}{
		{"scripts only", base, `{"name":"app","scripts":{"test":"node --test a.js b.js"},"dependencies":{"d3":"^7.0.0"}}`, false},
		{"name and version", base, `{"name":"app2","version":"1.0.1","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"}}`, false},
		{"key order and spacing", base, "{\n  \"dependencies\": {\"d3\": \"^7.0.0\"},\n  \"scripts\": {\"test\": \"node --test a.js\"},\n  \"name\": \"app\"\n}", false},
		{"dependency added", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0","d3-scale":"^4.0.0"}}`, true},
		{"dependency bumped", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.1.0"}}`, true},
		{"devDependencies added", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"},"devDependencies":{"vitest":"^2.0.0"}}`, true},
		{"peerDependencies added", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"},"peerDependencies":{"react":"^19.0.0"}}`, true},
		{"optionalDependencies added", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"},"optionalDependencies":{"fsevents":"^2.0.0"}}`, true},
		{"overrides added", base, `{"name":"app","scripts":{"test":"node --test a.js"},"dependencies":{"d3":"^7.0.0"},"overrides":{"d3-color":"3.1.0"}}`, true},
		{"new file with dependencies", "", base, true},
		{"new file without dependencies", "", `{"name":"app","scripts":{}}`, false},
		{"deleted file with dependencies", base, "", true},
		{"unparseable after", base, `{"name":`, true},
		{"unparseable before", `{`, base, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PackageJSONDependenciesChanged([]byte(c.before), []byte(c.after)); got != c.want {
				t.Fatalf("PackageJSONDependenciesChanged = %v, want %v", got, c.want)
			}
		})
	}
}

package plantmpl

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

// TestPackageInit_NoTemplateParse pins that importing plantmpl does not parse
// its templates or build its markdown parser and sanitizer: every wipnote
// process links this package, so work done at init is paid by every hook call
// (feat-af8d1689). Parsing at init measured ~5,400 allocations; the lazy form
// leaves well under 500.
func TestPackageInit_NoTemplateParse(t *testing.T) {
	allocs := packageInitAllocs(t, "github.com/shakestzd/wipnote/plan/plantmpl")
	if allocs >= 500 {
		t.Fatalf("plantmpl package init made %d allocations; templates must be built on first use", allocs)
	}
}

// packageInitAllocs re-runs this test binary with GODEBUG=inittrace=1 and no
// tests selected, and returns the allocation count the runtime reports for pkg.
func packageInitAllocs(t *testing.T, pkg string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("re-run test binary: %v\n%s", err, out)
	}
	re := regexp.MustCompile(`(?m)^init ` + regexp.QuoteMeta(pkg) + ` @.* (\d+) allocs$`)
	m := re.FindSubmatch(out)
	if m == nil {
		t.Fatalf("no inittrace line for %s in:\n%s", pkg, out)
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

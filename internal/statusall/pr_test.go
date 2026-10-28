package statusall

import (
	"context"
	"strings"
	"testing"
)

// ghForBranch answers ghWithPR's reads for PR 42, found by head branch.
func ghForBranch(branch string) *fakeGH {
	f := ghWithPR()
	f.rules[0] = [2]string{"gh pr list --head " + branch + " --state all", `[{"number":42}]`}
	return f
}

// The checked-out branch is always a branch: one named "123" must be looked
// up as a head branch, never read as PR number 123.
func TestFetchPR_NumericBranchIsLookedUpAsBranch(t *testing.T) {
	f := ghForBranch("123")
	pr, err := fetchPR(context.Background(), f, "123")
	if err != nil || pr == nil || pr.Number != 42 {
		t.Fatalf("fetchPR(branch 123) = %+v, %v; want PR 42 via branch lookup", pr, err)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "gh pr view 123") {
			t.Fatalf("branch 123 was read as a PR number: calls %q", f.calls)
		}
	}
}

func TestFetchPR_NamedBranchStillResolves(t *testing.T) {
	f := ghForBranch("work")
	pr, err := fetchPR(context.Background(), f, "work")
	if err != nil || pr == nil || pr.Number != 42 || pr.Title != "Add the thing" {
		t.Fatalf("fetchPR(branch work) = %+v, %v", pr, err)
	}
}

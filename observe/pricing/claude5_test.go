package pricing_test

import (
	"math"
	"testing"

	"github.com/shakestzd/wipnote/observe/pricing"
)

// TestModelsJSON_Claude5Present checks the Claude 5.x models are priced and
// that cache rates follow the published ratios (docs pricing page).
func TestModelsJSON_Claude5Present(t *testing.T) {
	tbl, err := pricing.Default()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := []struct {
		model            string
		in, out, readMul float64
	}{
		{"claude-opus-5-5", 4e-6, 20e-6, 0.05},
		{"claude-sonnet-5-5", 2e-6, 10e-6, 0.05},
		{"claude-fable-5-1", 10e-6, 50e-6, 0.025},
		{"claude-haiku-5-5", 0.1e-6, 0.5e-6, 0.10},
	}
	for _, c := range cases {
		p, ok := tbl.Lookup(c.model)
		if !ok {
			t.Errorf("%s missing from models.json", c.model)
			continue
		}
		if math.Abs(p.InputCostPerToken-c.in) > 1e-12 || math.Abs(p.OutputCostPerToken-c.out) > 1e-12 {
			t.Errorf("%s in/out = %v/%v, want %v/%v", c.model, p.InputCostPerToken, p.OutputCostPerToken, c.in, c.out)
		}
		if math.Abs(p.CacheReadInputTokenCost-c.in*c.readMul) > 1e-12 {
			t.Errorf("%s cache read = %v, want %v", c.model, p.CacheReadInputTokenCost, c.in*c.readMul)
		}
		if math.Abs(p.CacheCreationInputTokenCost-c.in*1.25) > 1e-12 {
			t.Errorf("%s cache write = %v, want %v", c.model, p.CacheCreationInputTokenCost, c.in*1.25)
		}
	}
	if _, ok := tbl.Lookup("claude-opus-4-7"); !ok {
		t.Error("old model claude-opus-4-7 must stay for history lookups")
	}
}

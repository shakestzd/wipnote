package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestHarnessFlags(t *testing.T) {
	cases := []struct {
		name      string
		o         SpawnOptions
		want      []string
		wantModel string
	}{
		{"nothing chosen", SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull}, nil, ""},
		{"a resume keeps its inherited flags", SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull, Model: "haiku", Flags: []string{"--model", "haiku"}},
			[]string{"--model", "haiku"}, "haiku"},
		{"claude model and effort", SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull, Model: "haiku", Effort: "low"},
			[]string{"--model", "haiku", "--effort", "low"}, "haiku"},
		{"model alone", SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull, Model: "haiku"}, []string{"--model", "haiku"}, "haiku"},
		{"codex effort is a config key", SpawnOptions{Harness: HarnessCodex, Model: "gpt-x", Effort: "low"},
			[]string{"--model", "gpt-x", "-c", `model_reasoning_effort="low"`}, "gpt-x"},
		{"extra flags keep their order after the model", SpawnOptions{Harness: HarnessClaude, Profile: ProfileFull, Model: "haiku", Extra: []string{"--mcp-config", "m.json", "--strict-mcp-config"}},
			[]string{"--model", "haiku", "--mcp-config", "m.json", "--strict-mcp-config"}, "haiku"},
		{"a model after -- is recorded", SpawnOptions{Harness: HarnessCodex, Extra: []string{"-m", "gpt-x"}},
			[]string{"-m", "gpt-x"}, "gpt-x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.o
			if err := harnessFlags(context.Background(), &o); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(o.Flags, tc.want) || o.Model != tc.wantModel {
				t.Fatalf("flags %q model %q, want %q %q", o.Flags, o.Model, tc.want, tc.wantModel)
			}
		})
	}
}

func TestHarnessFlags_RefusesWhatBreaksTheWorker(t *testing.T) {
	cases := []SpawnOptions{
		{Harness: HarnessClaude, Extra: []string{"-p"}},
		{Harness: HarnessClaude, Extra: []string{"--print"}},
		{Harness: HarnessClaude, Extra: []string{"--resume", "abc"}},
		{Harness: HarnessClaude, Extra: []string{"--session-id=abc"}},
		{Harness: HarnessClaude, Extra: []string{"--name", "x"}},
		{Harness: HarnessCodex, Extra: []string{"exec"}},
		{Harness: HarnessClaude, Model: "haiku", Extra: []string{"--model", "opus"}},
	}
	for _, o := range cases {
		if err := harnessFlags(context.Background(), &o); !errors.Is(err, ErrBadFlag) {
			t.Errorf("%+v: err = %v, want ErrBadFlag", o, err)
		}
	}
}

// -c is Codex's config flag but Claude's --continue: the refusal is per harness.
func TestHarnessFlags_ReservedIsPerHarness(t *testing.T) {
	o := SpawnOptions{Harness: HarnessCodex, Extra: []string{"-c", "x=1"}}
	if err := harnessFlags(context.Background(), &o); err != nil {
		t.Fatalf("codex -c refused: %v", err)
	}
	o = SpawnOptions{Harness: HarnessClaude, Extra: []string{"-c"}}
	if err := harnessFlags(context.Background(), &o); !errors.Is(err, ErrBadFlag) {
		t.Fatalf("claude -c: err = %v", err)
	}
}

func TestBootArgv_FlagsFollowTheBrief(t *testing.T) {
	info := Info{Name: "w1", Harness: HarnessClaude, Flags: []string{"--model", "haiku"}}
	got := BootArgv("wipnote", info, "the brief")
	want := []string{"wipnote", "claude", "--in-place", "--name", "w1", "--", "the brief", "--model", "haiku"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BootArgv = %q\nwant %q", got, want)
	}
	// Without a brief the flags still need the -- that sends them to the harness.
	got = BootArgv("wipnote", info, "")
	want = []string{"wipnote", "claude", "--in-place", "--name", "w1", "--", "--model", "haiku"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BootArgv = %q\nwant %q", got, want)
	}
}

func TestContainerArgv_OneDashAndFlagsAfterTheBrief(t *testing.T) {
	info := Info{Name: "w1", Harness: HarnessClaude, Flags: []string{"--model", "haiku"}}
	got := containerArgv(info, "the brief")
	want := []string{"wipnote", "claude", "--in-place", "--name", "w1", "--", "the brief", "--model", "haiku"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerArgv = %q\nwant %q", got, want)
	}
}

func TestFlagValue(t *testing.T) {
	cases := map[string][]string{
		"haiku": {"-p", "x", "--model", "haiku"},
		"opus":  {"--model=opus"},
		"gpt":   {"exec", "-m", "gpt"},
		"":      {"-p", "--model"},
	}
	for want, args := range cases {
		if got := FlagValue(args); got != want {
			t.Errorf("FlagValue(%q) = %q, want %q", args, got, want)
		}
	}
}

func TestSpawn_RecordsModelAndFlags(t *testing.T) {
	e := newEnv(t)
	s := e.workerWith(t, "w1", func(o *SpawnOptions) { o.Model, o.Extra = "haiku", []string{"--strict-mcp-config"} })
	info, err := ReadInfo(e.runFolder(s.Run))
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "haiku" || !reflect.DeepEqual(info.Flags, []string{"--model", "haiku", "--strict-mcp-config"}) {
		t.Fatalf("info model %q flags %q", info.Model, info.Flags)
	}
	r, ok, err := e.deps.Ledger.Get(s.Run)
	if err != nil || !ok || r.Model != "haiku" {
		t.Fatalf("ledger row %+v ok=%v err=%v", r, ok, err)
	}
	if s.Model != "haiku" {
		t.Fatalf("spawned = %+v", s)
	}
}

func TestSpawn_RefusesABadFlagBeforeOpeningARun(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	o.Extra = []string{"-p"}
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadFlag) {
		t.Fatalf("err = %v", err)
	}
	if recs, _ := e.deps.Ledger.ReadAll(); len(recs) != 0 {
		t.Fatalf("a refused spawn opened a run: %+v", recs)
	}
}

func (e *env) workerWith(t *testing.T, name string, set func(*SpawnOptions)) Spawned {
	t.Helper()
	o := e.opts(name)
	set(&o)
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

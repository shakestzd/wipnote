package session

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBadFlag refuses a harness flag that would break the worker's lifecycle.
var ErrBadFlag = errors.New("delegate: harness flag not allowed")

// reservedFlags are the flags wipnote sets itself or that end the interactive
// session a worker depends on: a print run has no input line, and another
// session id would break the claim and the resume record.
var reservedFlags = map[string]map[string]string{
	HarnessClaude: {
		"-p": "print", "--print": "print", "-r": "resume", "--resume": "resume", "-c": "continue", "--continue": "continue",
		"--session-id": "session id", "-n": "name", "--name": "name", "--fork-session": "resume",
	},
	HarnessCodex: {"exec": "print", "e": "print", "review": "print", "resume": "resume", "--last": "resume"},
}

// harnessFlags turns a spawn's choices into the flags boot passes. A resume
// that chooses nothing keeps the flags resumeOptions copied from the old run,
// so a worker on a cheap model does not come back on the default one.
func harnessFlags(o *SpawnOptions) error {
	if o.Effort == "" && len(o.Extra) == 0 && (o.Model == "" || len(o.Flags) > 0) {
		return nil
	}
	for _, f := range o.Extra {
		name, _, _ := strings.Cut(f, "=")
		if why, ok := reservedFlags[o.Harness][name]; ok {
			return fmt.Errorf("%w: %s (%s): wipnote runs this worker interactively and tracks its session; use delegate run or delegate spawn --resume", ErrBadFlag, name, why)
		}
	}
	inExtra := FlagValue(o.Extra)
	if o.Model != "" && inExtra != "" {
		return fmt.Errorf("%w: the model is given twice: --model %s and %s after --", ErrBadFlag, o.Model, inExtra)
	}
	var flags []string
	if o.Model != "" {
		flags = append(flags, "--model", o.Model)
	}
	if o.Effort != "" {
		switch o.Harness {
		case HarnessClaude:
			flags = append(flags, "--effort", o.Effort)
		case HarnessCodex:
			flags = append(flags, "-c", fmt.Sprintf("model_reasoning_effort=%q", o.Effort))
		}
	}
	o.Flags = append(flags, o.Extra...)
	if o.Model == "" {
		o.Model = inExtra
	}
	return nil
}

// FlagValue returns the model a harness argv asks for, or "": --model, Codex's
// -m, or either with =.
func FlagValue(args []string) string {
	for i, a := range args {
		switch {
		case (a == "--model" || a == "-m") && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--model="):
			return strings.TrimPrefix(a, "--model=")
		case strings.HasPrefix(a, "-m="):
			return strings.TrimPrefix(a, "-m=")
		}
	}
	return ""
}

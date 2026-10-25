package main

import (
	"context"
	"io"

	"github.com/shakestzd/wipnote/internal/gate"
)

func gateExecEnv(codeRoot string) (env []string, redirected bool, tmpDir string) {
	return gate.GateExecEnv(codeRoot)
}

func isLikelyNoexecFailure(output string) bool {
	return gate.IsLikelyNoexecFailure(output)
}

func gateTmpRemediation(codeRoot string) string {
	return gate.GateTmpRemediation(codeRoot)
}

func runManagedGate(ctx context.Context, name, dir string, env []string, stdout, stderr io.Writer, argv ...string) (string, error) {
	return gate.RunManagedGate(ctx, name, dir, env, stdout, stderr, argv...)
}

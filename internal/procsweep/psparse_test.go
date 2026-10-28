package procsweep

import (
	"errors"
	"testing"
	"time"
)

// Fixtures mirror `ps -o pid=,ppid=,uid=,lstart=,command=` and
// `ps -E -o pid=,command=` output: the -E form appends the environment to the
// command column.
const psInfoOut = `  501     1   501 Tue Oct 27 09:05:00 2026     npm run dev
  502   501   501 Tue Oct 27 09:06:10 2026     node server.js --port 3000
  777     1     0 Tue Oct 27 08:00:00 2026     /usr/sbin/rootd
`

const psEnvOut = `  501 npm run dev PATH=/usr/bin CLAUDE_CODE_SESSION_ID=` + liveSession + ` SECRET=x
  502 node server.js --port 3000 HOME=/h
  900 ps -E PATH=/usr/bin
`

func TestParsePS_MergesEnvAndFiltersUser(t *testing.T) {
	procs, err := parsePS(psInfoOut, psEnvOut, 501, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 2 {
		t.Fatalf("want 2 procs of uid 501, got %+v", procs)
	}
	p := procs[0]
	if p.PID != 501 || p.PPID != 1 || p.Command != "npm run dev" {
		t.Fatalf("bad proc: %+v", p)
	}
	if p.Env["CLAUDE_CODE_SESSION_ID"] != liveSession || p.Env["SECRET"] != "" {
		t.Fatalf("env parse: %v", p.Env)
	}
	want := time.Date(2026, 10, 27, 9, 5, 0, 0, time.Local)
	if !p.StartedAt.Equal(want) {
		t.Fatalf("start = %v, want %v", p.StartedAt, want)
	}
	if procs[1].Env["CLAUDE_CODE_SESSION_ID"] != "" {
		t.Fatalf("pid 502 has no session id: %v", procs[1].Env)
	}
}

// When ps -E shows no environment for our own process, the platform is not
// letting us read environments; say so instead of reporting "no processes".
func TestParsePS_NoEnvForSelfIsPlatformError(t *testing.T) {
	envOut := "  501 npm run dev\n  900 ps -E\n"
	_, err := parsePS(psInfoOut, envOut, 501, 900)
	if !errors.Is(err, ErrEnvUnreadable) {
		t.Fatalf("want ErrEnvUnreadable, got %v", err)
	}
}

package procsweep

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// lstartLayout is ps's lstart column ("%c" in the C locale) after whitespace
// is collapsed, e.g. "Tue Oct 27 09:05:00 2026".
const lstartLayout = "Mon Jan 2 15:04:05 2006"

// envToken matches a KEY=VALUE token that ps -E appended to a command.
var envToken = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// parsePS joins two ps listings into RawProcs for one uid:
//
//	info: ps -o pid=,ppid=,uid=,lstart=,command=
//	env:  ps -E -o pid=,command=   (environment appended to the command)
//
// The environment is what env's command column holds beyond info's command.
// When our own row (self) shows no environment at all, the platform is
// hiding environments, and that is reported as ErrEnvUnreadable.
func parsePS(info, env string, uid, self int) ([]RawProc, error) {
	envByPID := map[int][]string{}
	for _, line := range strings.Split(env, "\n") {
		f := strings.Fields(line)
		if pid, err := strconv.Atoi(firstOr(f)); err == nil {
			envByPID[pid] = f[1:]
		}
	}
	if !hasEnvToken(envByPID[self]) {
		return nil, ErrEnvUnreadable
	}
	var out []RawProc
	for _, line := range strings.Split(info, "\n") {
		rp, puid, ok := parseInfoLine(line)
		if !ok || puid != uid {
			continue
		}
		rp.Env = keepSessionEnv(envRemainder(rp.Command, envByPID[rp.PID]))
		out = append(out, rp)
	}
	return out, nil
}

// parseInfoLine parses "pid ppid uid <5 lstart tokens> command...".
func parseInfoLine(line string) (RawProc, int, bool) {
	f := strings.Fields(line)
	if len(f) < 9 {
		return RawProc{}, 0, false
	}
	pid, e1 := strconv.Atoi(f[0])
	ppid, e2 := strconv.Atoi(f[1])
	uid, e3 := strconv.Atoi(f[2])
	start, e4 := time.ParseInLocation(lstartLayout, strings.Join(f[3:8], " "), time.Local)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return RawProc{}, 0, false
	}
	return RawProc{PID: pid, PPID: ppid, Command: strings.Join(f[8:], " "), StartedAt: start}, uid, true
}

// envRemainder strips the command's own tokens off the -E row. A row whose
// prefix does not match (the process exec'd between the two ps runs) yields
// no environment rather than a guess.
func envRemainder(cmd string, row []string) []string {
	n := len(strings.Fields(cmd))
	if len(row) < n || strings.Join(row[:n], " ") != cmd {
		return nil
	}
	return row[n:]
}

func hasEnvToken(tokens []string) bool {
	for _, t := range tokens {
		if envToken.MatchString(t) {
			return true
		}
	}
	return false
}

func firstOr(f []string) string {
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// psError wraps a failed ps run.
func psError(args []string, err error) error {
	return fmt.Errorf("procsweep: ps %s: %w", strings.Join(args, " "), err)
}

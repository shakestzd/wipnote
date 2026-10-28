# shellcheck shell=bash
# Results output for scripts/delegate-phase-b.sh: the markdown table (stdout
# and --out), the redacted raw log, suggested arch cards, and the exit code.
# Sourced, never run.

# arch_paths ID: the repo paths a check's arch card covers (unique per card).
arch_paths() {
  case "$1" in
    H) echo "internal/delegate/herdr/commands.go internal/delegate/herdr/agent.go" ;;
    B1) echo "cmd/wipnote/claude.go" ;;
    B2) echo "internal/delegate/importorch/import.go" ;;
    B3) echo "plugin/hooks/hooks.json" ;;
    B4) echo "port/packages/codex-marketplace/**" ;;
    B5) echo "cmd/wipnote/codex_exec.go" ;;
    B6) echo ".devcontainer/devcontainer.json" ;;
    B7) echo "core/filelock/**" ;;
    B8) echo "core/claimledger/**" ;;
    B9) echo "internal/delegate/**" ;;
    D8) echo "internal/delegate/herdr/cli.go" ;;
    C) echo "docs/design/delegate-interactive-parity.md" ;;
  esac
}

# fill_missing: a selected check that recorded nothing is a script bug: FAIL.
fill_missing() {
  local id
  for id in $ALL_IDS; do
    want "$id" || continue
    [ -z "$(result_of "$id")" ] && fail "$id" "-" "the check recorded no result (script bug)"
  done
}

table() {
  local i=0
  echo "| Check | Result | Command | Observed |"
  echo "|---|---|---|---|"
  while [ "$i" -lt "${#R_ID[@]}" ]; do
    # shellcheck disable=SC2016 # literal backticks: a markdown code span
    printf '| %s %s | %s | `%s` | %s |\n' "${R_ID[$i]}" "$(cell "$(title_of "${R_ID[$i]}")" 100)" \
      "${R_RESULT[$i]}" "$(cell "${R_CMD[$i]}" 240 | sed 's/`/'"'"'/g')" "$(cell "${R_OBS[$i]}" 400)"
    i=$((i + 1))
  done
}

# arch_cards: one suggested `wipnote arch add` per check group that ran, to be
# run after the results are reviewed (the script never writes the real
# .wipnote/).
arch_cards() {
  local g ids id res body kind n=0
  echo '```bash'
  for g in H B1 B2 B3 B4 B5 B6 B7 B8 B9 D8 C; do
    ids="" res="" kind=invariant
    for id in $ALL_IDS; do
      case "$id" in "$g"|"$g"[0-9a-z]) ;; *) continue ;; esac
      case "$(result_of "$id")" in PASS|FAIL) ;; *) continue ;; esac
      ids="$ids $id" res="$res $id=$(result_of "$id")"
      [ "$(result_of "$id")" = FAIL ] && kind=hazard
    done
    [ -z "$ids" ] && continue
    body="Phase B ($(date +%Y-%m-%d)):$res. See docs/design/delegate-phase-b.md."
    printf 'wipnote arch add phase-b-%s --kind %s --created-by phase-b --links feat-0c976595' \
      "$(echo "$g" | tr '[:upper:]' '[:lower:]')" "$kind"
    for id in $(arch_paths "$g"); do printf " --paths '%s'" "$id"; done
    printf " --body '%s'\n" "$body"
    n=$((n + 1))
  done
  [ "$n" = 0 ] && echo "# none: no check ran to PASS or FAIL"
  echo '```'
}

write_report() {
  fill_missing
  local log_out
  log_out="$(dirname "$OUT")/phase-b-raw.log"
  mkdir -p "$(dirname "$OUT")" || return 1
  {
    echo "# Phase B results"
    echo
    echo "- Date: $(date -u +%Y-%m-%dT%H:%MZ)"
    echo "- Host: $(uname -sr)"
    echo "- wipnote: $V_WIPNOTE; claude: $V_CLAUDE; codex: $V_CODEX; herdr: $V_HERDR"
    echo "- Flags:${ONLY:+ --only $ONLY}$([ "$NO_MODEL" = 1 ] && echo ' --no-model')$([ "$NO_INTERACTIVE" = 1 ] && echo ' --no-interactive')$([ "$SELF_TEST" = 1 ] && echo ' --self-test')"
    echo "- Totals: $(count_result PASS) pass, $(count_result FAIL) fail, $(count_result 'NOT RUN') not run"
    echo
    table
    echo
    echo "## Suggested arch cards"
    echo
    arch_cards
  } | redact >"$OUT.tmp" || { echo "cannot write $OUT" >&2; return 1; }
  mv "$OUT.tmp" "$OUT" || return 1
  redact <"$RAW_LOG" >"$log_out" || { echo "cannot write $log_out" >&2; return 1; }
  echo
  cat "$OUT"
  echo
  echo "results: $OUT; raw log: $log_out"
  [ "$(count_result FAIL)" = 0 ]
}

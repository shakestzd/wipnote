# shellcheck shell=bash
# Shared helpers for scripts/delegate-phase-b.sh: result records, command
# capture with timeouts, redaction, JSON reads, scratch projects and owner
# prompts. Written for macOS /bin/bash 3.2 and Linux bash: no associative
# arrays, no mapfile, no ${var,,}. Sourced, never run.

# --- result records (parallel indexed arrays; bash 3.2 has no maps) ---------
R_ID=()
R_RESULT=()
R_CMD=()
R_OBS=()

# record ID RESULT CMD OBS: store one row and echo it to the terminal.
record() {
  R_ID+=("$1")
  R_RESULT+=("$2")
  R_CMD+=("$3")
  R_OBS+=("$4")
  log "== $1: $2"
  printf '%-5s %-8s %s\n' "$1" "$2" "$(oneline "$4" 100)"
}

pass() { record "$1" PASS "$2" "$3"; }
fail() { record "$1" FAIL "$2" "$3"; }
skip() { record "$1" "NOT RUN" "-" "$2"; }

# result_of ID: print the recorded result of ID, or nothing.
result_of() {
  local i=0
  while [ "$i" -lt "${#R_ID[@]}" ]; do
    if [ "${R_ID[$i]}" = "$1" ]; then
      printf '%s' "${R_RESULT[$i]}"
      return 0
    fi
    i=$((i + 1))
  done
}

# count_result RESULT: how many rows carry RESULT.
count_result() {
  local i=0 n=0
  while [ "$i" -lt "${#R_RESULT[@]}" ]; do
    [ "${R_RESULT[$i]}" = "$1" ] && n=$((n + 1))
    i=$((i + 1))
  done
  echo "$n"
}

# --- logging and text shaping ---------------------------------------------
log() { printf '%s\n' "$*" >>"$RAW_LOG"; }

# strip_ansi: remove terminal escape sequences from stdin.
strip_ansi() { perl -pe 's/\e\[[0-9;?]*[ -\/]*[@-~]//g; s/\e\][^\a\e]*(\a|\e\\)//g; s/\r//g'; }

# oneline TEXT MAX: collapse whitespace and cut to MAX characters.
oneline() {
  local max="${2:-300}"
  printf '%s' "$1" | strip_ansi | tr '\n\t' '  ' | tr -s ' ' | cut -c "1-$max"
}

# redact: replace scratch, repo, home, any /Users/<name> or /home/<name>,
# any macOS per-user temp path (/var/folders/..., which a cut line can leave
# only partly matching the scratch path), the user name (when it is not a
# generic account) and the host name.
redact() {
  P_SCRATCH="${SCRATCH:-}" P_CSCRATCH="${CSCRATCH:-}" P_REPO="${REPO:-}" \
    P_HOME="${HOME:-}" P_USER="${USER_NAME:-}" P_HOST="${HOST_NAME:-}" perl -pe '
    BEGIN { @r = (["P_CSCRATCH","<ctr-scratch>"],["P_SCRATCH","<scratch>"],
                  ["P_REPO","<repo>"],["P_HOME","~"]); }
    for my $p (@r) { my $v = $ENV{$p->[0]}; s/\Q$v\E/$p->[1]/g if length($v) > 1; }
    s{(/Users|/home)/[^/\s"'"'"'<>]+}{$1/<user>}g;
    s{(?:/private)?/var/folders/[^\s"'"'"'<>]*}{<tmp>}g;
    my $u = $ENV{P_USER};
    s/(?<![\w])\Q$u\E(?![\w])/<user>/g if length($u) > 2 && $u !~ /^(root|user|admin|vscode|runner|ubuntu)$/;
    my $h = $ENV{P_HOST}; s/(?<![\w.-])\Q$h\E(?![\w-])/<host>/g if length($h) > 2;'
}

# cell TEXT MAX: one markdown table cell (no pipes, no newlines).
cell() { oneline "$1" "${2:-300}" | sed 's/|/\\|/g'; }

# --- command capture --------------------------------------------------------
# quote_cmd ARGS...: the command as a shell line, without the env -u prefix
# that clears harness variables (it is noise in the table).
quote_cmd() {
  if [ "${1:-}" = env ]; then
    shift
    while [ "${1:-}" = -u ]; do shift 2; done
  fi
  local out="" a
  for a in "$@"; do
    case "$a" in
      *[!A-Za-z0-9_./:=,@+-]*|'') out="$out '$(printf '%s' "$a" | sed "s/'/'\\\\''/g")'" ;;
      *) out="$out $a" ;;
    esac
  done
  printf '%s' "${out# }"
}

# with_timeout SECS CMD...: run CMD, killed by SIGALRM after SECS (exit 142).
# macOS has no timeout(1); perl's alarm survives exec.
with_timeout() {
  local secs="$1"
  shift
  perl -e 'alarm shift @ARGV; exec @ARGV or die "exec $ARGV[0]: $!\n"' "$secs" "$@"
}

# capd DIR SECS CMD...: run CMD in DIR under a timeout. Sets CAP_CMD (the
# command line), CAP_OUT (stdout+stderr) and CAP_RC. Never fails the script.
capd() {
  local dir="$1" secs="$2"
  shift 2
  CAP_CMD=$(quote_cmd "$@")
  log "\$ (cd $dir) $CAP_CMD"
  CAP_OUT=$(cd "$dir" && with_timeout "$secs" "$@" 2>&1 </dev/null)
  CAP_RC=$?
  log "$CAP_OUT"
  log "[exit $CAP_RC]"
}

# cap SECS CMD...: capd in the current directory.
cap() { capd "$PWD" "$@"; }

# HARNESS_ENV clears variables that would make wipnote adopt this caller's
# session (a Claude or Codex parent) or another project.
HARNESS_ENV=(env -u WIPNOTE_PROJECT_DIR -u CLAUDE_PROJECT_DIR -u CLAUDECODE
  -u CLAUDE_CODE_ENTRYPOINT -u CLAUDE_CODE_SESSION_ID -u CLAUDE_SESSION_ID
  -u CODEX_THREAD_ID -u CODEX_SESSION_ID -u WIPNOTE_HARNESS -u WIPNOTE_SESSION_ID)
# shellcheck disable=SC2034 # used by herdr.sh and checks-container.sh
UNSET_LINE='unset WIPNOTE_PROJECT_DIR CLAUDE_PROJECT_DIR CLAUDECODE CLAUDE_CODE_ENTRYPOINT CLAUDE_CODE_SESSION_ID CLAUDE_SESSION_ID CODEX_THREAD_ID CODEX_SESSION_ID WIPNOTE_HARNESS WIPNOTE_SESSION_ID'

# --- small utilities --------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

new_uuid() {
  if have uuidgen; then uuidgen | tr '[:upper:]' '[:lower:]'
  elif [ -r /proc/sys/kernel/random/uuid ]; then cat /proc/sys/kernel/random/uuid
  else perl -e 'printf "%08x-%04x-4%03x-a%03x-%012x\n", rand(2**32), rand(2**16), rand(2**12), rand(2**12), rand(2**48)'
  fi
}

# jget PATH: read JSON on stdin, print the value at a dotted path
# (array indexes are numbers, e.g. result.layout.panes.0.pane_id).
jget() {
  perl -MJSON::PP -e '
    my $d = eval { decode_json(join "", <STDIN>) }; exit 1 unless defined $d;
    for my $k (split /\./, $ARGV[0]) {
      $d = ref $d eq "ARRAY" ? $d->[$k] : ref $d eq "HASH" ? $d->{$k} : undef;
      exit 1 unless defined $d;
    }
    print ref $d ? JSON::PP->new->canonical->encode($d) : $d;' "$1" 2>/dev/null
}

# wait_until SECS CMD...: poll CMD once a second until it succeeds.
wait_until() {
  local secs="$1" i=0
  shift
  while [ "$i" -lt "$secs" ]; do
    "$@" && return 0
    sleep 1
    i=$((i + 1))
  done
  return 1
}

# ask QUESTION: y/n from the owner on the terminal. Returns 0 for yes, 1 for
# no, 2 when there is no terminal to ask on.
ask() {
  local reply
  [ -r /dev/tty ] || return 2
  printf '\n>>> %s [y/n] ' "$1" >/dev/tty
  read -r reply </dev/tty || return 2
  log "asked: $1 -> $reply"
  case "$reply" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# --- scratch projects -------------------------------------------------------
# new_project DIR: a throwaway git repo with an initialised .wipnote/.
new_project() {
  mkdir -p "$1" || return 1
  (cd "$1" && git init -q && git -c user.name=phase-b -c user.email=phase-b@example.invalid \
    commit -q --allow-empty -m "phase-b scratch" && "${HARNESS_ENV[@]}" wipnote init >/dev/null 2>&1)
}

# ledger_sids DIR HARNESS: session ids of HARNESS rows in DIR's sessions ledger.
ledger_sids() {
  local f="$1/.wipnote/sessions-ledger.html"
  [ -f "$f" ] || return 0
  perl -ne 'BEGIN { $h = shift @ARGV }
    while (/data-session-id="([^"]+)" data-harness="([^"]*)"/g) { print "$1\n" if $2 eq $h || $h eq "" }' \
    "$2" "$f"
}

# ledger_harness DIR SID: the data-harness of SID's row in DIR's ledger.
ledger_harness() {
  local f="$1/.wipnote/sessions-ledger.html"
  [ -f "$f" ] || return 0
  perl -ne 'BEGIN { $s = shift @ARGV }
    print "$1\n" if /data-session-id="\Q$s\E" data-harness="([^"]*)"/' "$2" "$f" | head -1
}

# new_sids BEFORE AFTER: ids in AFTER (newline list) that are not in BEFORE.
new_sids() {
  local s
  for s in $2; do
    case " $(echo "$1" | tr '\n' ' ') " in *" $s "*) ;; *) echo "$s" ;; esac
  done
}

# --- cleanup ----------------------------------------------------------------
CLEAN_PIDS=""
CLEAN_DIRS=""
CLEAN_HOOKS=""

on_exit_cleanup() {
  local p d h
  for h in $CLEAN_HOOKS; do "$h" >/dev/null 2>&1; done
  for p in $CLEAN_PIDS; do kill "$p" >/dev/null 2>&1; done
  for d in $CLEAN_DIRS; do [ -n "$d" ] && [ -d "$d" ] && rm -rf "$d"; done
}

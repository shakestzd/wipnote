#!/usr/bin/env bash
# Before-and-after check for guidance changes (feat-217fb5f2). It builds wipnote
# for two git refs, runs each fixed task N times per ref with `wipnote delegate
# run`, and writes a comparison file. See docs/design/prompt-ab.md.
# Runs on macOS bash 3.2 and Linux bash.
set -u

usage() {
  cat <<'USAGE'
Usage: scripts/prompt-ab.sh --old REF (--new REF | --plant-no-tdd) [flags]

  --old REF         the reference guidance (a git ref)
  --new REF         the changed guidance (a git ref)
  --plant-no-tdd    use --old with the test-first rule deleted as the new side
  --runs N          runs per task per ref (default 3)
  --only TASK       run one task folder only
  --sides LIST      "old,new" (default), or one side to prove the path
  --out DIR         work and result folder (default: a new temp folder)
  --model NAME      claude model (default sonnet)
  --dry-run         print the plan and run nothing
Exit: 0 no regression flagged, 1 a regression flagged, 2 bad usage, a failed run
or a wrong result count. A failed run exits 2 even when the comparison is clean.
USAGE
}

OLD="" NEW="" PLANT=0 RUNS=3 ONLY="" SIDES="old,new" OUT="" MODEL=sonnet DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --old) OLD="${2:-}"; shift 2 ;;
    --new) NEW="${2:-}"; shift 2 ;;
    --plant-no-tdd) PLANT=1; shift ;;
    --runs) RUNS="${2:-}"; shift 2 ;;
    --only) ONLY="${2:-}"; shift 2 ;;
    --sides) SIDES="${2:-}"; shift 2 ;;
    --out) OUT="${2:-}"; shift 2 ;;
    --model) MODEL="${2:-}"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[ -n "$OLD" ] || { echo "--old is required" >&2; exit 2; }
if [ "$PLANT" = 1 ]; then NEW="${NEW:-$OLD+no-tdd}"; fi
[ -n "$NEW" ] || { echo "--new or --plant-no-tdd is required" >&2; exit 2; }

ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)" || exit 2
EVALS="$ROOT/plugin/evals/prompt-ab"
PY="$EVALS/prompt_ab.py"
[ -n "$OUT" ] || OUT="$(mktemp -d "${TMPDIR:-/tmp}/prompt-ab.XXXXXX")"

# resolve_path prints an absolute path with symlinks resolved, for a path that
# may not exist yet. It resolves the nearest existing parent and appends the rest.
resolve_path() {
  local p="$1" rest=""
  case "$p" in /*) ;; *) p="$PWD/$p" ;; esac
  while [ ! -d "$p" ]; do rest="/$(basename "$p")$rest"; p="$(dirname "$p")"; done
  echo "$(cd -P "$p" && pwd -P)$rest"
}

# A result folder inside the source checkout would put worktrees and build
# output into the caller's own tree, so the script refuses it (bug-fcaea540).
OUT="$(resolve_path "$OUT")"
ROOT_REAL="$(cd -P "$ROOT" && pwd -P)"
case "$OUT/" in
  "$ROOT_REAL"/*) echo "--out $OUT is inside the source checkout $ROOT_REAL; choose a folder outside it" >&2; exit 2 ;;
esac
[ "$DRY" = 1 ] || mkdir -p "$OUT" || exit 2

# Remove the two build worktrees on any exit. Earlier runs left them behind and
# they piled up in `git worktree list`. Results and the comparison file stay.
# A worktree that will not go is named on stderr, and prune clears stale entries.
cleanup_worktrees() {
  local wt err
  for wt in "$OUT/wt-old" "$OUT/wt-new"; do
    [ -d "$wt" ] || continue
    err="$(git -C "$ROOT" worktree remove --force "$wt" 2>&1)" ||
      echo "warning: could not remove worktree $wt: $err" >&2
  done
  [ "$DRY" = 1 ] || git -C "$ROOT" worktree prune >/dev/null 2>&1
  return 0
}
trap cleanup_worktrees EXIT

tasks() {
  local d
  for d in "$EVALS"/tasks/*/; do
    d="$(basename "$d")"
    if [ -z "$ONLY" ] || [ "$ONLY" = "$d" ]; then echo "$d"; fi
  done
}

ref_of() { if [ "$1" = old ]; then echo "$OLD"; else echo "$NEW"; fi; }

print_plan() {
  local side t
  for side in old new; do
    echo "worktree $side: $(ref_of "$side") -> $OUT/wt-$side"
    echo "build: $OUT/wt-$side -> $OUT/bin-$side/wipnote"
  done
  echo "runs per task per ref: $RUNS"
  for t in $(tasks); do echo "task: $t"; done
  [ "$PLANT" = 1 ] && echo "planted regression: test-first rule removed from the new side"
  echo "comparison file: $OUT/comparison.md"
}

# plant_no_tdd deletes the test-first rule and its checklist line from the
# worker-rules block in worktree $1.
plant_no_tdd() {
  local f="$1/cmd/wipnote/prompts/worker-rules.md"
  grep -v -e 'Write failing tests first' -e 'Failing tests written' -e 'Write one failing test' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  if grep -q -e 'Write failing tests first' -e 'Write one failing test' "$f"; then echo "plant failed" >&2; return 1; fi
}

setup_side() { # side
  local side="$1" ref wt="$OUT/wt-$1"
  ref="$(ref_of "$side")"
  if [ "$side" = new ] && [ "$PLANT" = 1 ]; then ref="$OLD"; fi
  git -C "$ROOT" worktree add --detach "$wt" "$ref" >/dev/null 2>&1 || { echo "worktree failed for $ref" >&2; return 1; }
  if [ "$side" = new ] && [ "$PLANT" = 1 ]; then plant_no_tdd "$wt" || return 1; fi
  mkdir -p "$OUT/bin-$side"
  (cd "$wt" && go build -o "$OUT/bin-$side/wipnote" ./cmd/wipnote) || { echo "build failed for $side" >&2; return 1; }
  local rules="$wt/cmd/wipnote/prompts/worker-rules.md"
  [ -f "$rules" ] || { echo "no worker-rules.md in $ref" >&2; return 1; }
  cat "$wt/cmd/wipnote/prompts/system-prompt.md" "$rules" > "$OUT/guidance-$side.md"
}

# record_of prints the raw stream log that `wipnote delegate run` keeps in the
# run folder. It holds every tool call with its input, which is what the
# behaviours are read from. The run ledger events file records tool names only.
record_of() { # project dir
  ls "$1"/.wipnote/delegate/runs/*/raw.log 2>/dev/null | head -1
}

# cenv runs a command with a clean environment. Without it the worker inherits
# WIPNOTE_PROJECT_DIR and CLAUDE_PROJECT_DIR from the caller and writes work
# items and runs into the caller's real project instead of the fixture.
cenv() {
  env -i HOME="$HOME" PATH="$BIN:$PATH" TERM="${TERM:-dumb}" USER="${USER:-}" TMPDIR="${TMPDIR:-/tmp}" \
    LANG="${LANG:-C}" ${ANTHROPIC_API_KEY:+ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"} "$@"
}

run_one() { # side task n
  local side="$1" task="$2" n="$3" proj="$OUT/proj/$1/$2/$3" BIN="$OUT/bin-$1" res="$OUT/results/$1/$2" item tr
  mkdir -p "$proj" "$res"
  cp -R "$EVALS/tasks/$task/fixture/." "$proj/"
  # Fixture sources end in .fixture so no Go tool in this repo scans them.
  for f in "$proj"/*.go.fixture; do mv "$f" "${f%.fixture}"; done
  (cd "$proj" && git init -q && git add -A && git -c user.name=ab -c user.email=ab@example.invalid commit -qm init) || return 1
  item="$(cd "$proj" && cenv wipnote init >/dev/null 2>&1; cenv wipnote feature create "ab $task" --standalone "prompt-ab fixture" --done-when "task done" 2>&1 | grep -o 'feat-[0-9a-f]*' | head -1)"
  [ -n "$item" ] || { echo "no work item for $side/$task/$n" >&2; return 1; }
  (cd "$proj" && cenv wipnote feature start "$item" >/dev/null 2>&1 && cenv wipnote delegate run --no-pane --work-item "$item" -- \
    claude -p "Work item: $item. Task: $(cat "$EVALS/tasks/$task/task.md") Name $item in every commit subject. Commit your work when done." \
    --model "$MODEL" --max-turns 40 --max-budget-usd 2 \
    --allowedTools "Bash,Read,Write,Edit,Glob,Grep" \
    --append-system-prompt-file "$OUT/guidance-$side.md") > "$res/run$n.log" 2>&1
  tr="$(record_of "$proj")"
  if [ -z "$tr" ]; then echo "no run record for $side/$task/$n (see $res/run$n.log)" >&2; return 1; fi
  python3 "$PY" behaviours --item "$item" "$tr" > "$res/run$n.json" || return 1
  echo "$side $task run$n: $(cat "$res/run$n.json")"
}

main() {
  print_plan
  [ "$DRY" = 1 ] && exit 0
  local side t n rc=0
  for side in old new; do
    case ",$SIDES," in *",$side,"*) setup_side "$side" || exit 2 ;; esac
  done
  for side in old new; do
    case ",$SIDES," in *",$side,"*) ;; *) continue ;; esac
    for t in $(tasks); do
      n=1
      while [ "$n" -le "$RUNS" ]; do run_one "$side" "$t" "$n" || rc=2; n=$((n + 1)); done
    done
  done
  [ "$rc" = 0 ] || echo "some runs failed; the comparison counts them as not held" >&2
  if [ "$SIDES" != "old,new" ]; then echo "one side only: no comparison written"; exit "$rc"; fi
  python3 "$PY" compare --results "$OUT/results" --old-ref "$OLD" --new-ref "$NEW" --runs "$RUNS" --out "$OUT/comparison.md"
  local crc=$?
  # A failed run is exit 2 whatever the comparator said. A regression is 1 only
  # when every run finished (bug-fcaea540: failed runs used to pass the check).
  [ "$rc" = 0 ] || exit 2
  exit "$crc"
}
main

#!/bin/bash
# Live check of the wipnote-visuals rule: does the model emit wip:* blocks where it should, and only there?
# Usage: eval/run.sh [reps] [--parallel [N]]   (needs `claude` and bun; writes $OUT_DIR/<cond>_<key>_<n>.txt)
#   reps          replies per cell (default 2)
#   --parallel N  run up to N cells at once (N defaults to 4, capped at 8); without the flag cells run one by one
#   OUT_DIR=path  output directory (default eval/out). Never point two runs at the same directory.
# Conditions: rule = RULE text only; rulestyle = RULE + the Plain English style body.
# Injects the rule with --append-system-prompt, so it does not depend on the mod loading.
# Resumable: a cell whose output is valid is skipped. A failed call is retried once after 5 s, then
# recorded as <cell>.INVALID (analyze.ts reads *.txt only).
set -u
cd "$(dirname "$0")"; HERE=$PWD; REPS=2; PAR=1
while [ $# -gt 0 ]; do case $1 in
  --parallel) PAR=4; [[ ${2:-} =~ ^[0-9]+$ ]] && { PAR=$2; shift; } ;;
  *) REPS=$1 ;;
esac; shift; done
[ "$PAR" -gt 8 ] && PAR=8
OUT=${OUT_DIR:-$HERE/out}; mkdir -p "$OUT/cwd"; export HERE OUT
bun -e "import {RULE} from '../hooks/blocks'; await Bun.write('$OUT/rule.txt', RULE)"
# style body = plain-english.md without its frontmatter
awk 'BEGIN{n=0} /^---$/{n++; next} n>=2' ../../../plugin/output-styles/plain-english.md > "$OUT/style.txt"

# A reply is valid when it has text and does not open with an API or CLI error.
valid() { [ -s "$1" ] && ! head -c 400 "$1" | grep -qiE '^(Error|API Error|Request failed)|rate limit|overloaded|\b(429|529)\b'; }
export -f valid

# One cell: condition, prompt key, rep. The prompt is read here from the .psv by key, never passed as text.
cell() {
  local cond=$1 key=$2 i=$3 name=${1}_${2}_${3}
  local out=$OUT/$name.txt; valid "$out" && return 0
  local prompt sys d try
  prompt=$(awk -F'|' -v k="$key" '$1==k{sub(/^[^|]*\|/,""); print; exit}' "$HERE/prompts.psv")
  sys="$(cat "$OUT/rule.txt")"; [ "$cond" = rulestyle ] && sys="$sys"$'\n\n'"$(cat "$OUT/style.txt")"
  d=$OUT/cwd/$name; mkdir -p "$d"
  for try in 1 2; do
    # GOTCHA: `< /dev/null` is required. claude -p would otherwise read the caller's stdin (the cell
    # list, or the prompts in the old `while read` loop) and every reply would answer the other prompts too.
    # Each cell has its own empty cwd, so parallel runs never share a working directory.
    (cd "$d" && timeout 150 claude -p "$prompt" --append-system-prompt "$sys" --model sonnet --max-turns 1 \
      --no-session-persistence < /dev/null > "$out" 2>&1)
    valid "$out" && { echo "ok      $name"; return 0; }
    [ "$try" = 1 ] && sleep 5
  done
  mv -f "$out" "$OUT/$name.INVALID"; echo "INVALID $name: $(head -c 120 "$OUT/$name.INVALID" | tr '\n' ' ')"
}
export -f cell

start=$SECONDS
for i in $(seq 1 "$REPS"); do for cond in rule rulestyle; do
  cut -d'|' -f1 prompts.psv | awk -v c="$cond" -v i="$i" '{print c, $1, i}'
done; done | xargs -P "$PAR" -L 1 bash -c 'cell "$@"' _

# Cells are <cond>_<key>_<n>; rule.txt and style.txt have no underscore
n_ok=$(find "$OUT" -maxdepth 1 -name 'rule*_*_*.txt' | wc -l); n_bad=$(find "$OUT" -maxdepth 1 -name 'rule*_*_*.INVALID' | wc -l)
echo "done: $OUT  valid $n_ok  invalid $n_bad  parallel $PAR  wall $((SECONDS - start))s  (analyze: OUT_DIR=$OUT bun eval/analyze.ts)"

#!/bin/bash
# Live check of the wipnote-visuals rule: does the model emit wip:* blocks where it should, and only there?
# Usage: eval/run.sh [reps]   (needs `claude` and bun; writes eval/out/<cond>_<key>_<n>.txt)
# Conditions: rule = RULE text only; rulestyle = RULE + the Plain English style body.
# Injects the rule with --append-system-prompt, so it does not depend on the mod loading.
set -u
cd "$(dirname "$0")"; HERE=$PWD; OUT=$HERE/out; REPS=${1:-2}; mkdir -p "$OUT" "$OUT/cwd"
bun -e "import {RULE} from '../hooks/blocks'; await Bun.write('$OUT/rule.txt', RULE)"
# style body = plain-english.md without its frontmatter
awk 'BEGIN{n=0} /^---$/{n++; next} n>=2' ../../../plugin/output-styles/plain-english.md > "$OUT/style.txt"
for i in $(seq 1 "$REPS"); do for cond in rule rulestyle; do
  while IFS='|' read -r key prompt; do
    sys="$(cat "$OUT/rule.txt")"; [ "$cond" = rulestyle ] && sys="$sys"$'\n\n'"$(cat "$OUT/style.txt")"
    d=$OUT/cwd/${cond}_${key}_$i; mkdir -p "$d"; cd "$d"
    # GOTCHA: `< /dev/null` is required. Inside a `while read` loop, claude -p would otherwise
    # swallow the loop's stdin and every reply would answer the other prompts too.
    timeout 150 claude -p "$prompt" --append-system-prompt "$sys" --model sonnet --max-turns 1 \
      --no-session-persistence < /dev/null > "$OUT/${cond}_${key}_$i.txt" 2>&1
    cd "$HERE"
  done < prompts.psv
done; done
echo "done: $OUT  (analyze: bun eval/analyze.ts)"

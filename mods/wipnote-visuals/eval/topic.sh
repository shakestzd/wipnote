#!/bin/bash
# Cross-contamination check: does each saved reply answer its own prompt?
# Usage: eval/topic.sh [out-dir]   (default eval/out). Prints one line per off-topic reply, then a count.
# A reply is on topic when it contains a keyword its prompt makes unavoidable (prompts.psv, left column).
cd "$(dirname "$0")"; OUT=${1:-${OUT_DIR:-$PWD/out}}
topic() { case $1 in
  steps) echo 'go (mod|test)|go\.mod' ;; compare) echo 'sqlite' ;; callout) echo 'force' ;;
  none1) echo 'rebase' ;; none2) echo 'hook' ;; none3) echo 'undefined|package|export' ;; none4) echo 'mutex|channel' ;;
  *) echo '.' ;; esac; }
bad=0; n=0
for f in "$OUT"/rule*_*_*.txt; do
  [ -e "$f" ] || continue; n=$((n + 1))
  key=$(basename "$f" .txt | cut -d_ -f2)
  grep -qiE "$(topic "$key")" "$f" || { echo "off-topic: $(basename "$f")"; bad=$((bad + 1)); }
done
echo "topic check: $n replies, $bad off-topic"

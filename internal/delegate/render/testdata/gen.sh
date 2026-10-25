#!/usr/bin/env bash
# Regenerates the golden outputs in this folder from the codex-herdr-pane mod's
# jq and awk pipeline (mods/codex-herdr-pane/viewer), the reference the Go
# filters must match byte for byte. The tests never run this script: they read
# the files it wrote. Run it only while the mod is still in the tree, and only
# when an input (*.raw, *.jsonl) is added or changed.
#
#   internal/delegate/render/testdata/gen.sh [viewer-dir]
#
# Each pipeline below is the one hooks/panescript.ts and hooks/mdpipe.ts run.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
V="${1:-$HERE/../../../../mods/codex-herdr-pane/viewer}"
cd "$V"

cx() { jq --unbuffered -R -r "$@" -f codex.jq; }
jqs() { local nf=; case "$1" in view|pane) ;; *) nf=-n;; esac; jq --unbuffered $nf -R -r --arg mode "$1" -f stream.jq; }

for raw in "$HERE"/codex/*.raw; do
  b="${raw%.raw}"
  # clean.log: codex.jq text mode, then the noise rules (CODEX_RUN)
  cx < "$raw" | awk -v review=1 -f clean.awk > "$b.clean.log"
  # clean.md: mdcodex (noise rules, codex.jq md mode, joinfence.awk)
  awk -v seen= -v review=1 -f clean.awk < "$raw" | cx --arg mode md | awk -f joinfence.awk > "$b.clean.md"
  # usage: the last turn's split, else the line after "tokens used" in clean.log (USAGE)
  cx --arg mode usage < "$raw" | tail -n 1 > "$b.usage"
  if [ ! -s "$b.usage" ]; then
    k=$(grep -A1 "^tokens used" "$b.clean.log" | tail -1 || true)
    if [ -n "$k" ]; then echo "$k tokens" > "$b.usage"; fi
  fi
  # last.md: codex writes its final message itself (--output-last-message);
  # run3.last.md is a real one, the others are the last agent message as is
  case "$b" in */run3) ;; *)
    jq -n -R -j '[inputs | fromjson? | select(type == "object" and .type == "item.completed" and .item.type == "agent_message") | .item.text] | last // empty' \
      < "$raw" > "$b.last.md" ;;
  esac
done

for log in "$HERE"/claude/*.jsonl; do
  b="${log%.jsonl}"
  jqs pane < "$log" > "$b.clean.log"   # headless claude in a pane (CLAUDE_RUN)
  jqs md < "$log" > "$b.clean.md"
  jqs view < "$log" > "$b.view.log"    # ccx: clean.log from its stream-json log (CCX_RUN)
  jqs mdlive < "$log" > "$b.mdlive.md" # ccx while the run is live
  # CLAUDE_END: one end pass, the usage line, then the answer
  jqs end < "$log" > "$b.end.out"
  { IFS= read -r u; if [ -n "$u" ]; then printf "%s\n" "$u"; fi > "$b.usage"; cat > "$b.last.md"; } < "$b.end.out"
  rm -f "$b.end.out"
done

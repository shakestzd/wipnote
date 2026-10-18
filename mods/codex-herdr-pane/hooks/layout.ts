// Where a delegation's herdr pane opens. The one place that knows the layout rules and the
// registry of this session's live delegation panes; wrap.ts only splices the snippets in.
//
// herdr (0.9.0) has no equalize command. `pane split` takes --ratio (the share the old
// pane keeps) and `pane resize` takes an unspecified --amount, so the policy below picks
// the split target and direction so that every split is an even halving, which needs
// neither. Verified: `pane split <id> --direction down --ratio 0.7` leaves the old pane 70%.
//
// Policy, by n = live delegation panes of this session (Claude's pane is C):
//   n=0  split C right        -> C | 1
//   n=1  split pane 1 down    -> 1 over 2
//   n=2  split pane 1 right   -> top row 1 | 3
//   n=3  split pane 2 right   -> bottom row 2 | 4   (a 2x2 grid beside C)
//   n>=4 split the newest pane down; n >= maxPanes: no pane, run in the calling shell.
// The registry is $TMPDIR/codex-herdr-panes.<HERDR_PANE_ID>, one pane id per line in
// creation order. An atomic mkdir lock covers prune, decide, split and append, so
// delegations that start in the same second see each other.

export type LayoutOptions = { layout: 'tile' | 'fixed'; direction: 'right' | 'down'; maxPanes: number }

const NO_LOG = '>/dev/null 2>&1'
const NODE_ID = `node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{console.log(JSON.parse(s).result.pane.pane_id)}catch(e){}})' 2>/dev/null`

// The registry helpers. __lock gives up after about 20 s (then the run is plain) and
// breaks a lock whose owner pid is dead.
const HELPERS = [
  '__REG="${TMPDIR:-/tmp}/codex-herdr-panes.${HERDR_PANE_ID:-cur}"',
  '__lock() { local i=0 p; until mkdir "$__REG.lock" 2>/dev/null; do',
  '  p=$(cat "$__REG.lock/pid" 2>/dev/null); if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null; then rm -rf "$__REG.lock"; continue; fi',
  '  i=$((i+1)); [ "$i" -gt 200 ] && return 1; sleep 0.1; done; echo $$ > "$__REG.lock/pid"; }',
  '__unlock() { rm -rf "$__REG.lock"; }',
  // keep only entries whose pane still exists
  '__prune() { local q; touch "$__REG"; : > "$__REG.new"; while read -r q; do',
  `  [ -n "$q" ] && herdr pane get "$q" </dev/null ${NO_LOG} && echo "$q" >> "$__REG.new"; done < "$__REG"; mv "$__REG.new" "$__REG"; }`,
  '__nth() { sed -n "$1p" "$__REG"; }',
  // drop one pane id from the registry (called when its pane closes)
  '__unreg() { __lock || return 0; grep -vxF "$1" "$__REG" > "$__REG.new"; mv "$__REG.new" "$__REG"; __unlock; }',
]

// Sets __P (empty when no pane was made) and, when the cap stopped it, __NOTE.
function tileSection(o: LayoutOptions): string[] {
  const max = Math.max(1, Math.floor(o.maxPanes))
  const opts = '--cwd "$PWD" --no-focus 2>/dev/null'
  return [
    '__P=; __NOTE=',
    'if __lock; then',
    '  __prune; __n=$(wc -l < "$__REG" | tr -d " ")',
    `  if [ "$__n" -ge ${max} ]; then __NOTE="herdr pane limit reached ($__n delegation panes open, layout limit ${max}): ran without a pane"`,
    '  else',
    '    case "$__n" in',
    '      0) __S="--current"; __DIR=right;;',
    '      1) __S=$(__nth 1); __DIR=down;;',
    '      2) __S=$(__nth 1); __DIR=right;;',
    '      3) __S=$(__nth 2); __DIR=right;;',
    '      *) __S=$(__nth "$__n"); __DIR=down;;',
    '    esac',
    `    __P=$(herdr pane split $__S --direction $__DIR ${opts} | ${NODE_ID})`,
    '    [ -n "$__P" ] && echo "$__P" >> "$__REG"',
    '  fi',
    '  __unlock',
    'fi',
    '[ -n "$__NOTE" ] && echo "$__NOTE"',
  ]
}

function fixedSection(o: LayoutOptions): string[] {
  return [`__P=$(herdr pane split --current --direction ${o.direction} --cwd "$PWD" --no-focus 2>/dev/null | ${NODE_ID})`]
}

// Shell lines that define the helpers and leave the new pane's id in __P.
export function splitSection(o: LayoutOptions): string[] {
  return o.layout === 'tile' ? [...HELPERS, ...tileSection(o)] : fixedSection(o)
}

// The shell expression run just before `herdr pane close "$__P"` and when the pane is gone.
export function unregisterCmd(o: LayoutOptions): string {
  return o.layout === 'tile' ? '__unreg "$__P"' : 'true'
}

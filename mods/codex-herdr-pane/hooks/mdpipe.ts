// The Markdown half of the run log, and the noise rules for the text returned to Claude:
// shell text shared by the pane script and the shim so each pipeline is written once.
// `root` is a shell expression for the plugin folder ($ROOT in the pane, $__CODEX_ROOT in
// the shim).
//
// mdcodex     raw Codex stream on stdin -> Markdown on stdout. The noise rules run first
//             (on the raw lines, which are JSONL or noise), because once a noise line is
//             inside a fence clean.awk's line-start patterns no longer match it. joinfence.awk
//             then runs neighbouring text fences together (codex review is plain text, one
//             fence per line otherwise).
// mdrest      the rest of a chained command's output -> Markdown: the same clean.awk, then
//             rest.awk (drops a copy of the final answer), then md.jq's mdline.
// restfilter  the same rules as plain text (what the Bash call returns, and the rest of a chained
//             command in clean.log); $1 is the
//             "MCP note already made" file for that text.
// MCP_SEEN_MD / MCP_SEEN_LOG are set by the pane script in a chained call only, so the MCP
// note is made once per run directory instead of once per process.
export function mdFunctions(root: string): string[] {
  const jq = (mode: string) => `jq --unbuffered -R -r --arg mode ${mode} -f "${root}/viewer/codex.jq" 2>/dev/null`
  const clean = (seen: string, review = '') => `awk -v seen="${seen}"${review} -f "${root}/viewer/clean.awk"`
  const rest = `awk -v last="\${__CODEX_LAST:-}" -f "${root}/viewer/rest.awk"`
  return [
    `mdcodex() { ${clean('${MCP_SEEN_MD:-}', ' -v review=1')} | ${jq('md')} | awk -f "${root}/viewer/joinfence.awk"; }`,
    `mdrest() { ${clean('${MCP_SEEN_MD:-}')} | ${rest} | ${jq('mdraw')}; }`,
    `restfilter() { ${clean('$1')} | ${rest} | jq --unbuffered -R -r --arg mode rest -f "${root}/viewer/codex.jq" 2>/dev/null; }`,
  ]
}

// Prints a newline when the file's last byte is not one (so a report never ends mid-line and
// never gets a blank line it did not need). $(tail -c1) is empty exactly when the file is
// empty or ends in a newline.
export const NL_FN = 'nl_if_open() { [ -n "$(tail -c1 "$1" 2>/dev/null)" ] && echo; return 0; }'

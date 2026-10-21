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

// jqs <mode>: stream.jq (claude's stream-json, for ccx and headless claude) in one of its modes;
// every mode but the line-at-a-time ones (view, pane) reads the whole stream, so needs -n.
export const streamJq = (root: string): string =>
  `jqs() { local nf=; case "$1" in view|pane) ;; *) nf=-n;; esac; jq --unbuffered $nf -R -r --arg mode "$1" -f "${root}/viewer/stream.jq" 2>/dev/null; }`

// mdwriter <clean.md> <status file>: claude's live Markdown writer, run as `tee ... >(mdwriter ...)`.
// A process substitution's subshell holds a copy of tee's stdout, and the pipeline ends only once
// every copy is closed. Were jq the subshell's last command, bash would exec it with that stdout
// redirected, the copy would close at once and the pipeline would end without waiting for the
// writer. The commands after jq keep the subshell, so the pipeline waits for the writer: they
// record jq's status (a failed writer's clean.md is repaired by its caller) and drain stdin, so a
// writer that died never cuts tee off with SIGPIPE and loses raw.log. mdcodex is waited for the
// same way: it is a pipeline.
export const MDWRITER_FN = 'mdwriter() { jqs md >> "$1"; echo "$?" > "$2"; cat > /dev/null; }'

// Prints a newline when the file's last byte is not one (so a report never ends mid-line and
// never gets a blank line it did not need). $(tail -c1) is empty exactly when the file is
// empty or ends in a newline.
export const NL_FN = 'nl_if_open() { [ -n "$(tail -c1 "$1" 2>/dev/null)" ] && echo; return 0; }'

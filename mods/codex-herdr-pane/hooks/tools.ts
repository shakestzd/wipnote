// The delegation tools the mod puts in a herdr pane: one table, one recogniser.
// Adding a tool is one entry here plus, if its log needs a new filter, a case in
// wrap.ts's paneScript.

export const MARKER = '# codex-herdr-pane'

export type DelegationTool = {
  name: string
  // The tool at a command position: the start of the command, or after ; & | ( or
  // a newline, behind optional VAR=value words (and a directory path if allowed).
  pattern: RegExp
  // The same invocation anywhere, with the subcommand captured: for the label.
  loose: RegExp
  title: (sub: string, text: string) => string // the pane title up to the colon; text is the label's prompt
  shim: boolean // inject the tool's shim (shim.ts): codex --output-last-message, claude stream-json
  filter: 'codex' | 'ccx' | 'claude' // how the pane turns the tool's output into a readable log
}

function invocation(name: string, subs: string, path: boolean) {
  const bin = `${path ? '(?:\\S*\\/)?' : ''}${name}`
  return {
    pattern: new RegExp(`(?:^|[;&|(\\n])\\s*(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*${bin}\\s+(?:${subs})(?=\\s|$)`),
    loose: new RegExp(`${name}\\s+(${subs})\\b`),
  }
}

// Headless Claude: claude with -p or --print among its own words (on its own line, or across
// backslash-newlines), bare or behind `dev exec [-d <dir>]` (a helper that runs its arguments in
// a Docker Compose container). Bare means the word claude: the shim (shim.ts) is a shell function,
// which a path such as /usr/local/bin/claude would bypass; behind dev exec any path is the
// container's and dev() sees it. A first word naming a subcommand (claude mcp, claude plugin, ...)
// is never a run, whatever follows it.
const CLAUDE_SUBS = 'mcp|plugins?|auth|agents|attach|auto-mode|daemon|doctor|gateway|import|install|logs|purge|remote-control|respawn|rm|self-hosted-runner|setup-token|stop|kill|ultrareview|update'
const WS = String.raw`(?:[ \t]|\\\n)+`
const DEV_EXEC = String.raw`(?:dev${WS}exec${WS}(?:-d(?:${WS}|=)?\S+${WS})?(?:\S*\/)?)?`
const claudePrint = {
  pattern: new RegExp(String.raw`(?:^|[;&|(\n])\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*${DEV_EXEC}claude`
    + String.raw`(?!${WS}(?:${CLAUDE_SUBS})(?=\s|$))(?:${WS}[^\s;&|()]+)*?${WS}(?:-p|--print)(?=\s|$)`),
  loose: /claude\b[^;&|\n]*?\s(-p|--print)(?=\s|$)/,
}

export const TOOLS: readonly DelegationTool[] = [
  {
    name: 'codex', ...invocation('codex', 'exec|e|review', false),
    title: (sub) => (sub === 'review' ? 'cdx review' : 'cdx'), shim: true, filter: 'codex', // short pane titles
  },
  {
    name: 'ccx', ...invocation('ccx', 'task', true),
    title: () => 'ccx task', shim: false, filter: 'ccx',
  },
  {
    name: 'claude', ...claudePrint,
    title: (_sub, text) => (text.startsWith('/code-review') ? 'Claude review' : 'Claude task'), shim: true, filter: 'claude',
  },
]

const INFO_ONLY = /\s(?:--help|-h|--version|-V)(?=\s|$)/
// A claude run that names its own --output-format is left as asked, unwrapped: its stdout (json,
// text) is the caller's to read, and holds nothing the pane could show while it runs.
const OWN_FORMAT = /\s--output-format(?:=|\s|$)/

// Quoted strings and heredoc bodies hold prompts and text, not commands, so
// they are blanked before the command is searched for an invocation.
function blankText(command: string): string {
  return command
    .replace(/<<-?\s*(['"]?)(\w+)\1[^\n]*\n[\s\S]*?\n\s*\2(?=\n|$)/g, '')
    .replace(/'[^']*'|"(?:\\.|[^"\\])*"/g, '""')
}

// The words of the invocation the tool's pattern found, up to the next separator
function ownWords(tool: DelegationTool, text: string): string {
  const at = tool.pattern.exec(text)?.index ?? 0
  return text.slice(at).replace(/\\\n/g, ' ').replace(/^[;&|(\n]/, '').split(/[;&|\n]/)[0] ?? ''
}

// codex and ccx: an info flag anywhere means no run. claude: only its own words are looked at,
// so `claude -p "x" | sort -h` is still a run.
export function toolFor(command: string): DelegationTool | undefined {
  if (command.includes(MARKER)) return undefined
  const text = blankText(command)
  const tool = TOOLS.find((t) => t.pattern.test(text))
  if (!tool || tool.name !== 'claude') return tool && !INFO_ONLY.test(text) ? tool : undefined
  const own = ownWords(tool, text)
  return INFO_ONLY.test(own) || OWN_FORMAT.test(own) ? undefined : tool
}

// Standalone or chained. A call is STANDALONE when, with quoted text and heredoc
// bodies blanked and every redirection removed, it is exactly one delegation
// invocation: it starts at the tool (behind optional VAR=value words, or dev exec) and holds no
// ; & | newline, backtick, $( or ( ) { }. Then the pane's stdout is the tool's own
// raw stream (Codex JSONL), which the caller must not see, and the report alone is
// returned. Anything else is CHAINED: other commands, or a pipe, whose output the
// caller expects, so wrap.ts returns the report plus the whole command's captured
// output and the shim gives codex's stdout the answer text instead of JSONL.
const REDIRECT = /\s*(?:&>>?|\d*>>?|\d*<)&?[^\s;&|]*/g

export function isStandalone(command: string): boolean {
  if (!toolFor(command)) return false // the tool is then at a command position; no separator remains, so it is the first word
  const text = blankText(command).replace(REDIRECT, ' ').trim()
  return !/[;&|\n`(){}]|\$\(/.test(text)
}

// Whether the command sends stdout somewhere itself (>, >>, 1>, &>, >&), outside quoted text and
// heredoc bodies. A standalone review that does keeps its stdout; the shim (shim.ts) is told so
// by the wrapper, since the descriptor's type alone cannot tell the pane's pipe from the caller's.
export const redirectsStdout = (command: string): boolean => /(?:^|[\s;&|])(?:1|&)?>/.test(blankText(command))

export const isDelegation = (command: string): boolean => toolFor(command) !== undefined
export const isCodexDelegation = isDelegation

// What a codex review with no prompt looks at, from its flags; plain `codex review` reviews the
// working tree.
function reviewTarget(command: string): string {
  const flag = (name: string) => new RegExp(`--${name}(?:=|\\s+)([^\\s;&|]+)`).exec(command)?.[1]
  const base = flag('base'), commit = flag('commit')
  if (base) return `changes against ${base}`
  if (commit) return `commit ${commit.slice(0, 12)}`
  return 'current changes'
}

// A short title for the pane: the start of the quoted prompt, else what a review looks at, else "running".
// The first line of text of the first heredoc in the command, or undefined.
function heredocText(command: string): string | undefined {
  const body = new RegExp(`<<-?\\s*(['"]?)(\\w+)\\1[^\\n]*\\n([\\s\\S]*?)\\n\\s*\\2(?=\\n|$|\\))`).exec(command)?.[3]
  return body?.split('\n').map((l) => l.trim()).find((l) => l !== '')
}

export function paneLabel(command: string): string {
  const tool = toolFor(command) ?? TOOLS.find((t) => t.loose.test(command)) ?? TOOLS[0]!
  const sub = tool.loose.exec(command)?.[1] ?? ''
  const prompt = new RegExp(`${tool.loose.source}[^"'\\n]*?(["'])((?:\\\\.|(?!\\2)[^\\\\\\n])+)\\2`).exec(command)
  // A prompt fed by a heredoc (codex exec "$(cat <<'EOF' … EOF)", or codex exec - <<EOF) is the
  // label, its first line of text: the first quoted text is then its delimiter or an option's
  // value (-C "$PWD")
  const quoted = prompt?.[3]
  const text = heredocText(command) || quoted?.replace(/\s+/g, ' ').trim() || (sub === 'review' ? reviewTarget(command) : '')
  return `${tool.title(sub, text)}: ${text ? text.slice(0, 48).trim() : 'running'}`
}

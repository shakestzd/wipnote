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
  title: (sub: string) => string // the pane title up to the colon
  shim: boolean // inject the answer-file shim (codex --output-last-message)
  filter: 'codex' | 'ccx' // how the pane turns the tool's output into a readable log
}

function invocation(name: string, subs: string, path: boolean) {
  const bin = `${path ? '(?:\\S*\\/)?' : ''}${name}`
  return {
    pattern: new RegExp(`(?:^|[;&|(\\n])\\s*(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*${bin}\\s+(?:${subs})(?=\\s|$)`),
    loose: new RegExp(`${name}\\s+(${subs})\\b`),
  }
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
]

const INFO_ONLY = /\s(?:--help|-h|--version|-V)(?=\s|$)/

// Quoted strings and heredoc bodies hold prompts and text, not commands, so
// they are blanked before the command is searched for an invocation.
function blankText(command: string): string {
  return command
    .replace(/<<-?\s*(['"]?)(\w+)\1[^\n]*\n[\s\S]*?\n\s*\2(?=\n|$)/g, '')
    .replace(/'[^']*'|"(?:\\.|[^"\\])*"/g, '""')
}

export function toolFor(command: string): DelegationTool | undefined {
  if (command.includes(MARKER)) return undefined
  const text = blankText(command)
  if (INFO_ONLY.test(text)) return undefined
  return TOOLS.find((t) => t.pattern.test(text))
}

// Standalone or chained. A call is STANDALONE when, with quoted text and heredoc
// bodies blanked and every redirection removed, it is exactly one delegation
// invocation: it starts at the tool (behind optional VAR=value words) and holds no
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
// The first line of text of the heredoc whose delimiter is `word` (any heredoc when no word is
// given), or undefined.
function heredocText(command: string, word?: string): string | undefined {
  const esc = word === undefined ? '(\\w+)' : `(${word.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`
  const body = new RegExp(`<<-?\\s*(['"]?)${esc}\\1[^\\n]*\\n([\\s\\S]*?)\\n\\s*\\2(?=\\n|$|\\))`).exec(command)?.[3]
  return body?.split('\n').map((l) => l.trim()).find((l) => l !== '')
}

export function paneLabel(command: string): string {
  const tool = toolFor(command) ?? TOOLS.find((t) => t.loose.test(command)) ?? TOOLS[0]!
  const sub = tool.loose.exec(command)?.[1] ?? ''
  const prompt = new RegExp(`${tool.loose.source}[^"'\\n]*?(["'])((?:\\\\.|(?!\\2)[^\\\\\\n])+)\\2`).exec(command)
  // A prompt fed by a heredoc (codex exec "$(cat <<'EOF' … EOF)", or codex exec - <<EOF) has no
  // quoted text, or only its delimiter: its first line of text is the label instead
  const quoted = prompt?.[3]
  const text = (quoted !== undefined && heredocText(command, quoted)) || quoted?.replace(/\s+/g, ' ').trim()
    || heredocText(command) || (sub === 'review' ? reviewTarget(command) : '')
  return `${tool.title(sub)}: ${text ? text.slice(0, 48).trim() : 'running'}`
}

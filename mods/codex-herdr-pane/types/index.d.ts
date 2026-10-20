// The values the Claude Code pane (hooks/runpane.tsx) keeps in $.state: one entry per delegation
// it follows, by the run's id.

export type RunView = {
  id: string
  dir: string // the run directory the wrapper writes (wrap.ts runDirPath)
  label: string
  // pending until the wrapper writes $dir/host: claude (this pane draws it) or herdr (only its end is read)
  host: 'pending' | 'claude' | 'herdr'
  registered: number // ms, when the hook saw the call; a run with no host after a minute is dropped
  started?: number // ms, from $dir/start
  ended?: number // ms, from $dir/end
  md: string // clean.md as last read
  exit?: string // the exit code, once $dir/exit exists
  usage?: string
  answer: string // last.md once the run ended
  tab: 'live' | 'answer'
  page?: string // run.html, once written at the end
  open: Record<string, boolean> // fold index -> opened or closed by the person (else: open if it failed)
}

declare module 'claude-code' {
  interface PluginState {
    'codex-herdr-pane': { runs: Record<string, RunView> }
  }
}

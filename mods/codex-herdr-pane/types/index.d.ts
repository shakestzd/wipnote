// The values the mod keeps in $.state (hooks/runstate.ts): one entry per delegation it follows,
// by the run's id. The log itself is not kept here: the pane reads clean.md when it draws.

export type RunView = {
  id: string
  dir: string // the run directory, under the user's own cache (wrap.ts runDirPath)
  label: string
  // pending until the wrapper writes $dir/host: claude (a Claude Code pane draws it) or herdr
  // (the herdr pane draws it; only its end is read here, for the page)
  host: 'pending' | 'claude' | 'herdr'
  registered: number // ms, when the hook saw the call
  callEnded?: number // ms, when the Bash call returned (a background one: when it was started)
  started?: number // ms, from $dir/start
  ended?: number // ms, from $dir/end, or when the run was given up on
  size: number // bytes of clean.md last seen: a change redraws the pane
  lastChange: number // ms, when clean.md last grew (a run silent for STALL_MS is given up on)
  exit?: string // the exit code once $dir/exit exists; 'gone' when the run ended without one
  usage?: string
  answer: string // last.md once the run ended (cut to MAX_ANSWER)
  page?: string // run.html, once written
  tab: 'live' | 'answer'
  open: Record<string, boolean> // fold index -> opened or closed by the person (else: open if it failed)
}

declare module 'claude-code' {
  interface PluginState {
    'codex-herdr-pane': { runs: Record<string, RunView> }
  }
}

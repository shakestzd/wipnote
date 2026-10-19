export type AgentRow = {
  id: string
  type: string
  description: string
  workItem?: string
  model?: string
  status: string
  startedAt: number
  endedAt?: number
  tools: number
  lastTool?: string
}

export type Limit = { kind: string; percentUsed: number; resetsAt?: string }

export type PromptInfo = {
  dir: string
  branch?: string
  dirty: number
  ahead: number
  behind: number
  model?: string
  ctxPercent?: number
  limits?: Limit[]
}

export type WorkCard = {
  id: string
  title: string
  status: string
  trackId?: string
  trackTitle?: string
  done?: number
  total?: number
  stepsDone?: number
  stepsTotal?: number
}

/** Why a card lookup failed: the item is absent, wipnote could not run, or another error. */
export type FailKind = 'missing' | 'unavailable' | 'error'

export type Step = { description: string; completed: boolean }

/** One work item as `wipnote <kind> show <id> --format json` reports it, for a wipnote-ref card. */
export type Card = {
  id: string
  title: string
  status: string
  type: string
  steps: Step[]
}

/**
 * A card's fetch state: loaded, still loading, or failed with a reason.
 * `at` is when the entry was written (ms), so a stale one is fetched again.
 */
export type CardEntry =
  | { state: 'loaded'; card: Card; at: number }
  | { state: 'loading'; at: number }
  | { state: 'failed'; reason: FailKind; at: number }

export type Mode = 'full' | 'compact' | 'off'
export type Placement = 'above' | 'below'

declare module 'claude-code' {
  interface PluginState {
    'wipnote-orchestra': {
      prompt: PromptInfo | null
      agents: AgentRow[]
      items: string[]
      cards: WorkCard[]
      mainTool: string | null
      now: number
      mode: Mode
      placement: Placement
      permissionMode: string | null
      refs: Record<string, CardEntry>
      claim: string | null
    }
  }
}

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
}

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
    }
  }
}

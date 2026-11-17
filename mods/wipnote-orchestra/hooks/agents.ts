import type { AgentRow, Tally } from '../types'
import { STALL_MS, isLive } from './model'

// The band's agent rows: synced with the engine's agent list and counted by
// the tool calls each agent makes. Pure: register.tsx owns the atoms and every `$` call, and
// passes the rows in and writes the rows back.

/** What a spawn said about the agent before the engine named it. */
export type Hint = { workItem?: string; model?: string }

/** The agents the engine lists (`$.agent.list()`), as far as the rows need them. */
export type Listed = { id: string; name?: string; type: string; description: string; status: string }

/**
 * Syncs the rows with the engine's agent list. A row the list has shown and no longer shows ended (the
 * engine dropped its task) and counts as done. A row only tool calls made (an agent the list never
 * showed, such as a workflow's) is closed uncounted once it has been quiet for STALL_MS with no
 * call running. Each agent is counted in the tally once: `ended` is what this sync adds to it.
 */
export function reconcileRows(
  rows: readonly AgentRow[],
  listed: readonly Listed[],
  at: number,
  hints: ReadonlyMap<string, Hint>,
): { rows: AgentRow[]; ended: Tally } {
  const ended: Tally = { done: 0, failed: 0 }
  const close = (row: AgentRow, status: string): AgentRow => {
    if (row.counted === true) return { ...row, status, endedAt: row.endedAt ?? at }
    if (status === 'failed' || status === 'killed') ended.failed += 1
    else ended.done += 1
    return { ...row, status, endedAt: at, counted: true }
  }
  const byId = new Map(rows.map(row => [row.id, row]))
  for (const info of listed) {
    const row = byId.get(info.id)
    const wasLive = row === undefined || isLive(row.status)
    const next: AgentRow = {
      tools: 0,
      startedAt: at,
      ...row,
      id: info.id,
      type: info.name ?? info.type,
      description: info.description,
      workItem: row?.workItem ?? hints.get(info.description)?.workItem,
      model: row?.model ?? hints.get(info.description)?.model,
      status: info.status,
      listed: true,
    }
    byId.set(info.id, wasLive && !isLive(info.status) ? close(next, info.status) : next)
  }
  const seen = new Set(listed.map(info => info.id))
  for (const row of byId.values()) {
    if (seen.has(row.id) || !isLive(row.status)) continue
    if (row.listed === true) byId.set(row.id, close(row, 'completed'))
    else if ((row.inFlight ?? 0) === 0 && at - (row.lastAt ?? row.startedAt) >= STALL_MS) {
      byId.set(row.id, { ...row, status: 'completed', endedAt: at, counted: true })
    }
  }
  return { rows: [...byId.values()].slice(-100), ended }
}

/** A subagent's tool call starts: its row counts the call and shows it as running. */
export function callStarted(
  rows: readonly AgentRow[],
  id: string,
  doing: string,
  pending: ReadonlyMap<string, Hint>,
  at: number,
): AgentRow[] {
  const row = rows.find(one => one.id === id)
  const base: AgentRow = row ?? {
    id,
    type: 'agent',
    description: '',
    status: 'running',
    startedAt: at,
    tools: 0,
  }
  const hint = pending.get(base.description)
  const workItem = base.workItem ?? hint?.workItem
  const model = base.model ?? hint?.model
  return [
    ...rows.filter(one => one.id !== id),
    { ...base, workItem, model, tools: base.tools + 1, lastTool: doing, lastAt: at, inFlight: (base.inFlight ?? 0) + 1 },
  ]
}

/** A subagent's tool call ended, answered or thrown: one fewer running for it. */
export function callEnded(rows: readonly AgentRow[], id: string, at: number): AgentRow[] {
  return rows.map(row => (row.id === id ? { ...row, lastAt: at, inFlight: Math.max(0, (row.inFlight ?? 1) - 1) } : row))
}

/** A main-loop spawn's row once the engine names the agent: its work item and model are kept. */
export function spawnedRows(
  rows: readonly AgentRow[],
  fresh: Pick<AgentRow, 'id' | 'type' | 'description'>,
  hint: Hint,
  model: string,
  at: number,
): AgentRow[] {
  const row = rows.find(one => one.id === fresh.id)
  const base: AgentRow = row ?? { ...fresh, status: 'running', startedAt: at, tools: 0 }
  return [...rows.filter(one => one.id !== fresh.id), { ...base, workItem: base.workItem ?? hint.workItem, model }]
}

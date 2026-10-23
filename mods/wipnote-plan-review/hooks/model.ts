import type { Approval, SliceRow } from '../types'

const PLAN_ID = 'plan-[0-9a-z]+'

/** `wipnote plan <sub> <plan-id>` calls that put a plan in the pane. */
const OPENS = new RegExp(`\\bwipnote\\s+plan\\s+(?:slices|approve-slice|reject-slice)\\s+(${PLAN_ID})\\b`, 'g')
/** Every other `wipnote plan <sub> <plan-id>` call: it may change the plan in view. */
const TOUCHES = new RegExp(`\\bwipnote\\s+plan\\s+[a-z-]+\\s+(${PLAN_ID})\\b`, 'g')

export type PlanCall = { planId: string; opens: boolean }

/** The last plan a shell command names, and whether it should open the pane. */
export function planCallIn(command: string): PlanCall | undefined {
  const opened = [...command.matchAll(OPENS)].map(m => m[1])
  if (opened.length > 0) return { planId: opened[opened.length - 1]!, opens: true }
  const touched = [...command.matchAll(TOUCHES)].map(m => m[1])
  if (touched.length > 0) return { planId: touched[touched.length - 1]!, opens: false }
  return undefined
}

const APPROVALS: readonly Approval[] = ['pending', 'approved', 'rejected', 'changes_requested']

function text(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

export type Parsed = { title: string; status: string; slices: SliceRow[] }

/** Reads `wipnote plan slices` JSON; undefined when it is not that shape. */
export function parseSlices(stdout: string): Parsed | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(stdout)
  } catch {
    return undefined
  }
  if (typeof raw !== 'object' || raw === null) return undefined
  const o = raw as Record<string, unknown>
  if (!Array.isArray(o.slices)) return undefined
  const slices: SliceRow[] = []
  for (const s of o.slices as Record<string, unknown>[]) {
    if (typeof s !== 'object' || s === null || typeof s.num !== 'number') continue
    const approval = APPROVALS.includes(s.approval_status as Approval) ? (s.approval_status as Approval) : 'pending'
    slices.push({
      num: s.num,
      title: text(s.title) ?? `Slice ${s.num}`,
      complexity: text(s.complexity) ?? 'standard',
      effort: text(s.effort),
      risk: text(s.risk),
      approval,
      execution: text(s.execution_status),
      featureId: text(s.feature_id),
    })
  }
  return { title: text(o.title) ?? '', status: text(o.status) ?? '', slices }
}

export const MARK: Record<Approval, { glyph: string; color: string; label: string }> = {
  approved: { glyph: '✔', color: 'green', label: 'approved' },
  changes_requested: { glyph: '↺', color: 'yellow', label: 'changes' },
  rejected: { glyph: '✘', color: 'red', label: 'rejected' },
  pending: { glyph: '○', color: 'gray', label: 'pending' },
}

/** "2 approved · 1 changes · 3 pending", in review order, zero counts left out. */
export function tallyLine(slices: SliceRow[]): string {
  const order: Approval[] = ['approved', 'changes_requested', 'rejected', 'pending']
  const parts = order
    .map(a => [a, slices.filter(s => s.approval === a).length] as const)
    .filter(([, n]) => n > 0)
    .map(([a, n]) => `${n} ${MARK[a].label}`)
  return parts.length > 0 ? parts.join(' · ') : 'no slices'
}

/** "standard · M · Low · promoted": the slice's facts after its title. */
export function detailLine(s: SliceRow): string {
  return [s.complexity, s.effort, s.risk, s.execution && s.execution !== 'not_started' ? s.execution : undefined]
    .filter((p): p is string => p !== undefined && p !== '')
    .join(' · ')
}

/** The first line of a failed run, short enough for the pane. */
export function failReason(stderr: string, exitCode: number): string {
  const line = stderr.split('\n').map(l => l.trim()).find(l => l !== '')
  return (line ?? `wipnote exited ${exitCode}`).replace(/^Error:\s*/, '').slice(0, 160)
}

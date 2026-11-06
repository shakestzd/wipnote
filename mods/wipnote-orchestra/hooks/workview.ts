import type { WorkCounts } from '../types'

/** How much of the work-item block the band draws: the active item, a capped list, or nothing. */
export type WorkView = { mode: 'active' | 'list' | 'off'; rows: number }

export const DEFAULT_VIEW: WorkView = { mode: 'active', rows: 3 }

/** The `workItems` and `workItemRows` options; anything unknown keeps the default. */
export function viewOf(options: { workItems?: unknown; workItemRows?: unknown }): WorkView {
  const mode = options.workItems === 'list' || options.workItems === 'off' ? options.workItems : DEFAULT_VIEW.mode
  const wanted = Number(options.workItemRows)
  const rows = Number.isInteger(wanted) && wanted >= 1 ? wanted : DEFAULT_VIEW.rows
  return { mode, rows }
}

/** The work items to draw: only the active one by default, the first `rows` in list mode, none when off. */
export function visibleWork(work: readonly string[], view: WorkView): string[] {
  if (view.mode === 'off') return []
  return view.mode === 'list' ? work.slice(0, view.rows) : work.slice(0, 1)
}

/**
 * The in-progress and open (todo) counts from `wipnote status`: its feature, bug and spike lines
 * (tracks are groupings, not work). Undefined when no such line is found.
 */
export function parseStatusCounts(text: string): WorkCounts | undefined {
  let found = false
  const counts = { active: 0, open: 0 }
  for (const line of text.split('\n')) {
    const m = /^\s*(?:features|bugs|spikes)\s.*\btodo:(\d+)\s+active:(\d+)/.exec(line)
    if (m === null) continue
    found = true
    counts.open += Number(m[1])
    counts.active += Number(m[2])
  }
  return found ? counts : undefined
}

/** "N in progress, M open", or empty while the counts are unknown. */
export function summaryText(counts: WorkCounts | undefined): string {
  return counts === undefined ? '' : `${counts.active} in progress, ${counts.open} open`
}

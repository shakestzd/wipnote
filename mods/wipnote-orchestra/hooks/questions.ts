// Open wipnote questions, read from `wipnote question list`. The CLI owns the state; this keeps
// only the ids already toasted, so a blocking question toasts once.

export type OpenQuestion = {
  itemId: string
  id: string
  text: string
  blocking: boolean
  recommended: string
}

export function openArgv(): string[] {
  return ['wipnote', 'question', 'list', '--open', '--format', 'json']
}

export function itemQuestionsArgv(itemId: string): string[] {
  return ['wipnote', 'question', 'list', '--item', itemId, '--open', '--format', 'json']
}

function text(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

/** Reads `question list --format json`; undefined when it is not a list (null is an empty one). */
export function parseOpen(stdout: string): OpenQuestion[] | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(stdout)
  } catch {
    return undefined
  }
  if (raw === null) return []
  if (!Array.isArray(raw)) return undefined
  const out: OpenQuestion[] = []
  for (const v of raw) {
    if (typeof v !== 'object' || v === null) continue
    const o = v as Record<string, unknown>
    if (text(o.question_id) === '') continue
    out.push({ itemId: text(o.item_id), id: text(o.question_id), text: text(o.text), blocking: o.blocking === true, recommended: text(o.recommended) })
  }
  return out
}

/** "N open questions · M blocking", or empty with none open or unknown. */
export function questionsText(rows: readonly OpenQuestion[] | undefined): string {
  if (rows === undefined || rows.length === 0) return ''
  return `${rows.length} open question${rows.length === 1 ? '' : 's'} · ${rows.filter(r => r.blocking).length} blocking`
}

/** The blocking questions not in `seen`; marks every row seen. */
export function fresh(rows: readonly OpenQuestion[], seen: Set<string>): OpenQuestion[] {
  const out = rows.filter(r => r.blocking && !seen.has(r.id))
  for (const r of rows) seen.add(r.id)
  return out
}

/** One line per question for a ref card. */
export function questionLines(rows: readonly OpenQuestion[]): string[] {
  return rows.map(r => `? ${r.text}${r.blocking ? ' · blocking' : ''}${r.recommended !== '' ? ` · recommends ${r.recommended}` : ''}`)
}

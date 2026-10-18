export type Step = { description: string; completed: boolean }

/** One work item as `wipnote <kind> show <id> --format json` reports it. */
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
  | { state: 'failed'; reason: string; at: number }

declare module 'claude-code' {
  interface PluginState {
    'wipnote-cards': { cards: Record<string, CardEntry> }
  }
}

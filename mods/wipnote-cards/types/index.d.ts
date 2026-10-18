export type Step = { description: string; completed: boolean }

/** One work item as `wipnote <kind> show <id> --format json` reports it. */
export type Card = {
  id: string
  title: string
  status: string
  type: string
  steps: Step[]
}

/** A card's fetch state: loaded, still loading, or failed with a reason. */
export type CardEntry =
  | { state: 'loaded'; card: Card }
  | { state: 'loading' }
  | { state: 'failed'; reason: string }

declare module 'claude-code' {
  interface PluginState {
    'wipnote-cards': { cards: Record<string, CardEntry> }
  }
}

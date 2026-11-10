export type Option = { key: string; label: string; tradeoff?: string }

/** One open question as `wipnote question list --open --format json` prints it. */
export type QuestionRow = {
  itemId: string
  itemTitle: string
  id: string
  text: string
  options: Option[]
  recommended: string
  blocking: boolean
  age: string
}

/** The list the pane shows, or why it cannot show it. */
export type Listing =
  | { state: 'loaded'; rows: QuestionRow[]; at: number }
  | { state: 'failed'; reason: string; at: number }

declare module 'claude-code' {
  interface PluginState {
    'wipnote-questions': {
      listing: Shaped<Listing | null>
      /** The last answer's output, shown under the list. */
      answered: string | null
    }
  }
}

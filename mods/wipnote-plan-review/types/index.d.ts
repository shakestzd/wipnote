export type Approval = 'pending' | 'approved' | 'rejected' | 'changes_requested'

/** One slice as `wipnote plan slices <plan-id>` prints it. */
export type SliceRow = {
  num: number
  title: string
  complexity: string
  effort?: string
  risk?: string
  approval: Approval
  execution?: string
  featureId?: string
}

/** The plan the pane shows, or why it cannot show it. */
export type Review =
  | { state: 'loaded'; planId: string; title: string; status: string; slices: SliceRow[]; at: number }
  | { state: 'failed'; planId: string; reason: string; at: number }

declare module 'claude-code' {
  interface PluginState {
    'wipnote-plan-review': {
      planId: string | null
      review: Shaped<Review | null>
    }
  }
}

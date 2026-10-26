// Conversation swaps: /clear, /resume and /branch keep the process but move it to another
// session id, and no session.start fires for the new conversation. The mod hooks no classic.*
// event (a managed, prepend-tier plugin may continue them all past every user-tier plugin), so
// a swap is seen as a changed `$.session.id()`: after session.end with `clear` or `resume`, and
// on the band's 1 s tick. Both paths go through one tracker, so a swap rehydrates once however
// many of them see it. The tracker is plain state; register.tsx reads the id and rehydrates.

export type SwapState = {
  /** The session id last seen; undefined before the first. */
  seen?: string
}

/** session.start: the id the session started under; nothing to rehydrate. */
export function swapStarted(s: SwapState, id: string | undefined): void {
  if (id !== undefined) s.seen = id
}

/** An id check: true (rehydrate) when the id moved since it was last seen. */
export function swapSeen(s: SwapState, id: string | undefined): boolean {
  if (id === undefined || id === s.seen) return false
  const isFirst = s.seen === undefined
  s.seen = id
  return !isFirst
}

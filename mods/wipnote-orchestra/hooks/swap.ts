// Conversation swaps: /clear, /resume and /branch keep the process but move it to another
// session id, and no session.start fires for the new conversation. classic.SessionStart says
// so, but a managed (prepend-tier) plugin may continue classic.* events past every user-tier
// plugin, so it cannot be the only path. The swap is also seen as a changed `$.session.id()`
// (after session.end with `clear` or `resume`, and on the band's 1 s tick). Every path goes
// through one tracker, so a swap rehydrates once however many of them see it. The tracker is
// plain state; register.tsx reads the id and rehydrates.

export type SwapState = {
  /** The session id last seen; undefined before the first. */
  seen?: string
  /** The id the id check last rehydrated for, until classic.SessionStart for it consumes it. */
  swappedTo?: string
}

/** session.start: the id the session started under; nothing to rehydrate. */
export function swapStarted(s: SwapState, id: string | undefined): void {
  if (id !== undefined) s.seen = id
  s.swappedTo = undefined
}

/** An id check: true (rehydrate) when the id moved since it was last seen. */
export function swapSeen(s: SwapState, id: string | undefined): boolean {
  if (id === undefined || id === s.seen) return false
  const isFirst = s.seen === undefined
  s.seen = id
  if (isFirst) return false
  s.swappedTo = id
  return true
}

/**
 * classic.SessionStart (not `startup`): true (rehydrate) unless the id check already did for
 * this very id, which it then forgets, so a later SessionStart on that id (a compact) still does.
 */
export function swapClassic(s: SwapState, id: string | undefined): boolean {
  const isDone = id !== undefined && id === s.swappedTo
  s.swappedTo = undefined
  if (isDone) return false
  if (id !== undefined) s.seen = id
  return true
}

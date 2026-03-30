/**
 * pruneOrphanedUnseenIds removes session IDs from the unseen list that have
 * no known mapping — neither a regular session nor a space session.
 *
 * These arise when a session belonged to a now-deleted space: the backend
 * emits "done" events, the frontend marks the ID unseen, but after the space
 * is gone the ID can never be cleared by markSpaceSeen (getSessionSpaceId
 * returns null for it), so the badge count stays positive forever.
 *
 * @param unseenIds    - current unseen session IDs from localStorage
 * @param isKnownSession - returns true if id is a regular (non-space) session
 * @param getSpaceId   - returns the space id that owns the session, or null
 */
export function pruneOrphanedUnseenIds(
  unseenIds: string[],
  isKnownSession: (id: string) => boolean,
  getSpaceId: (id: string) => string | null,
): string[] {
  return unseenIds.filter(id => isKnownSession(id) || getSpaceId(id) !== null)
}

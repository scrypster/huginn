import { type Ref } from 'vue'

/**
 * useOptimisticUpdate — generic helper for optimistic list mutations with
 * automatic rollback on API failure.
 *
 * Usage:
 *   const { update } = useOptimisticUpdate(myList, item => item.id)
 *   await update(itemId, { enabled: true }, () => api.put(...))
 *
 * If the API call throws, the item is restored to its state before the call.
 * All other items in the list are unaffected.
 */
export function useOptimisticUpdate<T extends object>(
  list: Ref<T[]>,
  getKey: (item: T) => string,
) {
  /**
   * Apply optimisticPatch to the item identified by key, call apiFn, and roll
   * back to the original item if apiFn throws. The error is re-thrown so
   * callers can still display it.
   */
  async function update(
    key: string,
    optimisticPatch: Partial<T>,
    apiFn: () => Promise<void>,
  ): Promise<void> {
    const idx = list.value.findIndex(item => getKey(item) === key)
    if (idx === -1) {
      // Item not in list — just call the API without any optimistic mutation.
      await apiFn()
      return
    }

    const original = { ...list.value[idx] } as T
    list.value[idx] = { ...original, ...optimisticPatch }

    try {
      await apiFn()
    } catch (e) {
      // Rollback: restore the original item.
      // Re-find the index in case the list was re-ordered between the patch
      // and the error (e.g. a concurrent load replaced the array).
      const rollbackIdx = list.value.findIndex(item => getKey(item) === key)
      if (rollbackIdx !== -1) {
        list.value[rollbackIdx] = original
      }
      throw e
    }
  }

  /**
   * Remove the item identified by key optimistically, and roll it back
   * (at the same position) if apiFn throws.
   */
  async function remove(
    key: string,
    apiFn: () => Promise<void>,
  ): Promise<void> {
    const idx = list.value.findIndex(item => getKey(item) === key)
    if (idx === -1) {
      await apiFn()
      return
    }

    const original = list.value[idx]!
    list.value.splice(idx, 1)

    try {
      await apiFn()
    } catch (e) {
      // Rollback: re-insert the item at its original index.
      list.value.splice(idx, 0, original)
      throw e
    }
  }

  return { update, remove }
}

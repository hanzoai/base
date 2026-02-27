/**
 * useMutation -- async mutation with optimistic update support.
 *
 * Returns a `mutate()` function that calls the appropriate
 * CollectionService method. Supports optimistic updates via
 * an `optimistic(store)` callback that applies immediately
 * and rolls back on error.
 */

import { useCallback, useRef, useSyncExternalStore } from 'react'
import type { BaseRecord, QueryStore } from '@hanzoai/base'
import { useBaseClient } from './provider.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type MutationAction = 'create' | 'update' | 'delete'

export interface MutationOptions<TData = Record<string, unknown>> {
  /** Optimistic update callback. Receives the store and input data. */
  optimistic?: (store: QueryStore, data: TData) => string | void
  /** Called on success with the server response. */
  onSuccess?: (record: BaseRecord) => void
  /** Called on error. */
  onError?: (error: Error) => void
  /** Called after success or error. */
  onSettled?: () => void
}

export interface UseMutationResult<TData = Record<string, unknown>> {
  mutate: (data: TData, id?: string) => Promise<BaseRecord | void>
  isLoading: boolean
  error: Error | null
  reset: () => void
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useMutation<TData extends Record<string, unknown> = Record<string, unknown>>(
  collection: string,
  action: MutationAction,
  options?: MutationOptions<TData>,
): UseMutationResult<TData> {
  const client = useBaseClient()

  // Mutable state for loading and error, tracked via a version counter.
  const stateRef = useRef({ loading: false, error: null as Error | null, version: 0 })

  const subscribe = useCallback((onStoreChange: () => void) => {
    // We use a simple polling-free approach: stateRef.version is bumped
    // on every state change, and we store the callback to call manually.
    const ref = { cb: onStoreChange }
    callbackRef.current = ref.cb
    return () => {
      if (callbackRef.current === ref.cb) {
        callbackRef.current = null
      }
    }
  }, [])

  const callbackRef = useRef<(() => void) | null>(null)

  const notify = useCallback(() => {
    stateRef.current.version++
    callbackRef.current?.()
  }, [])

  const getSnapshot = useCallback(() => stateRef.current.version, [])

  // Subscribe to force re-renders on state change.
  useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  const mutate = useCallback(
    async (data: TData, id?: string): Promise<BaseRecord | void> => {
      stateRef.current.loading = true
      stateRef.current.error = null
      notify()

      let optimisticId: string | undefined

      // Apply optimistic update if provided.
      if (options?.optimistic) {
        const result = options.optimistic(client.store, data)
        if (typeof result === 'string') {
          optimisticId = result
        }
      }

      try {
        let record: BaseRecord | void

        switch (action) {
          case 'create':
            record = await client.create(collection, data)
            break
          case 'update':
            if (!id) throw new Error('useMutation: `id` is required for update')
            record = await client.update(collection, id, data)
            break
          case 'delete':
            if (!id) throw new Error('useMutation: `id` is required for delete')
            await client.delete(collection, id)
            record = undefined
            break
        }

        // Rollback optimistic entry (server truth replaces it).
        if (optimisticId) {
          client.store.rollbackOptimistic(optimisticId)
        }

        stateRef.current.loading = false
        notify()

        if (record) {
          options?.onSuccess?.(record)
        }
        options?.onSettled?.()
        return record
      } catch (err) {
        // Rollback optimistic on error.
        if (optimisticId) {
          client.store.rollbackOptimistic(optimisticId)
        }

        const error = err instanceof Error ? err : new Error(String(err))
        stateRef.current.loading = false
        stateRef.current.error = error
        notify()

        options?.onError?.(error)
        options?.onSettled?.()
        throw error
      }
    },
    [client, collection, action, options, notify],
  )

  const reset = useCallback(() => {
    stateRef.current.loading = false
    stateRef.current.error = null
    notify()
  }, [notify])

  return {
    mutate,
    isLoading: stateRef.current.loading,
    error: stateRef.current.error,
    reset,
  }
}

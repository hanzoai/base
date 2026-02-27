/**
 * useQuery -- reactive collection query with SSE sync.
 *
 * Subscribes to the QueryStore for a (collection, filter) pair using
 * useSyncExternalStore. Automatically fetches on mount and subscribes
 * to realtime updates via subscribeAndSync.
 *
 * Multiple components using the same (collection, filter) share one
 * SSE connection through the RealtimeService ref-counting.
 */

import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react'
import type { BaseRecord, ListOptions } from '@hanzoai/base'
import { useBaseClient } from './provider.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseQueryOptions extends ListOptions {
  /** Disable automatic fetching. Default false. */
  enabled?: boolean
  /** Subscribe to realtime updates. Default true. */
  realtime?: boolean
}

export interface UseQueryResult<T extends BaseRecord = BaseRecord> {
  data: T[]
  isLoading: boolean
  error: Error | null
  refetch: () => Promise<void>
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useQuery<T extends BaseRecord = BaseRecord>(
  collection: string,
  options?: UseQueryOptions,
): UseQueryResult<T> {
  const client = useBaseClient()
  const filter = options?.filter ?? ''
  const enabled = options?.enabled !== false
  const realtimeEnabled = options?.realtime !== false

  // Stable reference for the current snapshot from the store.
  const snapshotRef = useRef<T[]>(
    (client.store.getQuery(collection, filter) as T[] | undefined) ?? [],
  )
  const errorRef = useRef<Error | null>(null)
  const loadingRef = useRef(enabled)

  // Subscribe to QueryStore changes via useSyncExternalStore.
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      return client.store.subscribe(collection, filter, (records) => {
        snapshotRef.current = records as T[]
        onStoreChange()
      })
    },
    [client, collection, filter],
  )

  const getSnapshot = useCallback(() => {
    return snapshotRef.current
  }, [])

  const data = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  // Track loading state with a separate subscription.
  const loadingSubscribe = useCallback(
    (onStoreChange: () => void) => {
      // Loading state is managed internally; we piggyback on the store
      // subscription to trigger re-renders when loading changes.
      return client.store.subscribe(collection, filter, () => {
        if (loadingRef.current) {
          loadingRef.current = false
          onStoreChange()
        }
      })
    },
    [client, collection, filter],
  )

  const getLoadingSnapshot = useCallback(() => {
    return loadingRef.current
  }, [])

  const isLoading = useSyncExternalStore(
    loadingSubscribe,
    getLoadingSnapshot,
    getLoadingSnapshot,
  )

  // Fetch function.
  const refetch = useCallback(async () => {
    loadingRef.current = true
    errorRef.current = null
    try {
      await client.list(collection, options)
      loadingRef.current = false
    } catch (err) {
      errorRef.current = err instanceof Error ? err : new Error(String(err))
      loadingRef.current = false
    }
  }, [client, collection, options])

  // Initial fetch on mount.
  useEffect(() => {
    if (!enabled) return
    refetch()
  }, [enabled, collection, filter]) // eslint-disable-line react-hooks/exhaustive-deps

  // Realtime subscription (ref-counted via subscribeAndSync).
  useEffect(() => {
    if (!enabled || !realtimeEnabled) return
    const unsub = client.subscribeAndSync(collection)
    return unsub
  }, [client, collection, enabled, realtimeEnabled])

  // Auto-resubscribe on auth change.
  useEffect(() => {
    if (!enabled) return
    const unsub = client.authStore.onChange(() => {
      refetch()
    })
    return unsub
  }, [client, enabled, refetch])

  return {
    data,
    isLoading,
    error: errorRef.current,
    refetch,
  }
}

/**
 * useRealtime -- low-level SSE subscription hook.
 *
 * Subscribes to realtime events for a specific collection and optional
 * record id. The callback fires on every create/update/delete event.
 *
 * Uses the RealtimeService's built-in ref-counting: the SSE connection
 * is shared across all hooks subscribing to the same collection, and
 * auto-disconnects when the last subscriber unmounts.
 */

import { useEffect, useRef } from 'react'
import type { RealtimeEvent, ConnectionState } from '@hanzoai/base'
import { useBaseClient } from './provider.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type RealtimeHandler = (event: RealtimeEvent) => void

export interface UseRealtimeOptions {
  /** Record id to subscribe to. Default "*" (all records). */
  recordId?: string
  /** Disable the subscription. Default false. */
  enabled?: boolean
  /** Connection state change callback. */
  onConnectionChange?: (state: ConnectionState) => void
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useRealtime(
  collection: string,
  callback: RealtimeHandler,
  options?: UseRealtimeOptions,
): void {
  const client = useBaseClient()
  const topic = options?.recordId ?? '*'
  const enabled = options?.enabled !== false

  // Keep callback ref stable to avoid re-subscribing on every render.
  const callbackRef = useRef(callback)
  callbackRef.current = callback

  // SSE subscription.
  useEffect(() => {
    if (!enabled) return

    const unsub = client.realtime.subscribe(collection, topic, (event) => {
      callbackRef.current(event)
    })

    return unsub
  }, [client, collection, topic, enabled])

  // Connection state listener.
  const onConnectionChangeRef = useRef(options?.onConnectionChange)
  onConnectionChangeRef.current = options?.onConnectionChange

  useEffect(() => {
    if (!enabled || !onConnectionChangeRef.current) return

    const unsub = client.realtime.onConnectionChange((state) => {
      onConnectionChangeRef.current?.(state)
    })

    return unsub
  }, [client, enabled])
}

/**
 * useConnectionState -- track the realtime SSE connection state.
 *
 * Returns the current ConnectionState ('disconnected' | 'connecting' | 'connected').
 * Re-renders only when the state changes.
 */

import { useCallback, useRef, useSyncExternalStore } from 'react'
import type { ConnectionState } from '@hanzoai/base'
import { useBaseClient } from './provider.js'

export function useConnectionState(): ConnectionState {
  const client = useBaseClient()
  const stateRef = useRef<ConnectionState>(client.realtime.state)

  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      return client.realtime.onConnectionChange((state) => {
        stateRef.current = state
        onStoreChange()
      })
    },
    [client],
  )

  const getSnapshot = useCallback(() => stateRef.current, [])

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

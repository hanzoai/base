/**
 * usePresence -- presence tracking via CRDT sync channel.
 *
 * Tracks peers connected to the same CRDT document and broadcasts
 * local presence metadata (cursor position, display name, color, etc.).
 */

import { useState, useCallback, useEffect } from 'react'
import type { CRDTSync, PeerState } from '@hanzoai/base/crdt'

export interface UsePresenceResult {
  /** Map of peer siteId to their current state. */
  peers: Map<string, PeerState>
  /** Our own presence metadata. */
  myState: Record<string, unknown>
  /** Update our presence metadata (broadcast to peers). */
  updateState: (meta: Record<string, unknown>) => void
}

export function usePresence(sync: CRDTSync): UsePresenceResult {
  const [peers, setPeers] = useState<Map<string, PeerState>>(() => new Map(sync.peers))
  const [myState, setMyState] = useState<Record<string, unknown>>({})

  useEffect(() => {
    const unsubscribe = sync.onPeersChange((p) => {
      setPeers(new Map(p))
    })
    return unsubscribe
  }, [sync])

  const updateState = useCallback(
    (meta: Record<string, unknown>) => {
      setMyState(meta)
      sync.updatePresence(meta)
    },
    [sync],
  )

  return { peers, myState, updateState }
}

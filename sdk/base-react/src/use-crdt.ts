/**
 * useCRDT -- collaborative CRDT document hook with auto-sync.
 *
 * Creates a CRDTDocument and CRDTSync instance, connects to the server
 * via WebSocket, and auto-syncs all local operations bidirectionally.
 *
 * Also provides convenience hooks for subscribing to individual CRDT
 * field values: useCRDTText and useCRDTCounter.
 */

import { useState, useCallback, useEffect, useRef } from 'react'
import { useBaseClient } from './provider.js'
import {
  CRDTDocument,
  CRDTSync,
  type CRDTText,
  type CRDTCounter,
  type CRDTSet,
  type CRDTRegister,
  type SyncState,
} from '@hanzoai/base/crdt'

// ---------------------------------------------------------------------------
// useCRDT
// ---------------------------------------------------------------------------

export interface UseCRDTOptions {
  /** WebSocket URL for the CRDT sync endpoint. Auto-derived from client URL if omitted. */
  wsUrl?: string
  /** Site id for this client. Auto-generated if omitted. */
  siteId?: string
}

export interface UseCRDTResult {
  /** The CRDTDocument instance. */
  doc: CRDTDocument
  /** Convenience accessor for text fields. */
  text: (field: string) => CRDTText
  /** Convenience accessor for counter fields. */
  counter: (field: string) => CRDTCounter
  /** Convenience accessor for set fields. */
  set: <T = unknown>(field: string) => CRDTSet<T>
  /** Convenience accessor for register fields. */
  register: <T = unknown>(field: string) => CRDTRegister<T>
  /** The CRDTSync instance (for presence, state listeners, etc.). */
  sync: CRDTSync
  /** Current sync state. */
  syncState: SyncState
}

export function useCRDT(documentId: string, options?: UseCRDTOptions): UseCRDTResult {
  const client = useBaseClient()
  const [syncState, setSyncState] = useState<SyncState>('disconnected')

  // Stable refs: document and sync persist across re-renders.
  // Recreated only when documentId changes.
  const docRef = useRef<CRDTDocument | null>(null)
  const syncRef = useRef<CRDTSync | null>(null)
  const prevDocIdRef = useRef<string>('')

  if (!docRef.current || prevDocIdRef.current !== documentId) {
    docRef.current = new CRDTDocument(documentId, options?.siteId)
    prevDocIdRef.current = documentId
  }
  if (!syncRef.current) {
    syncRef.current = new CRDTSync()
  }

  const doc = docRef.current
  const sync = syncRef.current

  // Connect on mount / documentId change, disconnect on unmount.
  useEffect(() => {
    const wsUrl = options?.wsUrl ?? deriveCrdtWsUrl(client.url)
    const token = client.authStore.token || undefined

    sync.connect(wsUrl, doc, token)

    const unsubState = sync.onStateChange((s) => {
      setSyncState(s)
    })

    return () => {
      unsubState()
      sync.disconnect()
    }
  }, [doc, sync, client, options?.wsUrl])

  // Convenience field accessors.
  const text = useCallback((field: string) => doc.getText(field), [doc])
  const counter = useCallback((field: string) => doc.getCounter(field), [doc])
  const set = useCallback(<T = unknown>(field: string) => doc.getSet<T>(field), [doc])
  const register = useCallback(<T = unknown>(field: string) => doc.getRegister<T>(field), [doc])

  return { doc, text, counter, set, register, sync, syncState }
}

// ---------------------------------------------------------------------------
// useCRDTText -- subscribe to a text field's current value
// ---------------------------------------------------------------------------

export function useCRDTText(doc: CRDTDocument, field: string): string {
  const textCrdt = doc.getText(field)
  const [value, setValue] = useState(() => textCrdt.toString())

  useEffect(() => {
    // Sync initial value in case document was populated before hook mounted.
    setValue(textCrdt.toString())

    const unsubscribe = textCrdt.onChange((text) => {
      setValue(text)
    })
    return unsubscribe
  }, [textCrdt])

  return value
}

// ---------------------------------------------------------------------------
// useCRDTCounter -- subscribe to a counter field's current value
// ---------------------------------------------------------------------------

export function useCRDTCounter(doc: CRDTDocument, field: string): number {
  const counterCrdt = doc.getCounter(field)
  const [value, setValue] = useState(() => counterCrdt.value)

  useEffect(() => {
    setValue(counterCrdt.value)

    const unsubscribe = counterCrdt.onChange((v) => {
      setValue(v)
    })
    return unsubscribe
  }, [counterCrdt])

  return value
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function deriveCrdtWsUrl(httpUrl: string): string {
  const url = httpUrl.replace(/\/$/, '')
  if (url.startsWith('https://')) {
    return url.replace('https://', 'wss://') + '/api/crdt'
  }
  return url.replace('http://', 'ws://') + '/api/crdt'
}

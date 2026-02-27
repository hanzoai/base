/**
 * React hooks for Hanzo Base.
 *
 * All hooks use the BaseClient from the nearest BaseProvider context.
 * They manage subscriptions, cleanup, and state transitions correctly
 * with React 19 patterns (useEffect, useState, useCallback, useRef).
 */

import {
  useState,
  useEffect,
  useCallback,
  useRef,
} from 'react'

import { useBase } from './context.js'
import type { BaseRecord } from '../core/state.js'
import type { ListOptions, AuthStore } from '../core/client.js'
import type { RealtimeEvent, ConnectionState } from '../core/realtime.js'
import type { CRDTDocument } from '../crdt/document.js'
import type { CRDTText } from '../crdt/text.js'
import type { CRDTCounter } from '../crdt/counter.js'
import type { CRDTSet } from '../crdt/set.js'
import type { CRDTRegister } from '../crdt/register.js'
import type { PeerState, SyncState } from '../crdt/sync.js'
import { CRDTSync } from '../crdt/sync.js'
import { CRDTDocument as CRDTDocumentImpl } from '../crdt/document.js'

// ---------------------------------------------------------------------------
// useQuery -- reactive collection query with SSE subscription
// ---------------------------------------------------------------------------

export interface UseQueryOptions extends ListOptions {
  /** Whether to auto-subscribe to realtime updates. Default: true. */
  realtime?: boolean
  /** Whether to run the query immediately. Default: true. */
  enabled?: boolean
}

export interface UseQueryResult<T = BaseRecord> {
  data: T[]
  isLoading: boolean
  error: Error | null
  /** Manually refetch the query. */
  refetch: () => Promise<void>
}

/**
 * Reactive query hook.
 *
 * Fetches records from a collection and subscribes to realtime updates.
 * Results are cached in the QueryStore for deduplication across components.
 */
export function useQuery<T extends BaseRecord = BaseRecord>(
  collection: string,
  options?: UseQueryOptions,
): UseQueryResult<T> {
  const client = useBase()
  const [data, setData] = useState<T[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Stabilize options reference.
  const filter = options?.filter ?? ''
  const sort = options?.sort
  const expand = options?.expand
  const fields = options?.fields
  const page = options?.page
  const perPage = options?.perPage
  const realtimeEnabled = options?.realtime !== false
  const enabled = options?.enabled !== false

  const fetchData = useCallback(async () => {
    if (!enabled) return
    setIsLoading(true)
    setError(null)
    try {
      const result = await client.list(collection, {
        filter, sort, expand, fields, page, perPage,
      })
      setData(result.items as T[])
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setIsLoading(false)
    }
  }, [client, collection, filter, sort, expand, fields, page, perPage, enabled])

  // Initial fetch.
  useEffect(() => {
    fetchData()
  }, [fetchData])

  // Subscribe to QueryStore for optimistic + server updates.
  useEffect(() => {
    if (!enabled) return

    const unsubscribe = client.store.subscribe(collection, filter, (records) => {
      setData(records as T[])
    })

    return unsubscribe
  }, [client, collection, filter, enabled])

  // SSE realtime subscription.
  useEffect(() => {
    if (!realtimeEnabled || !enabled) return

    const unsubscribe = client.subscribeAndSync(collection, '*')
    return unsubscribe
  }, [client, collection, realtimeEnabled, enabled])

  return { data, isLoading, error, refetch: fetchData }
}

// ---------------------------------------------------------------------------
// useMutation -- mutation with optimistic update support
// ---------------------------------------------------------------------------

export type MutationAction = 'create' | 'update' | 'delete'

export interface MutateOptions {
  /** If true, apply an optimistic update to the QueryStore before the server responds. */
  optimistic?: boolean
}

export interface UseMutationResult {
  mutate: (data: Record<string, unknown>, options?: MutateOptions) => Promise<BaseRecord | void>
  isLoading: boolean
  error: Error | null
}

/**
 * Mutation hook.
 *
 * Performs a create, update, or delete mutation against a collection.
 * Supports optimistic updates that are automatically rolled back on failure.
 */
export function useMutation(
  collection: string,
  action: MutationAction,
): UseMutationResult {
  const client = useBase()
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)

  const mutate = useCallback(
    async (data: Record<string, unknown>, opts?: MutateOptions): Promise<BaseRecord | void> => {
      setIsLoading(true)
      setError(null)

      let optimisticId: string | undefined

      try {
        // Optimistic update.
        if (opts?.optimistic && action !== 'delete') {
          const tempRecord: BaseRecord = {
            id: data.id as string ?? `_temp_${Date.now()}`,
            ...data,
          }
          optimisticId = client.store.optimisticSet(collection, tempRecord)
        } else if (opts?.optimistic && action === 'delete' && data.id) {
          optimisticId = client.store.optimisticDelete(collection, data.id as string)
        }

        // Server request.
        let result: BaseRecord | void = undefined
        switch (action) {
          case 'create':
            result = await client.create(collection, data)
            break
          case 'update': {
            const id = data.id as string
            if (!id) throw new Error('useMutation: update requires data.id')
            const { id: _id, ...rest } = data
            result = await client.update(collection, id, rest)
            break
          }
          case 'delete': {
            const id = data.id as string
            if (!id) throw new Error('useMutation: delete requires data.id')
            await client.delete(collection, id)
            break
          }
        }

        // On success, remove optimistic entry (server truth takes over via applyServerUpdate).
        if (optimisticId) {
          client.store.rollbackOptimistic(optimisticId)
        }

        return result
      } catch (err) {
        // Rollback optimistic on failure.
        if (optimisticId) {
          client.store.rollbackOptimistic(optimisticId)
        }
        const e = err instanceof Error ? err : new Error(String(err))
        setError(e)
        throw e
      } finally {
        setIsLoading(false)
      }
    },
    [client, collection, action],
  )

  return { mutate, isLoading, error }
}

// ---------------------------------------------------------------------------
// useAuth -- auth state
// ---------------------------------------------------------------------------

export interface UseAuthResult {
  user: BaseRecord | null
  token: string
  isValid: boolean
  signIn: (identity: string, password: string) => Promise<void>
  signUp: (data: Record<string, unknown>) => Promise<BaseRecord>
  signOut: () => void
  /** Subscribe to auth changes. Returns unsubscribe. */
  onChange: AuthStore['onChange']
}

/**
 * Auth state hook.
 *
 * Returns the current auth state and provides sign-in, sign-up, and
 * sign-out methods. Re-renders when auth state changes.
 *
 * @param collection - Auth collection name (default: "users").
 */
export function useAuth(collection = 'users'): UseAuthResult {
  const client = useBase()
  const [user, setUser] = useState<BaseRecord | null>(client.authStore.record)
  const [token, setToken] = useState(client.authStore.token)

  useEffect(() => {
    const unsubscribe = client.authStore.onChange((newToken, newRecord) => {
      setToken(newToken)
      setUser(newRecord)
    })
    return unsubscribe
  }, [client])

  const signIn = useCallback(
    async (identity: string, password: string) => {
      await client.signInWithPassword(collection, identity, password)
    },
    [client, collection],
  )

  const signUp = useCallback(
    async (data: Record<string, unknown>) => {
      return client.signUp(collection, data)
    },
    [client, collection],
  )

  const signOut = useCallback(() => {
    client.signOut()
  }, [client])

  return {
    user,
    token,
    isValid: client.authStore.isValid,
    signIn,
    signUp,
    signOut,
    onChange: client.authStore.onChange.bind(client.authStore),
  }
}

// ---------------------------------------------------------------------------
// useRealtime -- low-level SSE subscription
// ---------------------------------------------------------------------------

/**
 * Low-level realtime subscription hook.
 *
 * Subscribes to SSE events for a collection topic on mount and
 * unsubscribes on unmount.
 */
export function useRealtime(
  collection: string,
  topic: string,
  callback: (event: RealtimeEvent) => void,
): void {
  const client = useBase()
  const callbackRef = useRef(callback)
  callbackRef.current = callback

  useEffect(() => {
    const unsubscribe = client.realtime.subscribe(collection, topic, (event) => {
      callbackRef.current(event)
    })
    return unsubscribe
  }, [client, collection, topic])
}

// ---------------------------------------------------------------------------
// useConnectionState -- realtime connection state
// ---------------------------------------------------------------------------

export function useConnectionState(): ConnectionState {
  const client = useBase()
  const [state, setState] = useState<ConnectionState>(client.realtime.state)

  useEffect(() => {
    const unsubscribe = client.realtime.onConnectionChange((s) => {
      setState(s)
    })
    return unsubscribe
  }, [client])

  return state
}

// ---------------------------------------------------------------------------
// usePresence -- presence tracking via CRDT sync
// ---------------------------------------------------------------------------

export interface UsePresenceResult {
  /** Map of peer siteId to their state. */
  peers: Map<string, PeerState>
  /** Our own presence metadata. */
  myState: Record<string, unknown>
  /** Update our presence metadata. */
  updateState: (meta: Record<string, unknown>) => void
}

/**
 * Presence tracking hook.
 *
 * Requires a CRDTSync instance (usually obtained from useCRDT).
 * Tracks peers connected to the same document.
 */
export function usePresence(sync: CRDTSync): UsePresenceResult {
  const [peers, setPeers] = useState<Map<string, PeerState>>(new Map())
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

// ---------------------------------------------------------------------------
// useCRDT -- CRDT document hook with auto-sync
// ---------------------------------------------------------------------------

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
  /** The CRDTSync instance for advanced usage (presence, state listeners). */
  sync: CRDTSync
  /** Current sync state. */
  syncState: SyncState
}

/**
 * CRDT document hook.
 *
 * Creates a CRDTDocument and CRDTSync, connects to the server via
 * WebSocket, and auto-syncs all local operations.
 *
 * @param documentId - Unique document identifier.
 * @param wsUrl - WebSocket URL for the CRDT sync endpoint. If not provided,
 *                derives it from the BaseClient URL (http->ws, https->wss).
 */
export function useCRDT(documentId: string, wsUrl?: string): UseCRDTResult {
  const client = useBase()

  // Stable refs for document and sync -- created once per documentId.
  const docRef = useRef<CRDTDocument | null>(null)
  const syncRef = useRef<CRDTSync | null>(null)
  const [syncState, setSyncState] = useState<SyncState>('disconnected')

  // Create document and sync on first render or documentId change.
  if (!docRef.current || docRef.current.id !== documentId) {
    docRef.current = new CRDTDocumentImpl(documentId)
  }
  if (!syncRef.current) {
    syncRef.current = new CRDTSync()
  }

  const doc = docRef.current
  const sync = syncRef.current

  // Connect on mount, disconnect on unmount.
  useEffect(() => {
    const url = wsUrl ?? deriveCrdtWsUrl(client.url)
    const token = client.authStore.token || undefined

    sync.connect(url, doc, token)

    const unsubState = sync.onStateChange((s) => {
      setSyncState(s)
    })

    return () => {
      unsubState()
      sync.disconnect()
    }
  }, [doc, sync, client, wsUrl])

  // Convenience accessors that delegate to the document.
  const text = useCallback((field: string) => doc.getText(field), [doc])
  const counter = useCallback((field: string) => doc.getCounter(field), [doc])
  const set = useCallback(<T = unknown>(field: string) => doc.getSet<T>(field), [doc])
  const register = useCallback(<T = unknown>(field: string) => doc.getRegister<T>(field), [doc])

  return { doc, text, counter, set, register, sync, syncState }
}

// ---------------------------------------------------------------------------
// useCRDTText -- subscribe to a CRDT text field's content
// ---------------------------------------------------------------------------

/**
 * Subscribe to a specific CRDTText field's current string value.
 * Re-renders whenever the text changes (local or remote).
 */
export function useCRDTText(doc: CRDTDocument, field: string): string {
  const textCrdt = doc.getText(field)
  const [value, setValue] = useState(() => textCrdt.toString())

  useEffect(() => {
    const unsubscribe = textCrdt.onChange((text) => {
      setValue(text)
    })
    return unsubscribe
  }, [textCrdt])

  return value
}

// ---------------------------------------------------------------------------
// useCRDTCounter -- subscribe to a CRDT counter field
// ---------------------------------------------------------------------------

export function useCRDTCounter(doc: CRDTDocument, field: string): number {
  const counterCrdt = doc.getCounter(field)
  const [value, setValue] = useState(() => counterCrdt.value)

  useEffect(() => {
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

/** Derive WebSocket URL from HTTP URL: https://x -> wss://x/api/crdt */
function deriveCrdtWsUrl(httpUrl: string): string {
  const url = httpUrl.replace(/\/$/, '')
  if (url.startsWith('https://')) {
    return url.replace('https://', 'wss://') + '/api/crdt'
  }
  return url.replace('http://', 'ws://') + '/api/crdt'
}

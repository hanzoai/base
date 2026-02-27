/**
 * RealtimeService -- SSE-based subscription manager.
 *
 * Features:
 * - Query deduplication via hash(collection+topic) with reference counting
 * - Auto-reconnect with exponential backoff
 * - Max observed timestamp tracking
 * - Connection state notifications
 */

import type { BaseRecord } from './state.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ConnectionState = 'disconnected' | 'connecting' | 'connected'

export interface RealtimeEvent {
  action: 'create' | 'update' | 'delete'
  record: BaseRecord
}

export type RealtimeCallback = (event: RealtimeEvent) => void
export type ConnectionCallback = (state: ConnectionState) => void

interface Subscription {
  collection: string
  topic: string
  callbacks: Set<RealtimeCallback>
}

// ---------------------------------------------------------------------------
// RealtimeService
// ---------------------------------------------------------------------------

export class RealtimeService {
  private readonly _baseUrl: string
  private readonly _getToken: () => string

  private _eventSource: EventSource | null = null
  private _state: ConnectionState = 'disconnected'
  private _connectionListeners = new Set<ConnectionCallback>()

  /** Dedup map: hash -> Subscription. */
  private _subscriptions = new Map<string, Subscription>()

  /** SSE client id assigned by the server on connect. */
  private _clientId: string | null = null

  /** Reconnect state. */
  private _reconnectAttempts = 0
  private _reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private _maxReconnectDelay = 30_000
  private _baseReconnectDelay = 500

  /** High-water mark of observed server timestamps. */
  private _maxTimestamp = 0n

  /** Set to true when disconnect() is called explicitly. */
  private _intentionalDisconnect = false

  constructor(baseUrl: string, getToken: () => string) {
    this._baseUrl = baseUrl.replace(/\/$/, '')
    this._getToken = getToken
  }

  // ---- Public API ---------------------------------------------------------

  get state(): ConnectionState {
    return this._state
  }

  get maxTimestamp(): bigint {
    return this._maxTimestamp
  }

  /**
   * Subscribe to realtime events for a collection topic.
   *
   * Topic is usually "*" (all changes) or a record id.
   * Returns an unsubscribe function.
   */
  subscribe(
    collection: string,
    topic: string,
    callback: RealtimeCallback,
  ): () => void {
    const hash = this._hash(collection, topic)
    let sub = this._subscriptions.get(hash)

    if (!sub) {
      sub = { collection, topic, callbacks: new Set() }
      this._subscriptions.set(hash, sub)
    }
    sub.callbacks.add(callback)

    // If this is the first subscription, connect.
    if (this._subscriptions.size === 1 && !this._eventSource) {
      this._connect()
    } else if (this._clientId) {
      // Already connected -- submit this subscription to the server.
      this._submitSubscriptions()
    }

    return () => {
      sub!.callbacks.delete(callback)
      if (sub!.callbacks.size === 0) {
        this._subscriptions.delete(hash)
        if (this._clientId) {
          this._submitSubscriptions()
        }
      }
      // If no subscriptions remain, disconnect.
      if (this._subscriptions.size === 0) {
        this.disconnect()
      }
    }
  }

  /** Register a connection-state listener. Returns unsubscribe. */
  onConnectionChange(callback: ConnectionCallback): () => void {
    this._connectionListeners.add(callback)
    return () => {
      this._connectionListeners.delete(callback)
    }
  }

  /** Explicitly disconnect. */
  disconnect(): void {
    this._intentionalDisconnect = true
    this._clearReconnect()
    if (this._eventSource) {
      this._eventSource.close()
      this._eventSource = null
    }
    this._clientId = null
    this._setState('disconnected')
  }

  // ---- Connection ---------------------------------------------------------

  private _connect(): void {
    if (this._eventSource) return
    this._intentionalDisconnect = false
    this._setState('connecting')

    const url = `${this._baseUrl}/api/realtime`
    this._eventSource = new EventSource(url)

    this._eventSource.addEventListener('PB_CONNECT', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as { clientId: string }
        this._clientId = data.clientId
        this._reconnectAttempts = 0
        this._setState('connected')
        this._submitSubscriptions()
      } catch {
        // malformed connect event
      }
    })

    // Listen for all SSE events. The server sends events named after
    // the collection, e.g. event: "posts".
    this._eventSource.onmessage = (e: MessageEvent) => {
      this._handleMessage(e)
    }

    // The server sends named events for each collection.
    // We use the generic handler plus specific ones registered dynamically.
    this._eventSource.onerror = () => {
      this._eventSource?.close()
      this._eventSource = null
      this._clientId = null
      this._setState('disconnected')

      if (!this._intentionalDisconnect) {
        this._scheduleReconnect()
      }
    }
  }

  private _handleMessage(e: MessageEvent): void {
    let payload: { action: string; record: BaseRecord }
    try {
      payload = JSON.parse(e.data)
    } catch {
      return
    }

    const action = payload.action as RealtimeEvent['action']
    const record = payload.record
    if (!record?.id) return

    // Track timestamp high-water mark.
    const ts = record.updated ?? record.created
    if (ts) {
      const n = BigInt(new Date(ts).getTime()) * 1000n
      if (n > this._maxTimestamp) {
        this._maxTimestamp = n
      }
    }

    // Fan out to matching subscriptions.
    const collectionName = record.collectionName ?? ''
    for (const sub of this._subscriptions.values()) {
      if (sub.collection !== collectionName) continue
      if (sub.topic !== '*' && sub.topic !== record.id) continue
      const event: RealtimeEvent = { action, record }
      for (const cb of sub.callbacks) {
        try {
          cb(event)
        } catch {
          // subscriber errors must not break fanout
        }
      }
    }
  }

  /** Submit current subscription set to the server via POST. */
  private async _submitSubscriptions(): Promise<void> {
    if (!this._clientId) return

    const topics: string[] = []
    for (const sub of this._subscriptions.values()) {
      topics.push(`${sub.collection}/${sub.topic}`)
    }

    const token = this._getToken()
    try {
      await fetch(`${this._baseUrl}/api/realtime`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: token } : {}),
        },
        body: JSON.stringify({
          clientId: this._clientId,
          subscriptions: topics,
        }),
      })
    } catch {
      // will retry on next reconnect
    }
  }

  // ---- Reconnect ----------------------------------------------------------

  private _scheduleReconnect(): void {
    this._clearReconnect()
    const delay = Math.min(
      this._baseReconnectDelay * Math.pow(2, this._reconnectAttempts),
      this._maxReconnectDelay,
    )
    // Add jitter: +/- 25%
    const jitter = delay * (0.75 + Math.random() * 0.5)
    this._reconnectAttempts++

    this._reconnectTimer = setTimeout(() => {
      this._reconnectTimer = null
      this._connect()
    }, jitter)
  }

  private _clearReconnect(): void {
    if (this._reconnectTimer !== null) {
      clearTimeout(this._reconnectTimer)
      this._reconnectTimer = null
    }
  }

  // ---- State --------------------------------------------------------------

  private _setState(state: ConnectionState): void {
    if (this._state === state) return
    this._state = state
    for (const cb of this._connectionListeners) {
      try {
        cb(state)
      } catch {
        // listener errors must not break notification
      }
    }
  }

  // ---- Helpers ------------------------------------------------------------

  private _hash(collection: string, topic: string): string {
    return `${collection}::${topic}`
  }
}

/**
 * CRDTSync -- WebSocket-based sync protocol handler.
 *
 * Connects to the Base server's CRDT sync endpoint and manages
 * bidirectional operation exchange. Handles:
 *
 * - Buffering and batching local operations
 * - Applying remote operations to the CRDTDocument
 * - Reconnection with exponential backoff
 * - Acknowledgment tracking
 * - Presence (peer awareness) via the same channel
 */

import type { CRDTDocument } from './document.js'
import type { Operation } from './operations.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type SyncState = 'disconnected' | 'connecting' | 'connected' | 'syncing'

export interface PeerState {
  siteId: string
  /** Arbitrary JSON-serializable state (cursor position, name, color, etc.). */
  meta: Record<string, unknown>
  /** Last seen timestamp (local clock). */
  lastSeen: number
}

export type SyncStateCallback = (state: SyncState) => void
export type PeerCallback = (peers: Map<string, PeerState>) => void
export type RemoteChangeCallback = (ops: Operation[]) => void

/** Wire message types sent over WebSocket. */
interface SyncMessage {
  type: 'ops' | 'ack' | 'presence' | 'state_request' | 'state_response'
  documentId: string
  siteId: string
  payload: unknown
}

interface OpsPayload {
  ops: Operation[]
}

interface AckPayload {
  lastOpId: string
}

interface PresencePayload {
  meta: Record<string, unknown>
}

// ---------------------------------------------------------------------------
// CRDTSync
// ---------------------------------------------------------------------------

export class CRDTSync {
  private _ws: WebSocket | null = null
  private _state: SyncState = 'disconnected'
  private _document: CRDTDocument | null = null
  private _wsUrl: string | null = null

  /** Flush interval for batching local ops. */
  private _flushTimer: ReturnType<typeof setInterval> | null = null
  private _flushIntervalMs = 50

  /** Reconnection. */
  private _reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private _reconnectAttempts = 0
  private _maxReconnectDelay = 30_000
  private _baseReconnectDelay = 500
  private _intentionalClose = false

  /** Presence. */
  private _peers = new Map<string, PeerState>()
  private _localPresence: Record<string, unknown> = {}
  private _presenceTimer: ReturnType<typeof setInterval> | null = null
  private _presenceIntervalMs = 2_000
  private _peerTimeoutMs = 10_000

  /** Listeners. */
  private _stateListeners = new Set<SyncStateCallback>()
  private _peerListeners = new Set<PeerCallback>()
  private _remoteChangeListeners = new Set<RemoteChangeCallback>()

  // ---- Public API ---------------------------------------------------------

  get state(): SyncState {
    return this._state
  }

  get peers(): ReadonlyMap<string, PeerState> {
    return this._peers
  }

  /**
   * Connect to the CRDT sync endpoint and start syncing a document.
   *
   * @param wsUrl - WebSocket URL, e.g. "wss://myapp.hanzo.ai/api/crdt"
   * @param document - The CRDTDocument to sync.
   * @param token - Optional auth token.
   */
  connect(wsUrl: string, document: CRDTDocument, token?: string): void {
    if (this._ws) {
      this.disconnect()
    }

    this._wsUrl = wsUrl
    this._document = document
    this._intentionalClose = false
    this._setState('connecting')

    const url = new URL(wsUrl)
    url.searchParams.set('documentId', document.id)
    url.searchParams.set('siteId', document.clock.siteId)
    if (token) {
      url.searchParams.set('token', token)
    }

    this._ws = new WebSocket(url.toString())
    this._ws.binaryType = 'arraybuffer'

    this._ws.onopen = () => {
      this._reconnectAttempts = 0
      this._setState('connected')

      // Request full state from server if we have no unsynced ops.
      if (document.getUnsyncedOps().length === 0) {
        this._send({
          type: 'state_request',
          documentId: document.id,
          siteId: document.clock.siteId,
          payload: {},
        })
      } else {
        // Re-send unsynced ops.
        this._sendOps(Array.from(document.getUnsyncedOps()))
      }

      this._startFlush()
      this._startPresence()
    }

    this._ws.onmessage = (e: MessageEvent) => {
      this._handleMessage(e.data)
    }

    this._ws.onclose = () => {
      this._stopFlush()
      this._stopPresence()
      this._ws = null
      this._setState('disconnected')

      if (!this._intentionalClose) {
        this._scheduleReconnect()
      }
    }

    this._ws.onerror = () => {
      // The close event will fire next -- handle reconnection there.
    }
  }

  disconnect(): void {
    this._intentionalClose = true
    this._stopFlush()
    this._stopPresence()
    this._clearReconnect()

    if (this._ws) {
      this._ws.close()
      this._ws = null
    }

    this._peers.clear()
    this._setState('disconnected')
  }

  /** Update local presence metadata (cursor, name, etc.). */
  updatePresence(meta: Record<string, unknown>): void {
    this._localPresence = meta
    this._broadcastPresence()
  }

  // ---- Listeners ----------------------------------------------------------

  onStateChange(cb: SyncStateCallback): () => void {
    this._stateListeners.add(cb)
    return () => { this._stateListeners.delete(cb) }
  }

  onPeersChange(cb: PeerCallback): () => void {
    this._peerListeners.add(cb)
    return () => { this._peerListeners.delete(cb) }
  }

  onRemoteChange(cb: RemoteChangeCallback): () => void {
    this._remoteChangeListeners.add(cb)
    return () => { this._remoteChangeListeners.delete(cb) }
  }

  // ---- Message handling ---------------------------------------------------

  private _handleMessage(raw: string | ArrayBuffer): void {
    let text: string
    if (raw instanceof ArrayBuffer) {
      text = new TextDecoder().decode(raw)
    } else {
      text = raw as string
    }

    let msg: SyncMessage
    try {
      msg = JSON.parse(text)
    } catch {
      return
    }

    if (!this._document || msg.documentId !== this._document.id) return

    switch (msg.type) {
      case 'ops': {
        const payload = msg.payload as OpsPayload
        if (!payload.ops || payload.ops.length === 0) return

        // Skip our own operations echoed back.
        const remoteOps = payload.ops.filter(
          (op) => op.hlc.siteId !== this._document!.clock.siteId,
        )

        if (remoteOps.length > 0) {
          this._document.applyRemoteOps(remoteOps)
          for (const cb of this._remoteChangeListeners) {
            try {
              cb(remoteOps)
            } catch {
              // listener errors must not break processing
            }
          }
        }
        break
      }

      case 'ack': {
        const payload = msg.payload as AckPayload
        this._document.acknowledge(payload.lastOpId)
        if (this._state === 'syncing' && this._document.getUnsyncedOps().length === 0) {
          this._setState('connected')
        }
        break
      }

      case 'presence': {
        const payload = msg.payload as PresencePayload
        if (msg.siteId === this._document.clock.siteId) return
        this._peers.set(msg.siteId, {
          siteId: msg.siteId,
          meta: payload.meta,
          lastSeen: Date.now(),
        })
        this._notifyPeers()
        break
      }

      case 'state_response': {
        // Full state from server -- apply as remote ops.
        const payload = msg.payload as OpsPayload
        if (payload.ops && payload.ops.length > 0) {
          this._document.applyRemoteOps(payload.ops)
        }
        break
      }
    }
  }

  // ---- Flush (batched sending of local ops) -------------------------------

  private _startFlush(): void {
    this._stopFlush()
    this._flushTimer = setInterval(() => {
      this._flush()
    }, this._flushIntervalMs)
  }

  private _stopFlush(): void {
    if (this._flushTimer !== null) {
      clearInterval(this._flushTimer)
      this._flushTimer = null
    }
  }

  private _flush(): void {
    if (!this._document || !this._ws || this._ws.readyState !== WebSocket.OPEN) return

    const ops = this._document.collectOps()
    if (ops.length === 0) return

    this._sendOps(ops)
    this._setState('syncing')
  }

  private _sendOps(ops: Operation[]): void {
    if (!this._document || !this._ws || this._ws.readyState !== WebSocket.OPEN) return

    this._send({
      type: 'ops',
      documentId: this._document.id,
      siteId: this._document.clock.siteId,
      payload: { ops } satisfies OpsPayload,
    })
  }

  // ---- Presence -----------------------------------------------------------

  private _startPresence(): void {
    this._stopPresence()
    this._broadcastPresence()
    this._presenceTimer = setInterval(() => {
      this._broadcastPresence()
      this._pruneStalePresence()
    }, this._presenceIntervalMs)
  }

  private _stopPresence(): void {
    if (this._presenceTimer !== null) {
      clearInterval(this._presenceTimer)
      this._presenceTimer = null
    }
  }

  private _broadcastPresence(): void {
    if (!this._document || !this._ws || this._ws.readyState !== WebSocket.OPEN) return

    this._send({
      type: 'presence',
      documentId: this._document.id,
      siteId: this._document.clock.siteId,
      payload: { meta: this._localPresence } satisfies PresencePayload,
    })
  }

  private _pruneStalePresence(): void {
    const now = Date.now()
    let changed = false
    for (const [siteId, peer] of this._peers) {
      if (now - peer.lastSeen > this._peerTimeoutMs) {
        this._peers.delete(siteId)
        changed = true
      }
    }
    if (changed) {
      this._notifyPeers()
    }
  }

  // ---- Reconnection -------------------------------------------------------

  private _scheduleReconnect(): void {
    this._clearReconnect()
    const delay = Math.min(
      this._baseReconnectDelay * Math.pow(2, this._reconnectAttempts),
      this._maxReconnectDelay,
    )
    const jitter = delay * (0.75 + Math.random() * 0.5)
    this._reconnectAttempts++

    this._reconnectTimer = setTimeout(() => {
      this._reconnectTimer = null
      if (this._document && this._wsUrl) {
        this.connect(this._wsUrl, this._document)
      }
    }, jitter)
  }

  private _clearReconnect(): void {
    if (this._reconnectTimer !== null) {
      clearTimeout(this._reconnectTimer)
      this._reconnectTimer = null
    }
  }

  // ---- Internal helpers ---------------------------------------------------

  private _send(msg: SyncMessage): void {
    if (!this._ws || this._ws.readyState !== WebSocket.OPEN) return
    this._ws.send(JSON.stringify(msg))
  }

  private _setState(state: SyncState): void {
    if (this._state === state) return
    this._state = state
    for (const cb of this._stateListeners) {
      try {
        cb(state)
      } catch {
        // listener errors must not break state management
      }
    }
  }

  private _notifyPeers(): void {
    for (const cb of this._peerListeners) {
      try {
        cb(this._peers)
      } catch {
        // listener errors must not break peer notification
      }
    }
  }
}

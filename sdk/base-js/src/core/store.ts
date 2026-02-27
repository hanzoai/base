/**
 * QueryStore -- manages server state + optimistic overlay.
 *
 * Subscribers are notified whenever the effective (server + optimistic)
 * state for their query changes.  Optimistic mutations are tracked by
 * mutationId so they can be rolled back individually.
 */

import type { BaseRecord, Modification } from './state.js'
import { VersionTracker } from './state.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface QueryKey {
  collection: string
  filter: string
}

export type StoreCallback = (records: BaseRecord[]) => void

interface OptimisticEntry {
  mutationId: string
  collection: string
  /** null means "delete this id" */
  record: BaseRecord | null
  deletedId?: string
  createdAt: number
}

interface QuerySlot {
  key: QueryKey
  serverRecords: BaseRecord[]
  listeners: Set<StoreCallback>
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function queryHash(collection: string, filter: string): string {
  return `${collection}::${filter}`
}

function mergeOptimistic(
  server: BaseRecord[],
  optimistic: OptimisticEntry[],
  collection: string,
): BaseRecord[] {
  // Start with a mutable copy keyed by id.
  const map = new Map<string, BaseRecord>()
  for (const r of server) {
    map.set(r.id, r)
  }

  for (const entry of optimistic) {
    if (entry.collection !== collection) continue
    if (entry.record === null && entry.deletedId) {
      map.delete(entry.deletedId)
    } else if (entry.record) {
      map.set(entry.record.id, entry.record)
    }
  }

  return Array.from(map.values())
}

// ---------------------------------------------------------------------------
// QueryStore
// ---------------------------------------------------------------------------

export class QueryStore {
  private readonly _slots = new Map<string, QuerySlot>()
  private readonly _optimistic: OptimisticEntry[] = []
  private readonly _version = new VersionTracker()

  get version() {
    return this._version.current
  }

  // ---- Query cache --------------------------------------------------------

  /** Return cached effective (server+optimistic) result or undefined. */
  getQuery(collection: string, filter = ''): BaseRecord[] | undefined {
    const slot = this._slots.get(queryHash(collection, filter))
    if (!slot) return undefined
    return mergeOptimistic(slot.serverRecords, this._optimistic, collection)
  }

  /** Overwrite the server-truth cache for a query and notify. */
  setQuery(collection: string, filter: string, data: BaseRecord[]): void {
    const hash = queryHash(collection, filter)
    let slot = this._slots.get(hash)
    if (!slot) {
      slot = { key: { collection, filter }, serverRecords: [], listeners: new Set() }
      this._slots.set(hash, slot)
    }
    slot.serverRecords = data

    this._version.advance(
      data.map((r) => ({ type: 'QueryUpdated' as const, collection, record: r })),
    )

    this._notify(slot)
  }

  // ---- Optimistic mutations -----------------------------------------------

  /** Apply an optimistic create/update. Returns a mutationId for rollback. */
  optimisticSet(collection: string, record: BaseRecord): string {
    const mutationId = `opt_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`
    this._optimistic.push({
      mutationId,
      collection,
      record,
      createdAt: Date.now(),
    })
    this._notifyCollection(collection)
    return mutationId
  }

  /** Apply an optimistic delete. */
  optimisticDelete(collection: string, id: string): string {
    const mutationId = `opt_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`
    this._optimistic.push({
      mutationId,
      collection,
      record: null,
      deletedId: id,
      createdAt: Date.now(),
    })
    this._notifyCollection(collection)
    return mutationId
  }

  /** Remove a single optimistic mutation and re-derive. */
  rollbackOptimistic(mutationId: string): void {
    const idx = this._optimistic.findIndex((e) => e.mutationId === mutationId)
    if (idx === -1) return
    const entry = this._optimistic[idx]
    this._optimistic.splice(idx, 1)
    this._notifyCollection(entry.collection)
  }

  /** Drop all optimistic entries for a collection. */
  clearOptimistic(collection: string): void {
    for (let i = this._optimistic.length - 1; i >= 0; i--) {
      if (this._optimistic[i].collection === collection) {
        this._optimistic.splice(i, 1)
      }
    }
    this._notifyCollection(collection)
  }

  // ---- Server event ingestion ---------------------------------------------

  /**
   * Apply a realtime SSE event from the server.
   * `action` is one of "create", "update", "delete".
   */
  applyServerUpdate(
    collection: string,
    action: 'create' | 'update' | 'delete',
    record: BaseRecord,
  ): void {
    const mods: Modification[] = []

    for (const slot of this._slots.values()) {
      if (slot.key.collection !== collection) continue

      if (action === 'delete') {
        const before = slot.serverRecords.length
        slot.serverRecords = slot.serverRecords.filter((r) => r.id !== record.id)
        if (slot.serverRecords.length !== before) {
          mods.push({ type: 'QueryRemoved', collection, id: record.id })
        }
      } else {
        // create or update -- upsert
        const idx = slot.serverRecords.findIndex((r) => r.id === record.id)
        if (idx >= 0) {
          slot.serverRecords[idx] = record
        } else {
          slot.serverRecords.push(record)
        }
        mods.push({ type: 'QueryUpdated', collection, record })
      }
    }

    if (mods.length > 0) {
      const ts = record.updated
        ? BigInt(new Date(record.updated).getTime()) * 1000n
        : undefined
      this._version.advance(mods, ts)
    }

    this._notifyCollection(collection)
  }

  // ---- Subscriptions ------------------------------------------------------

  /** Subscribe to effective-state changes for a query. Returns unsubscribe. */
  subscribe(collection: string, filter: string, callback: StoreCallback): () => void {
    const hash = queryHash(collection, filter)
    let slot = this._slots.get(hash)
    if (!slot) {
      slot = { key: { collection, filter }, serverRecords: [], listeners: new Set() }
      this._slots.set(hash, slot)
    }
    slot.listeners.add(callback)
    return () => {
      slot!.listeners.delete(callback)
      // GC empty slots
      if (slot!.listeners.size === 0 && slot!.serverRecords.length === 0) {
        this._slots.delete(hash)
      }
    }
  }

  // ---- Internal -----------------------------------------------------------

  private _notify(slot: QuerySlot): void {
    if (slot.listeners.size === 0) return
    const effective = mergeOptimistic(slot.serverRecords, this._optimistic, slot.key.collection)
    for (const cb of slot.listeners) {
      try {
        cb(effective)
      } catch {
        // listener errors must not break the store
      }
    }
  }

  private _notifyCollection(collection: string): void {
    for (const slot of this._slots.values()) {
      if (slot.key.collection === collection) {
        this._notify(slot)
      }
    }
  }
}

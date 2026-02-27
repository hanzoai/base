/**
 * State version tracking (Convex-inspired).
 *
 * Each query result is tagged with a StateVersion so the client can
 * detect ordering, replay transitions, and drive optimistic rollbacks.
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface StateVersion {
  /** Monotonically increasing counter bumped on every query-set change. */
  querySet: number
  /** Server timestamp (microseconds since epoch) of the latest known event. */
  ts: bigint
  /** Identity hash -- changes when the authenticated user changes. */
  identity: number
}

export type Modification =
  | { type: 'QueryUpdated'; collection: string; record: BaseRecord }
  | { type: 'QueryRemoved'; collection: string; id: string }
  | { type: 'QueryFailed'; collection: string; error: string }

export interface Transition {
  startVersion: StateVersion
  endVersion: StateVersion
  modifications: Modification[]
}

/** Minimal record shape that every Base record satisfies. */
export interface BaseRecord {
  id: string
  collectionId?: string
  collectionName?: string
  created?: string
  updated?: string
  [key: string]: unknown
}

// ---------------------------------------------------------------------------
// VersionTracker
// ---------------------------------------------------------------------------

export class VersionTracker {
  private _version: StateVersion
  private _history: Transition[] = []
  private readonly _maxHistory: number

  constructor(maxHistory = 128) {
    this._version = { querySet: 0, ts: 0n, identity: 0 }
    this._maxHistory = maxHistory
  }

  get current(): Readonly<StateVersion> {
    return { ...this._version }
  }

  get history(): readonly Transition[] {
    return this._history
  }

  /**
   * Advance the version and record a transition.
   * Returns the new version.
   */
  advance(modifications: Modification[], serverTs?: bigint): StateVersion {
    const start = { ...this._version }

    this._version = {
      querySet: this._version.querySet + 1,
      ts: serverTs ?? this._version.ts,
      identity: this._version.identity,
    }

    const transition: Transition = {
      startVersion: start,
      endVersion: { ...this._version },
      modifications,
    }

    this._history.push(transition)
    if (this._history.length > this._maxHistory) {
      this._history.shift()
    }

    return { ...this._version }
  }

  /** Update identity hash (e.g. on auth change). */
  setIdentity(identity: number): void {
    this._version = { ...this._version, identity }
  }

  /** Update the high-water timestamp without bumping querySet. */
  updateTimestamp(ts: bigint): void {
    if (ts > this._version.ts) {
      this._version = { ...this._version, ts }
    }
  }

  /** Simple FNV-1a-like hash for identity derivation. */
  static hashIdentity(token: string): number {
    let h = 0x811c9dc5
    for (let i = 0; i < token.length; i++) {
      h ^= token.charCodeAt(i)
      h = Math.imul(h, 0x01000193)
    }
    return h >>> 0
  }
}

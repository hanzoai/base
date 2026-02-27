/**
 * Hybrid logical clock for CRDT operation ordering.
 *
 * Each operation is tagged with (timestamp, counter, siteId) to provide
 * a total order across all sites. Compatible with the Go HLC implementation.
 */

export interface HLCTimestamp {
  /** Wall-clock milliseconds since epoch. */
  ts: number
  /** Monotonic counter to break ties within the same ms. */
  counter: number
  /** Unique site identifier (usually client session id). */
  siteId: string
}

export class HybridLogicalClock {
  private _ts: number
  private _counter: number
  readonly siteId: string

  constructor(siteId?: string) {
    this.siteId = siteId ?? randomSiteId()
    this._ts = Date.now()
    this._counter = 0
  }

  /** Generate a new timestamp, guaranteed > any previous. */
  now(): HLCTimestamp {
    const wall = Date.now()
    if (wall > this._ts) {
      this._ts = wall
      this._counter = 0
    } else {
      this._counter++
    }
    return { ts: this._ts, counter: this._counter, siteId: this.siteId }
  }

  /** Receive a remote timestamp and merge with local clock. */
  receive(remote: HLCTimestamp): HLCTimestamp {
    const wall = Date.now()
    if (wall > this._ts && wall > remote.ts) {
      this._ts = wall
      this._counter = 0
    } else if (remote.ts > this._ts) {
      this._ts = remote.ts
      this._counter = remote.counter + 1
    } else if (this._ts > remote.ts) {
      this._counter++
    } else {
      // Same ts -- take max counter + 1
      this._counter = Math.max(this._counter, remote.counter) + 1
    }
    return { ts: this._ts, counter: this._counter, siteId: this.siteId }
  }
}

/** Compare two HLC timestamps. Returns <0, 0, or >0. */
export function compareHLC(a: HLCTimestamp, b: HLCTimestamp): number {
  if (a.ts !== b.ts) return a.ts - b.ts
  if (a.counter !== b.counter) return a.counter - b.counter
  if (a.siteId < b.siteId) return -1
  if (a.siteId > b.siteId) return 1
  return 0
}

function randomSiteId(): string {
  const buf = new Uint8Array(8)
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(buf)
  } else {
    for (let i = 0; i < buf.length; i++) {
      buf[i] = (Math.random() * 256) | 0
    }
  }
  return Array.from(buf, (b) => b.toString(16).padStart(2, '0')).join('')
}

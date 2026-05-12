/**
 * Auth-store implementations beyond the in-memory default.
 *
 * Native `LocalAuthStore` / `AsyncAuthStore` implementations so the
 * SDK has zero upstream client dependency. The shape matches the
 * legacy client's auth stores so existing apps migrating to
 * `@hanzo/base` keep working.
 */

import type { AuthStore, AuthChangeCallback } from './client.js'
import type { BaseRecord } from './state.js'

interface PersistedAuth {
  token: string
  record: BaseRecord | null
}

/**
 * Type alias matching the upstream interface name. New code should use
 * `AuthStore`.
 */
export type BaseAuthStore = AuthStore

/**
 * Synchronous localStorage-backed auth store. Suitable for browser SPAs.
 * Falls back to in-memory storage when `window.localStorage` is absent
 * (SSR, sandboxed iframes, etc.).
 */
export class LocalAuthStore implements AuthStore {
  private _storageKey: string
  private _listeners = new Set<AuthChangeCallback>()
  private _memToken = ''
  private _memRecord: BaseRecord | null = null

  constructor(storageKey: string = 'base_auth') {
    this._storageKey = storageKey
  }

  private get _storage(): Storage | null {
    try {
      if (typeof globalThis !== 'undefined' && 'localStorage' in globalThis) {
        return (globalThis as { localStorage?: Storage }).localStorage ?? null
      }
    } catch {
      // Access can throw in some sandboxed contexts; fall through.
    }
    return null
  }

  private _read(): PersistedAuth {
    const storage = this._storage
    if (!storage) return { token: this._memToken, record: this._memRecord }
    try {
      const raw = storage.getItem(this._storageKey)
      if (!raw) return { token: '', record: null }
      const parsed = JSON.parse(raw) as Partial<PersistedAuth>
      return { token: parsed.token ?? '', record: parsed.record ?? null }
    } catch {
      return { token: '', record: null }
    }
  }

  private _write(value: PersistedAuth): void {
    const storage = this._storage
    if (!storage) {
      this._memToken = value.token
      this._memRecord = value.record
      return
    }
    try {
      if (!value.token) {
        storage.removeItem(this._storageKey)
      } else {
        storage.setItem(this._storageKey, JSON.stringify(value))
      }
    } catch {
      // Quota / serialization errors fall back to memory.
      this._memToken = value.token
      this._memRecord = value.record
    }
  }

  get token(): string {
    return this._read().token
  }

  get record(): BaseRecord | null {
    return this._read().record
  }

  get isValid(): boolean {
    const token = this.token
    if (!token) return false
    try {
      const parts = token.split('.')
      if (parts.length !== 3) return false
      const payload = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
      if (typeof payload.exp === 'number') return payload.exp > Date.now() / 1000
      return true
    } catch {
      return false
    }
  }

  save(token: string, record: BaseRecord | null): void {
    this._write({ token, record })
    this._notify(token, record)
  }

  clear(): void {
    this._write({ token: '', record: null })
    this._notify('', null)
  }

  onChange(callback: AuthChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notify(token: string, record: BaseRecord | null): void {
    for (const cb of this._listeners) {
      try {
        cb(token, record)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}

/**
 * Async auth store — wraps any async storage backend (cookies via
 * fetch, encrypted SecureStore on mobile, KV namespace on the edge,
 * etc.). Reads and writes are buffered through an in-memory cache so
 * the `token`/`record` accessors stay synchronous (matching upstream).
 *
 * Pass a `save` function that persists the serialized payload to your
 * backend, and an `initial` value loaded synchronously at app boot.
 */
export class AsyncAuthStore implements AuthStore {
  private _token = ''
  private _record: BaseRecord | null = null
  private _listeners = new Set<AuthChangeCallback>()
  private readonly _save: (serialized: string) => Promise<void> | void
  private readonly _clear?: () => Promise<void> | void

  constructor(config: {
    save: (serialized: string) => Promise<void> | void
    initial?: string | null
    clear?: () => Promise<void> | void
  }) {
    this._save = config.save
    this._clear = config.clear
    if (config.initial) {
      try {
        const parsed = JSON.parse(config.initial) as Partial<PersistedAuth>
        this._token = parsed.token ?? ''
        this._record = parsed.record ?? null
      } catch {
        // ignore malformed initial value
      }
    }
  }

  get token(): string {
    return this._token
  }

  get record(): BaseRecord | null {
    return this._record
  }

  get isValid(): boolean {
    if (!this._token) return false
    try {
      const parts = this._token.split('.')
      if (parts.length !== 3) return false
      const payload = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
      if (typeof payload.exp === 'number') return payload.exp > Date.now() / 1000
      return true
    } catch {
      return false
    }
  }

  save(token: string, record: BaseRecord | null): void {
    this._token = token
    this._record = record
    void this._save(JSON.stringify({ token, record }))
    this._notify()
  }

  clear(): void {
    this._token = ''
    this._record = null
    if (this._clear) {
      void this._clear()
    } else {
      void this._save('')
    }
    this._notify()
  }

  onChange(callback: AuthChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notify(): void {
    for (const cb of this._listeners) {
      try {
        cb(this._token, this._record)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}

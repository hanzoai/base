/**
 * @hanzoai/base -- Core entry point.
 *
 * Re-exports the client, collection, store, state, and realtime modules.
 */

// Client
export { BaseClient, BaseClientError, MemoryAuthStore, FileService } from './client.js'
export type { AuthStore, AuthChangeCallback, ClientConfig, ListOptions, ListResult } from './client.js'

// Collection
export { CollectionService, ClientResponseError } from './collection.js'
export type {
  RecordQueryOptions,
  RecordFullListOptions,
  FileOptions,
  AuthResponse,
  OAuth2Options,
  ClientResponseErrorData,
} from './collection.js'

// Store
export { QueryStore } from './store.js'
export type { QueryKey, StoreCallback } from './store.js'

// State
export { VersionTracker } from './state.js'
export type { StateVersion, Modification, Transition, BaseRecord } from './state.js'

// Realtime
export { RealtimeService } from './realtime.js'
export type {
  ConnectionState,
  RealtimeEvent,
  RealtimeCallback,
  ConnectionCallback,
} from './realtime.js'

/**
 * @hanzo/base -- Core entry point.
 *
 * Exposes the full Base client surface natively. No upstream package
 * dependency — every type and helper the SDK needs lives here.
 */

import { BaseClient } from './client.js'

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

// Schema types — admin UI consumers
export type { CollectionField, CollectionModel, RecordModel } from './types.js'

// Auth stores — beyond the in-memory default
export { LocalAuthStore, AsyncAuthStore } from './auth-stores.js'
export type { BaseAuthStore } from './auth-stores.js'

// Token + cookie helpers
export {
  getTokenPayload,
  isTokenExpired,
  cookieParse,
  cookieSerialize,
  normalizeUnknownQueryParams,
} from './tokens.js'
export type { CookieSerializeOptions } from './tokens.js'

// Default export — matches the upstream client default. Consumers can
// `import Base from '@hanzo/base'` and continue calling `new Base(url)`
// exactly as they did against the upstream package.
export default BaseClient

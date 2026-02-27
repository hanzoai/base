/**
 * @hanzoai/base/react -- React hooks and context for Hanzo Base.
 *
 * Provides:
 * - BaseProvider: context providing the BaseClient
 * - useBase: access the BaseClient instance
 * - useQuery: reactive collection query with SSE subscription
 * - useMutation: mutation with optimistic update support
 * - useAuth: auth state management
 * - useRealtime: low-level SSE subscription
 * - useConnectionState: realtime connection state
 * - usePresence: presence tracking via CRDT sync
 * - useCRDT: CRDT document with auto-sync
 * - useCRDTText: subscribe to a CRDT text field
 * - useCRDTCounter: subscribe to a CRDT counter field
 */

// Context & Provider
export { BaseProvider, useBase } from './context.js'
export type { BaseProviderProps } from './context.js'

// Hooks
export {
  useQuery,
  useMutation,
  useAuth,
  useRealtime,
  useConnectionState,
  usePresence,
  useCRDT,
  useCRDTText,
  useCRDTCounter,
} from './hooks.js'
export type {
  UseQueryOptions,
  UseQueryResult,
  MutationAction,
  MutateOptions,
  UseMutationResult,
  UseAuthResult,
  UsePresenceResult,
  UseCRDTResult,
} from './hooks.js'

// Re-export core types commonly needed in React components
export { BaseClient, BaseClientError } from '../core/client.js'
export type {
  BaseRecord,
  AuthStore,
  ClientConfig,
  ListOptions,
  ListResult,
  StateVersion,
  RealtimeEvent,
  ConnectionState,
} from '../core/index.js'

/**
 * @hanzoai/base-react -- React hooks for Hanzo Base.
 *
 * Standalone package that depends on @hanzoai/base as a peer dependency.
 * Provides all React hooks for reactive queries, mutations, auth,
 * realtime subscriptions, presence, and CRDT collaboration.
 */

// Provider & context
export { BaseProvider, useBaseClient } from './provider.js'
export type { BaseProviderProps, BaseContextValue } from './provider.js'

// Query
export { useQuery } from './use-query.js'
export type { UseQueryOptions, UseQueryResult } from './use-query.js'

// Mutation
export { useMutation } from './use-mutation.js'
export type { MutationAction, MutationOptions, UseMutationResult } from './use-mutation.js'

// Auth
export { useAuth } from './use-auth.js'
export type { UseAuthResult } from './use-auth.js'

// Realtime
export { useRealtime } from './use-realtime.js'
export type { RealtimeHandler, UseRealtimeOptions } from './use-realtime.js'

// Connection state
export { useConnectionState } from './use-connection.js'

// Presence
export { usePresence } from './use-presence.js'
export type { UsePresenceResult } from './use-presence.js'

// CRDT
export { useCRDT, useCRDTText, useCRDTCounter } from './use-crdt.js'
export type { UseCRDTOptions, UseCRDTResult } from './use-crdt.js'

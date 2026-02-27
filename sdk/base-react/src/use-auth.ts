/**
 * useAuth -- authentication state hook.
 *
 * Subscribes to the client's AuthStore via useSyncExternalStore.
 * Returns the current user record and auth actions (signIn, signUp, signOut).
 */

import { useCallback, useRef, useSyncExternalStore } from 'react'
import type { BaseRecord } from '@hanzoai/base'
import { useBaseClient } from './provider.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseAuthResult {
  /** The authenticated user record, or null. */
  user: BaseRecord | null
  /** Current JWT token, or empty string. */
  token: string
  /** Whether the token is present and not expired. */
  isAuthenticated: boolean
  /** Sign in with email/username and password. */
  signIn: (identity: string, password: string, collection?: string) => Promise<BaseRecord>
  /** Create a new user account. */
  signUp: (data: Record<string, unknown>, collection?: string) => Promise<BaseRecord>
  /** Clear auth state. */
  signOut: () => void
}

// ---------------------------------------------------------------------------
// Auth snapshot (immutable per change)
// ---------------------------------------------------------------------------

interface AuthSnapshot {
  user: BaseRecord | null
  token: string
  isAuthenticated: boolean
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useAuth(defaultCollection = 'users'): UseAuthResult {
  const client = useBaseClient()

  // Build snapshot from current auth state.
  const buildSnapshot = useCallback((): AuthSnapshot => {
    return {
      user: client.authStore.record,
      token: client.authStore.token,
      isAuthenticated: client.authStore.isValid,
    }
  }, [client])

  const snapshotRef = useRef<AuthSnapshot>(buildSnapshot())

  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      return client.authStore.onChange(() => {
        snapshotRef.current = buildSnapshot()
        onStoreChange()
      })
    },
    [client, buildSnapshot],
  )

  const getSnapshot = useCallback(() => snapshotRef.current, [])

  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  // Auth actions.
  const signIn = useCallback(
    async (identity: string, password: string, collection?: string): Promise<BaseRecord> => {
      const result = await client.signInWithPassword(
        collection ?? defaultCollection,
        identity,
        password,
      )
      return result.record
    },
    [client, defaultCollection],
  )

  const signUp = useCallback(
    async (data: Record<string, unknown>, collection?: string): Promise<BaseRecord> => {
      return client.signUp(collection ?? defaultCollection, data)
    },
    [client, defaultCollection],
  )

  const signOut = useCallback(() => {
    client.signOut()
  }, [client])

  return {
    user: snapshot.user,
    token: snapshot.token,
    isAuthenticated: snapshot.isAuthenticated,
    signIn,
    signUp,
    signOut,
  }
}

/**
 * BaseProvider -- React context providing the BaseClient instance.
 *
 * Usage:
 *   <BaseProvider url="https://myapp.hanzo.ai">
 *     <App />
 *   </BaseProvider>
 *
 *   // or with an existing client:
 *   <BaseProvider client={myClient}>
 *     <App />
 *   </BaseProvider>
 */

import { createContext, useContext, useRef, useEffect, type ReactNode } from 'react'
import { BaseClient, type AuthStore, type ClientConfig } from '../core/client.js'

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

const BaseContext = createContext<BaseClient | null>(null)

// ---------------------------------------------------------------------------
// Provider props
// ---------------------------------------------------------------------------

export interface BaseProviderProps {
  /** Base URL for automatic client creation. */
  url?: string
  /** Optional auth store override. */
  authStore?: AuthStore
  /** Pre-created BaseClient (takes precedence over url). */
  client?: BaseClient
  children: ReactNode
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

export function BaseProvider({ url, authStore, client: externalClient, children }: BaseProviderProps) {
  // Use a ref to hold the client so it survives re-renders without
  // creating a new client on every render.
  const clientRef = useRef<BaseClient | null>(null)

  if (externalClient) {
    clientRef.current = externalClient
  } else if (!clientRef.current && url) {
    const config: ClientConfig = { url }
    if (authStore) config.authStore = authStore
    clientRef.current = new BaseClient(config)
  }

  const client = clientRef.current
  if (!client) {
    throw new Error('BaseProvider: provide either `url` or `client` prop')
  }

  // Cleanup: disconnect realtime on unmount.
  useEffect(() => {
    return () => {
      // Only disconnect if we own the client (created from url).
      if (!externalClient) {
        client.disconnect()
      }
    }
  }, [client, externalClient])

  return <BaseContext value={client}>{children}</BaseContext>
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Get the BaseClient from the nearest BaseProvider.
 * Throws if used outside a provider.
 */
export function useBase(): BaseClient {
  const client = useContext(BaseContext)
  if (!client) {
    throw new Error('useBase: must be used within a <BaseProvider>')
  }
  return client
}

/**
 * BaseProvider -- React context for @hanzoai/base client.
 *
 * Creates a BaseClient from `url` (or accepts a pre-built client) and
 * makes it available to all descendant hooks via context.
 */

import { createContext, useContext, useRef, useEffect, type ReactNode } from 'react'
import { BaseClient, type ClientConfig } from '@hanzoai/base'

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

export interface BaseContextValue {
  client: BaseClient
}

const BaseContext = createContext<BaseContextValue | null>(null)

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

export interface BaseProviderProps {
  /** Base server URL (e.g. "https://myapp.hanzo.ai"). */
  url?: string
  /** Full client config. Takes precedence over `url`. */
  config?: ClientConfig
  /** Pre-built client instance. Takes precedence over both `url` and `config`. */
  client?: BaseClient
  children: ReactNode
}

export function BaseProvider({ url, config, client: externalClient, children }: BaseProviderProps) {
  const clientRef = useRef<BaseClient | null>(null)

  if (!clientRef.current) {
    if (externalClient) {
      clientRef.current = externalClient
    } else if (config) {
      clientRef.current = new BaseClient(config)
    } else if (url) {
      clientRef.current = new BaseClient(url)
    } else {
      throw new Error('BaseProvider: one of `url`, `config`, or `client` is required')
    }
  }

  const client = clientRef.current

  // Cleanup: disconnect realtime on unmount.
  useEffect(() => {
    return () => {
      // Only disconnect if we created the client (not externally provided).
      if (!externalClient) {
        client.disconnect()
      }
    }
  }, [client, externalClient])

  return (
    <BaseContext.Provider value={{ client }}>
      {children}
    </BaseContext.Provider>
  )
}

// ---------------------------------------------------------------------------
// Hook to access client
// ---------------------------------------------------------------------------

export function useBaseClient(): BaseClient {
  const ctx = useContext(BaseContext)
  if (!ctx) {
    throw new Error('useBaseClient: must be used within a <BaseProvider>')
  }
  return ctx.client
}

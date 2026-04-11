/**
 * @hanzo/base/compat -- upstream-compatible client re-exports.
 *
 * Provides a migration shim so existing code can switch to @hanzo/base
 * without changes. Will be removed in a future major version.
 *
 * Usage:
 *   import Base, { LocalAuthStore, isTokenExpired } from '@hanzo/base/compat'
 */

export {
  default,
  default as Base,
  LocalAuthStore,
  AsyncAuthStore,
  BaseAuthStore,
  isTokenExpired,
  ClientResponseError,
  cookieParse,
  cookieSerialize,
  getTokenPayload,
  normalizeUnknownQueryParams,
} from 'pocketbase'

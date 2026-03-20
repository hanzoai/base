/**
 * @hanzoai/base/compat -- PocketBase-compatible client re-exports.
 *
 * Provides a drop-in replacement for the `pocketbase` npm package
 * so that existing code can migrate to `@hanzoai/base` without changes.
 *
 * Usage:
 *   import Base, { LocalAuthStore, isTokenExpired } from '@hanzoai/base/compat'
 */

export {
  default,
  default as PocketBase,
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

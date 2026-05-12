/**
 * @hanzo/base/compat -- drop-in for legacy client imports.
 *
 * Re-exports every symbol consumers used to import from the legacy
 * client. Switching the specifier (and only the specifier) carries
 * existing code over with no further changes:
 *
 *   - import Base, { LocalAuthStore } from '@hanzo/base/compat'
 *
 * Everything here is implemented natively in @hanzo/base — no
 * upstream package dependency.
 */

export {
  default,
  default as Base,
  BaseClient,
  MemoryAuthStore,
  LocalAuthStore,
  AsyncAuthStore,
  ClientResponseError,
  isTokenExpired,
  getTokenPayload,
  cookieParse,
  cookieSerialize,
  normalizeUnknownQueryParams,
} from '../core/index.js'

export type {
  AuthStore,
  AuthChangeCallback,
  ClientConfig,
  ListOptions,
  ListResult,
  BaseAuthStore,
  BaseRecord,
  RecordModel,
  CollectionField,
  CollectionModel,
  RecordQueryOptions,
  RecordFullListOptions,
  FileOptions,
  AuthResponse,
  OAuth2Options,
  ClientResponseErrorData,
  CookieSerializeOptions,
} from '../core/index.js'

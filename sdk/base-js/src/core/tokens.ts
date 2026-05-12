/**
 * JWT + cookie helpers — small utilities the compat layer used to
 * re-export from the upstream client. Implemented natively so the SDK
 * has zero upstream dependency.
 */

/**
 * Decode the payload of a JWT without verifying its signature.
 * Returns `null` for malformed tokens. Safe for browser + Node use
 * (relies only on global `atob`).
 */
export function getTokenPayload<T = Record<string, unknown>>(token: string): T | null {
  if (!token) return null
  const parts = token.split('.')
  if (parts.length !== 3) return null
  try {
    const padded = parts[1].replace(/-/g, '+').replace(/_/g, '/')
    return JSON.parse(atob(padded)) as T
  } catch {
    return null
  }
}

/**
 * Check whether a JWT's `exp` claim has passed.
 * `expirationThreshold` (seconds) is subtracted from `exp` to expire
 * tokens early — set this when you want to refresh before the actual
 * expiration. Tokens without an `exp` claim are treated as
 * non-expiring.
 */
export function isTokenExpired(token: string, expirationThreshold: number = 0): boolean {
  const payload = getTokenPayload<{ exp?: number }>(token)
  if (!payload || typeof payload.exp !== 'number') return true
  return payload.exp - expirationThreshold <= Date.now() / 1000
}

/**
 * Cookie parsing — extracts a name→value map from a Set-Cookie or
 * Cookie header value. Decodes URI-encoded values. Mirrors the
 * `cookie` npm package's signature so it's drop-in for the upstream
 * client's `cookieParse`.
 */
export function cookieParse(input: string): Record<string, string> {
  const out: Record<string, string> = {}
  if (!input) return out
  for (const segment of input.split(/;\s*/)) {
    if (!segment) continue
    const eq = segment.indexOf('=')
    if (eq < 0) continue
    const key = segment.slice(0, eq).trim()
    let value = segment.slice(eq + 1).trim()
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1)
    try {
      out[key] = decodeURIComponent(value)
    } catch {
      out[key] = value
    }
  }
  return out
}

export interface CookieSerializeOptions {
  encode?: (value: string) => string
  maxAge?: number
  domain?: string
  path?: string
  expires?: Date
  httpOnly?: boolean
  secure?: boolean
  sameSite?: 'strict' | 'lax' | 'none' | boolean
  priority?: 'low' | 'medium' | 'high'
}

/**
 * Cookie serialization — builds a `Set-Cookie` header value.
 * `encode` defaults to `encodeURIComponent`. Throws if the name or
 * encoded value contain invalid characters.
 */
export function cookieSerialize(
  name: string,
  value: string,
  options: CookieSerializeOptions = {},
): string {
  if (!/^[\w!#$%&'*+\-.^`|~]+$/.test(name)) {
    throw new TypeError(`cookieSerialize: invalid cookie name ${JSON.stringify(name)}`)
  }
  const encode = options.encode ?? encodeURIComponent
  const encoded = encode(value)
  if (encoded && !/^[\w!#$%&'()*+\-./:<=>?@[\]^`{|}~]*$/.test(encoded)) {
    throw new TypeError(`cookieSerialize: invalid cookie value for ${name}`)
  }
  const parts = [`${name}=${encoded}`]
  if (typeof options.maxAge === 'number' && Number.isFinite(options.maxAge)) {
    parts.push(`Max-Age=${Math.floor(options.maxAge)}`)
  }
  if (options.domain) parts.push(`Domain=${options.domain}`)
  if (options.path) parts.push(`Path=${options.path}`)
  if (options.expires) parts.push(`Expires=${options.expires.toUTCString()}`)
  if (options.httpOnly) parts.push('HttpOnly')
  if (options.secure) parts.push('Secure')
  if (options.sameSite !== undefined && options.sameSite !== false) {
    const ss = options.sameSite
    const value =
      ss === true
        ? 'Strict'
        : `${ss.charAt(0).toUpperCase()}${ss.slice(1)}`
    parts.push(`SameSite=${value}`)
  }
  if (options.priority) {
    parts.push(`Priority=${options.priority.charAt(0).toUpperCase() + options.priority.slice(1)}`)
  }
  return parts.join('; ')
}

/**
 * Normalize a query-param record so values are always strings (or
 * arrays of strings). Mirrors the upstream `normalizeUnknownQueryParams`
 * helper used by the auto-encoding URL builder. Nullish entries are
 * dropped; non-primitive entries are JSON-stringified.
 */
export function normalizeUnknownQueryParams(
  params: Record<string, unknown> | null | undefined,
): Record<string, string | string[]> {
  const out: Record<string, string | string[]> = {}
  if (!params) return out
  for (const [key, raw] of Object.entries(params)) {
    if (raw === undefined || raw === null) continue
    if (Array.isArray(raw)) {
      out[key] = raw.map((v) => (typeof v === 'string' ? v : JSON.stringify(v)))
    } else if (typeof raw === 'string') {
      out[key] = raw
    } else if (typeof raw === 'number' || typeof raw === 'boolean') {
      out[key] = String(raw)
    } else {
      out[key] = JSON.stringify(raw)
    }
  }
  return out
}

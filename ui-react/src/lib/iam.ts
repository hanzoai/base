// Hanzo IAM auth for the Base admin (HIP-0111).
//
// Base is IAM-native: it retired the local `_superusers` password login.
// The admin authenticates with OAuth2 PKCE (S256) against Hanzo IAM via the
// canonical `@hanzo/iam` SPA SDK, then rides the resulting IAM access-token
// JWT as the Base `/v1` bearer. The Base server validates that JWT against
// IAM's JWKS (`loadAuthToken`/`resolveJWKSToken`) and, for admins
// (`isGlobalAdmin` / `isAdmin` / built-in org), mints an ephemeral
// `_superusers` session — no identity is ever stored in Base.
//
// serverUrl points DIRECTLY at IAM, not at Base's same-origin `/v1/iam`
// reverse proxy: that proxy strips the `/v1/iam` prefix and would forward to
// IAM's SPA HTML catch-all, and IAM already returns
// `Access-Control-Allow-Origin` for the admin origin — so the direct,
// discovery-driven canonical endpoints are the one correct way.
import { IAM } from '@hanzo/iam/browser';

// Overridable per-deploy; the defaults are the Hanzo production values.
const serverUrl = import.meta.env.VITE_IAM_URL || 'https://hanzo.id';
const clientId = import.meta.env.VITE_IAM_CLIENT_ID || 'hanzo-base';

// Derived from the admin mount (Vite BASE_URL) rather than written out, so the
// redirect this SPA asks for and the address it is served at cannot disagree —
// and the issuer must accept whatever comes out. The Go static handler serves
// index.html for unknown deep links, so the router resolves it client-side.
function redirectUri(): string {
  const mount = import.meta.env.BASE_URL; // ends in a slash
  return `${window.location.origin}${mount}auth/callback`;
}

// `storage` is where the SESSION lives — access token, refresh token, expiry.
// localStorage, because every tab on this origin shares it and the refresh
// token is what renews the bearer: with sessionStorage only the tab that signed
// in could renew, and the recovery this admin offers signs in on a NEW tab, so
// the tab holding the user's work would be the one that never could.
// (The PKCE verifier is a separate store the SDK resolves itself; it has always
// been localStorage so it survives the cross-origin redirect.)
export const iam = new IAM({
  serverUrl,
  clientId,
  redirectUri: redirectUri(),
  scope: 'openid profile email',
  storage: localStorage,
});

// Minimal JWT payload decode (no verification — the Base server verifies the
// signature against IAM JWKS). Used only to populate the client-side session
// record for display + the superuser guard.
export function decodeJwtClaims(token: string): Record<string, unknown> {
  try {
    const payload = token.split('.')[1];
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
    return JSON.parse(json) as Record<string, unknown>;
  } catch {
    return {};
  }
}

// Who a bearer says it belongs to, for this browser's own use — the sidebar's
// name and the settings guard. Sign-in and renewal both land here, so the two
// paths cannot describe the same session differently.
//
// The server does not take this browser's word for any of it: `resolveJWKSToken`
// decides authority from IAM membership and mirrors the caller into
// `_superusers` or `users` itself. This is the local echo of that decision, and
// it is the one place in the admin that echoes it.
export function identity(accessToken: string): Record<string, unknown> {
  const claims = decodeJwtClaims(accessToken);
  const admin =
    claims.isGlobalAdmin === true ||
    claims.isAdmin === true ||
    claims.owner === 'built-in' ||
    claims.owner === 'superuser';

  return {
    id: String(claims.sub ?? claims.id ?? ''),
    email: String(claims.email ?? ''),
    name: String(claims.name ?? claims.displayName ?? ''),
    // Base guards key the superuser off collectionName.
    collectionName: admin ? '_superusers' : 'users',
  };
}

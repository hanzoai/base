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

// The callback lives under the admin mount (Vite BASE_URL, e.g. `/_/`). The Go
// static handler serves index.html for unknown deep links, so the SPA router
// resolves `/_/auth/callback` client-side.
function redirectUri(): string {
  const mount = import.meta.env.BASE_URL; // '/_/' — trailing slash
  return `${window.location.origin}${mount}auth/callback`;
}

// PKCE verifier + state ride sessionStorage across the cross-origin redirect
// round-trip (survives same-tab navigation to IAM and back).
export const iam = new IAM({
  serverUrl,
  clientId,
  redirectUri: redirectUri(),
  scope: 'openid profile email',
  storage: sessionStorage,
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

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';

import { setAuth } from '~/lib/api';
import { decodeJwtClaims, iam } from '~/lib/iam';

// OAuth2 PKCE callback. IAM redirects here with `?code=`; we exchange it for
// the IAM access-token JWT and bridge it into the Base auth store. Every Base
// `/v1` request then carries that JWT as the bearer — the Base server
// validates it against IAM's JWKS and, for admins, mints an ephemeral
// `_superusers` session. Base never persists identity.
function AuthCallback() {
  const nav = useNavigate();
  const [error, setError] = useState<string>('');
  // The authorization code is one-shot (the SDK clears the PKCE verifier
  // before the network call); guard against React StrictMode's double effect.
  const ran = useRef(false);

  useEffect(() => {
    if (ran.current) return;
    ran.current = true;

    (async () => {
      try {
        const token = await iam.handleCallback();
        const claims = decodeJwtClaims(token.accessToken);

        // Mirror the server's admin rule (resolveJWKSToken): global/org admin
        // or the built-in/superuser org maps to a `_superusers` session.
        const isAdmin =
          claims.isGlobalAdmin === true ||
          claims.isAdmin === true ||
          claims.owner === 'built-in' ||
          claims.owner === 'superuser';

        setAuth(token.accessToken, {
          id: String(claims.sub ?? claims.id ?? ''),
          email: String(claims.email ?? ''),
          name: String(claims.name ?? claims.displayName ?? ''),
          // Base guards key the superuser off collectionName.
          collectionName: isAdmin ? '_superusers' : 'users',
        });

        await nav({ to: '/', replace: true });
      } catch (err: unknown) {
        setError((err as Error)?.message ?? 'Sign-in failed');
      }
    })();
  }, [nav]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 text-center">
        { error ? (
          <>
            <p className="mb-4 text-sm text-destructive">{ error }</p>
            <a href={ `${import.meta.env.BASE_URL}login` } className="text-sm text-primary underline-offset-4 hover:underline">
              Back to sign in
            </a>
          </>
        ) : (
          <p className="text-sm text-muted-foreground">Completing sign-in…</p>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/auth/callback')({ component: AuthCallback });

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';

import { setAuth } from '~/lib/api';
import { iam, identity } from '~/lib/iam';

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
        // Sign-in and renewal describe a session the same way, because they
        // describe it in the same place.
        setAuth(token.accessToken, identity(token.accessToken));

        await nav({ to: '/', replace: true });
      } catch (err: unknown) {
        setError((err as Error)?.message ?? 'Sign-in failed');
      }
    })();
  }, [nav]);

  return (
    <div className="shell shell--centered">
      <div className="panel stack" style={{ textAlign: 'center' }}>
        { error ? (
          <>
            <p className="danger">{ error }</p>
            <a href={ `${import.meta.env.BASE_URL}login` }>Back to sign in</a>
          </>
        ) : (
          <p className="muted">Completing sign-in…</p>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/auth/callback')({ component: AuthCallback });

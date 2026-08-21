import { createFileRoute, redirect } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

import { base } from '~/lib/base';
import { embedded } from '~/lib/embed';
import { iam } from '~/lib/iam';
import { icon } from '../icon'

// Marks that the frame has already been round-tripped to IAM once. Without it a
// signed-out visitor bounces frame → IAM → frame → IAM forever, because landing
// back here signed out looks exactly like arriving for the first time.
const TRIED = 'base_embed_signin_tried';

// Base is IAM-native: sign-in is OAuth2 PKCE against Hanzo IAM. There is no
// local password — the retired `_superusers` password endpoint is gone
// (410/404). "Sign in with Hanzo" redirects to the IAM authorize endpoint;
// the `/auth/callback` route completes the exchange.
//
// Embedded, that same redirect is what makes the host's session carry: a
// visitor who is signed in at IAM has a live session there, so authorize hands
// back a code without rendering anything and the frame simply fills in. So the
// frame takes the trip on its own — asking someone to sign in to Base when
// they are already signed in to the page Base is sitting in is the second login
// this is meant not to have.
//
// It gets ONE attempt. A visitor with no IAM session cannot finish inside a
// frame — the IdP's own login refuses to be framed, and it is right to — so
// the honest answer is Base's real address in a new tab, not a login form of
// ours in here.
function LoginPage() {
  const [error, setError] = useState<string>('');
  const [busy, setBusy] = useState(false);
  const spent = embedded && sessionStorage.getItem(TRIED) === '1';

  const signIn = async () => {
    setError('');
    setBusy(true);
    try {
      if (embedded) sessionStorage.setItem(TRIED, '1');
      await iam.signinRedirect();
      // signinRedirect navigates away; nothing runs after it on success.
    } catch (err: unknown) {
      setError((err as Error)?.message ?? 'Sign-in failed');
      setBusy(false);
    }
  };

  useEffect(() => {
    if (embedded && !spent) void signIn();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (embedded && !spent) {
    return <div className="shell shell--centered"><p className="muted">Signing in…</p></div>;
  }

  return (
    <div className="shell shell--centered">
      <div className="panel stack">
        <div className="row">
          <span
            className="mark"
            role="img"
            aria-label="Base"
            style={ { ['--mark' as string]: `url(${icon})`, width: 24, height: 24 } }
          />
          <h1 className="page__title">Sign in to Base</h1>
        </div>
        <p className="muted">
          { embedded
            ? 'Base could not pick up your session in this panel. Open it directly to sign in.'
            : 'Base signs you in with Hanzo ID. Use your Hanzo account to reach the admin.' }
        </p>
        { error && <p className="danger">{ error }</p> }
        { embedded ? (
          <a className="btn" href={ window.location.origin } target="_blank" rel="noreferrer">
            Open Base
          </a>
        ) : (
          <button type="button" onClick={ signIn } disabled={ busy } className="btn">
            { busy ? 'Redirecting…' : 'Sign in with Hanzo' }
          </button>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/login')({
  // Already signed in → straight to the dashboard. A session, not a leftover
  // token: someone whose bearer has expired came here to fix exactly that.
  beforeLoad: () => {
    if (base.authStore.isValid) throw redirect({ to: '/' });
  },
  component: LoginPage,
});

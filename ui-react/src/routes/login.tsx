import { createFileRoute, redirect } from '@tanstack/react-router';
import { useState } from 'react';

import { base } from '~/lib/base';
import { iam } from '~/lib/iam';
import { icon } from '../icon'

// Base is IAM-native: sign-in is OAuth2 PKCE against Hanzo IAM. There is no
// local password — the retired `_superusers` password endpoint is gone
// (410/404). "Sign in with Hanzo" redirects to the IAM authorize endpoint;
// the `/auth/callback` route completes the exchange.
function LoginPage() {
  const [error, setError] = useState<string>('');
  const [busy, setBusy] = useState(false);

  const signIn = async () => {
    setError('');
    setBusy(true);
    try {
      await iam.signinRedirect();
      // signinRedirect navigates away; nothing runs after it on success.
    } catch (err: unknown) {
      setError((err as Error)?.message ?? 'Sign-in failed');
      setBusy(false);
    }
  };

  return (
    <div className="shell shell--centered">
      <div className="panel stack">
        <div className="row">
          <img src={icon} alt="Base" width={ 24 } height={ 24 } />
          <h1 className="page__title">Sign in to Base</h1>
        </div>
        <p className="muted">
          Base uses Hanzo IAM for authentication. Sign in with your Hanzo
          account to reach the admin.
        </p>
        { error && <p className="danger">{ error }</p> }
        <button type="button" onClick={ signIn } disabled={ busy } className="btn">
          { busy ? 'Redirecting…' : 'Sign in with Hanzo' }
        </button>
      </div>
    </div>
  );
}

export const Route = createFileRoute('/login')({
  // Already signed in → straight to the dashboard.
  beforeLoad: () => {
    if (base.authStore.token) throw redirect({ to: '/' });
  },
  component: LoginPage,
});

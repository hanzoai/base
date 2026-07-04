import { createFileRoute, redirect } from '@tanstack/react-router';
import { useState } from 'react';

import { Button } from '~/components/ui/button';
import { base } from '~/lib/base';
import { iam } from '~/lib/iam';

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
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8">
        <div className="mb-6 flex items-center gap-2">
          <img src="/icon.svg" alt="Base" className="h-6 w-6" />
          <h1 className="text-lg font-semibold text-foreground">Sign in to Base</h1>
        </div>
        <p className="mb-6 text-sm text-muted-foreground">
          Base uses Hanzo IAM for authentication. Sign in with your Hanzo
          account to reach the admin.
        </p>
        { error && <p className="mb-4 text-sm text-destructive">{ error }</p> }
        <Button onClick={ signIn } isLoading={ busy } disabled={ busy } className="w-full">
          { busy ? 'Redirecting…' : 'Sign in with Hanzo' }
        </Button>
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

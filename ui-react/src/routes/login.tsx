import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';

import { Button } from '~/components/ui/button';
import { Input } from '~/components/ui/input';
import { Label } from '~/components/ui/label';
import { base } from '~/lib/base';

interface LoginForm {
  identity: string;
  password: string;
}

function LoginPage() {
  const nav = useNavigate();
  const [error, setError] = useState<string>('');
  const { register, handleSubmit, formState } = useForm<LoginForm>();

  const onSubmit = async (values: LoginForm) => {
    setError('');
    try {
      await base.collection('_superusers').authWithPassword(values.identity, values.password);
      await nav({ to: '/' });
    } catch (err: unknown) {
      setError((err as Error)?.message ?? 'Sign-in failed');
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8">
        <div className="mb-6 flex items-center gap-2">
          <img src="/icon.svg" alt="Base" className="h-6 w-6" />
          <h1 className="text-lg font-semibold text-foreground">Sign in to Base</h1>
        </div>
        <form onSubmit={ handleSubmit(onSubmit) } className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="identity">Email</Label>
            <Input
              id="identity"
              { ...register('identity', { required: true }) }
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              { ...register('password', { required: true, minLength: 10 }) }
              type="password"
              autoComplete="current-password"
            />
          </div>
          { error && <p className="text-sm text-destructive">{ error }</p> }
          <Button type="submit" disabled={ formState.isSubmitting } className="mt-2 w-full">
            { formState.isSubmitting ? 'Signing in…' : 'Sign in' }
          </Button>
        </form>
      </div>
    </div>
  );
}

export const Route = createFileRoute('/login')({ component: LoginPage });

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { useForm } from 'react-hook-form';

import { base } from '~/lib/base';

interface LoginForm {
  identity: string;
  password: string;
}

function LoginPage() {
  const nav = useNavigate();
  const [ error, setError ] = useState<string>('');
  const { register, handleSubmit, formState } = useForm<LoginForm>();

  const onSubmit = async(values: LoginForm) => {
    setError('');
    try {
      await base.collection('_superusers').authWithPassword(values.identity, values.password);
      await nav({ to: '/' });
    } catch (err: unknown) {
      setError((err as Error)?.message ?? 'Sign-in failed');
    }
  };

  return (
    <div className="mx-auto mt-24 max-w-sm rounded-lg border border-neutral-800 p-6">
      <h1 className="mb-6 text-lg font-semibold">Sign in to Base</h1>
      <form onSubmit={ handleSubmit(onSubmit) } className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-400">Email</span>
          <input
            { ...register('identity', { required: true }) }
            type="email"
            autoComplete="email"
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-400">Password</span>
          <input
            { ...register('password', { required: true, minLength: 10 }) }
            type="password"
            autoComplete="current-password"
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5"
          />
        </label>
        { error && <div className="text-sm text-red-400">{ error }</div> }
        <button
          type="submit"
          disabled={ formState.isSubmitting }
          className="mt-2 rounded bg-indigo-600 py-1.5 font-medium hover:bg-indigo-500 disabled:opacity-50"
        >
          { formState.isSubmitting ? 'Signing in…' : 'Sign in' }
        </button>
      </form>
    </div>
  );
}

export const Route = createFileRoute('/login')({ component: LoginPage });

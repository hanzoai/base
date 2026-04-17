import { useState, type FormEvent } from 'react'
import { useLocation } from 'wouter'
import { authWithPassword } from '~/lib/api'

export function Login() {
  const [, navigate] = useLocation()
  const [identity, setIdentity] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      await authWithPassword(identity, password)
      navigate('/')
    } catch (err) {
      setError((err as Error)?.message ?? 'Sign-in failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto mt-24 max-w-sm rounded-lg border border-neutral-800 p-6">
      <h1 className="mb-6 text-lg font-semibold">Sign in to Base</h1>
      <form onSubmit={handleSubmit} className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-400">Email</span>
          <input
            value={identity}
            onChange={(e) => setIdentity(e.target.value)}
            type="email"
            autoComplete="email"
            required
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-neutral-400">Password</span>
          <input
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            type="password"
            autoComplete="current-password"
            required
            minLength={10}
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5"
          />
        </label>
        {error && <div className="text-sm text-red-400">{error}</div>}
        <button
          type="submit"
          disabled={submitting}
          className="mt-2 rounded bg-indigo-600 py-1.5 font-medium hover:bg-indigo-500 disabled:opacity-50"
        >
          {submitting ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}

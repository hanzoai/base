import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { getSettings, testEmail, updateSettings } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'
const btnSecondary = 'rounded border border-neutral-700 px-4 py-1.5 text-sm hover:bg-neutral-800 disabled:opacity-50'

export function SettingsSmtp() {
  const qc = useQueryClient()
  const [testOpen, setTestOpen] = useState(false)
  const [testStatus, setTestStatus] = useState('')

  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })

  const smtp = settings.data?.smtp as Record<string, unknown> | undefined
  const meta = settings.data?.meta as Record<string, unknown> | undefined

  const [smtpForm, setSmtpForm] = useState<Record<string, unknown> | null>(null)
  const [metaForm, setMetaForm] = useState<Record<string, unknown> | null>(null)

  const smtpVals = smtpForm ?? {
    enabled: (smtp?.enabled as boolean) ?? false,
    host: (smtp?.host as string) ?? '',
    port: (smtp?.port as number) ?? 587,
    username: (smtp?.username as string) ?? '',
    password: (smtp?.password as string) ?? '',
    authMethod: (smtp?.authMethod as string) ?? 'PLAIN',
    tls: (smtp?.tls as boolean) ?? false,
    localName: (smtp?.localName as string) ?? '',
  }

  const metaVals = metaForm ?? {
    senderName: (meta?.senderName as string) ?? '',
    senderAddress: (meta?.senderAddress as string) ?? '',
  }

  const dirty = smtpForm !== null || metaForm !== null

  function setSmtp(k: string, v: unknown) { setSmtpForm({ ...smtpVals, [k]: v }) }
  function setMeta(k: string, v: unknown) { setMetaForm({ ...metaVals, [k]: v }) }

  const saveMutation = useMutation({
    mutationFn: () => {
      const smtpPayload: Record<string, unknown> = { ...smtpVals }
      if (smtpPayload.password === (smtp?.password as string)) delete smtpPayload.password
      return updateSettings({ smtp: smtpPayload, meta: { senderName: metaVals.senderName, senderAddress: metaVals.senderAddress } })
    },
    onSuccess: () => {
      setSmtpForm(null)
      setMetaForm(null)
      qc.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const [testTo, setTestTo] = useState('')
  const [testTemplate, setTestTemplate] = useState('verification')
  const [testCollection, setTestCollection] = useState('_superusers')

  const testMutation = useMutation({
    mutationFn: () => testEmail(testCollection, testTo, testTemplate),
    onSuccess: () => setTestStatus('Test email sent.'),
    onError: (err: Error) => setTestStatus(err.message),
  })

  function handleSave(e: FormEvent) {
    e.preventDefault()
    saveMutation.mutate()
  }

  if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>
  if (settings.error) return <div className="text-sm text-red-400">{String(settings.error)}</div>

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Sender" description="Default sender name and address for outgoing emails.">
        <div className="grid grid-cols-2 gap-4">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-400">Sender name</span>
            <input value={String(metaVals.senderName)} onChange={(e) => setMeta('senderName', e.target.value)} className={inputClass} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-400">Sender address</span>
            <input value={String(metaVals.senderAddress)} onChange={(e) => setMeta('senderAddress', e.target.value)} type="email" className={inputClass} />
          </label>
        </div>
      </SectionCard>

      <SectionCard title="SMTP" description="Configure SMTP mail server for sending emails.">
        <form onSubmit={handleSave} className="flex flex-col gap-4">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={Boolean(smtpVals.enabled)} onChange={(e) => setSmtp('enabled', e.target.checked)} className="accent-indigo-500" />
            <span>Use SMTP mail server (recommended)</span>
          </label>

          {smtpVals.enabled && (
            <>
              <div className="grid grid-cols-4 gap-4">
                <label className="col-span-2 flex flex-col gap-1 text-sm"><span className="text-neutral-400">Host</span><input value={String(smtpVals.host)} onChange={(e) => setSmtp('host', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Port</span><input type="number" value={Number(smtpVals.port)} onChange={(e) => setSmtp('port', Number(e.target.value))} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">TLS</span><select value={String(smtpVals.tls)} onChange={(e) => setSmtp('tls', e.target.value === 'true')} className={inputClass}><option value="">Auto</option><option value="true">Always</option></select></label>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Username</span><input value={String(smtpVals.username)} onChange={(e) => setSmtp('username', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Password</span><input type="password" value={String(smtpVals.password)} onChange={(e) => setSmtp('password', e.target.value)} className={inputClass} /></label>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">AUTH method</span><select value={String(smtpVals.authMethod)} onChange={(e) => setSmtp('authMethod', e.target.value)} className={inputClass}><option value="PLAIN">PLAIN</option><option value="LOGIN">LOGIN</option></select></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">EHLO/HELO domain</span><input value={String(smtpVals.localName)} onChange={(e) => setSmtp('localName', e.target.value)} placeholder="localhost" className={inputClass} /></label>
              </div>
            </>
          )}

          <div className="flex items-center gap-2 pt-2">
            <button type="submit" disabled={!dirty || saveMutation.isPending} className={btnPrimary}>
              {saveMutation.isPending ? 'Saving...' : 'Save changes'}
            </button>
            {saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
            {saveMutation.error && <span className="text-xs text-red-400">{String(saveMutation.error)}</span>}
          </div>
        </form>
      </SectionCard>

      <SectionCard title="Test email" description="Send a test email to verify your SMTP configuration.">
        <button type="button" onClick={() => setTestOpen(!testOpen)} className={btnSecondary}>
          {testOpen ? 'Hide test form' : 'Send test email'}
        </button>
        {testOpen && (
          <form onSubmit={(e) => { e.preventDefault(); testMutation.mutate() }} className="mt-4 flex flex-col gap-3">
            <div className="grid grid-cols-2 gap-4">
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">To email</span><input value={testTo} onChange={(e) => setTestTo(e.target.value)} type="email" required className={inputClass} /></label>
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Template</span>
                <select value={testTemplate} onChange={(e) => setTestTemplate(e.target.value)} className={inputClass}>
                  <option value="verification">Verification</option>
                  <option value="password-reset">Password reset</option>
                  <option value="email-change">Email change</option>
                  <option value="otp">OTP</option>
                  <option value="login-alert">Login alert</option>
                </select>
              </label>
            </div>
            <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Auth collection</span><input value={testCollection} onChange={(e) => setTestCollection(e.target.value)} className={inputClass} /></label>
            <div className="flex items-center gap-2">
              <button type="submit" disabled={testMutation.isPending} className={btnPrimary}>{testMutation.isPending ? 'Sending...' : 'Send'}</button>
              {testStatus && <span className={`text-xs ${testMutation.isError ? 'text-red-400' : 'text-green-400'}`}>{testStatus}</span>}
            </div>
          </form>
        )}
      </SectionCard>
    </div>
  )
}

import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface SmtpForm {
    enabled: boolean;
    host: string;
    port: number;
    username: string;
    password: string;
    authMethod: string;
    tls: boolean;
    localName: string;
}

interface MetaForm {
    senderName: string;
    senderAddress: string;
}

interface TestEmailForm {
    collection: string;
    toEmail: string;
    template: string;
}

const authMethods = [
    { label: 'PLAIN (default)', value: 'PLAIN' },
    { label: 'LOGIN', value: 'LOGIN' },
];

const templateOptions = [
    { label: 'Verification', value: 'verification' },
    { label: 'Password reset', value: 'password-reset' },
    { label: 'Confirm email change', value: 'email-change' },
    { label: 'OTP', value: 'otp' },
    { label: 'Login alert', value: 'login-alert' },
];

function labelClass(sub?: boolean) {
    return 'flex flex-col gap-1 text-sm' + (sub ? ' text-neutral-400' : '');
}

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';
const btnSecondary = 'rounded border border-neutral-700 px-4 py-1.5 text-sm hover:bg-neutral-800 disabled:opacity-50';

function SmtpSettings() {
    const qc = useQueryClient();
    const [testOpen, setTestOpen] = useState(false);
    const [testStatus, setTestStatus] = useState<string>('');

    const settings = useQuery({
        queryKey: ['settings'],
        queryFn: () => base.settings.getAll(),
    });

    const smtp = settings.data?.smtp as Record<string, unknown> | undefined;
    const meta = settings.data?.meta as Record<string, unknown> | undefined;

    const { register: regSmtp, handleSubmit: submitSmtp, formState: fsSmtp, watch: watchSmtp } = useForm<SmtpForm>({
        values: {
            enabled: (smtp?.enabled as boolean) ?? false,
            host: (smtp?.host as string) ?? '',
            port: (smtp?.port as number) ?? 587,
            username: (smtp?.username as string) ?? '',
            password: (smtp?.password as string) ?? '',
            authMethod: (smtp?.authMethod as string) ?? 'PLAIN',
            tls: (smtp?.tls as boolean) ?? false,
            localName: (smtp?.localName as string) ?? '',
        },
    });

    const { register: regMeta, handleSubmit: submitMeta } = useForm<MetaForm>({
        values: {
            senderName: (meta?.senderName as string) ?? '',
            senderAddress: (meta?.senderAddress as string) ?? '',
        },
    });

    const { register: regTest, handleSubmit: submitTest, formState: fsTest } = useForm<TestEmailForm>({
        defaultValues: { collection: '_superusers', toEmail: '', template: 'verification' },
    });

    const smtpEnabled = watchSmtp('enabled');

    const saveMutation = useMutation({
        mutationFn: (data: { smtp: SmtpForm; meta: MetaForm }) => {
            const smtpPayload: Record<string, unknown> = { ...data.smtp };
            // Never send the redacted placeholder back — omit to preserve the real secret.
            if (smtpPayload.password === (smtp?.password as string)) delete smtpPayload.password;
            return base.settings.update({ smtp: smtpPayload, meta: { senderName: data.meta.senderName, senderAddress: data.meta.senderAddress } });
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['settings'] }); },
    });

    const testMutation = useMutation({
        mutationFn: (data: TestEmailForm) =>
            base.settings.testEmail(data.collection, data.toEmail, data.template),
        onSuccess: () => { setTestStatus('Test email sent.'); },
        onError: (err: Error) => { setTestStatus(err.message); },
    });

    function handleSave(smtpValues: SmtpForm) {
        submitMeta((metaValues) => {
            saveMutation.mutate({ smtp: smtpValues, meta: metaValues });
        })();
    }

    if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;
    if (settings.error) return <div className="text-sm text-red-400">{ String(settings.error) }</div>;

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Sender" description="Default sender name and address for outgoing emails.">
                <form onSubmit={ submitMeta(() => {}) } className="grid grid-cols-2 gap-4">
                    <label className={ labelClass() }>
                        <span className="text-neutral-400">Sender name</span>
                        <input { ...regMeta('senderName', { required: true }) } className={ inputClass } />
                    </label>
                    <label className={ labelClass() }>
                        <span className="text-neutral-400">Sender address</span>
                        <input { ...regMeta('senderAddress', { required: true }) } type="email" className={ inputClass } />
                    </label>
                </form>
            </SectionCard>

            <SectionCard title="SMTP" description="Configure SMTP mail server for sending emails.">
                <form onSubmit={ submitSmtp(handleSave) } className="flex flex-col gap-4">
                    <label className="flex items-center gap-2 text-sm">
                        <input { ...regSmtp('enabled') } type="checkbox" className="accent-indigo-500" />
                        <span>Use SMTP mail server (recommended)</span>
                    </label>

                    { smtpEnabled && (
                        <>
                            <div className="grid grid-cols-4 gap-4">
                                <label className={ labelClass() + ' col-span-2' }>
                                    <span className="text-neutral-400">Host</span>
                                    <input { ...regSmtp('host', { required: true }) } className={ inputClass } />
                                </label>
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">Port</span>
                                    <input { ...regSmtp('port', { required: true, valueAsNumber: true }) } type="number" className={ inputClass } />
                                </label>
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">TLS</span>
                                    <select { ...regSmtp('tls') } className={ inputClass }>
                                        <option value="">Auto (StartTLS)</option>
                                        <option value="true">Always</option>
                                    </select>
                                </label>
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">Username</span>
                                    <input { ...regSmtp('username') } className={ inputClass } />
                                </label>
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">Password</span>
                                    <input { ...regSmtp('password') } type="password" className={ inputClass } />
                                </label>
                            </div>
                            <div className="grid grid-cols-2 gap-4">
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">AUTH method</span>
                                    <select { ...regSmtp('authMethod') } className={ inputClass }>
                                        { authMethods.map((m) => (
                                            <option key={ m.value } value={ m.value }>{ m.label }</option>
                                        )) }
                                    </select>
                                </label>
                                <label className={ labelClass() }>
                                    <span className="text-neutral-400">EHLO/HELO domain</span>
                                    <input { ...regSmtp('localName') } placeholder="localhost" className={ inputClass } />
                                </label>
                            </div>
                        </>
                    ) }

                    <div className="flex items-center gap-2 pt-2">
                        <button type="submit" disabled={ !fsSmtp.isDirty || saveMutation.isPending } className={ btnPrimary }>
                            { saveMutation.isPending ? 'Saving...' : 'Save changes' }
                        </button>
                        { saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span> }
                        { saveMutation.error && <span className="text-xs text-red-400">{ saveMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>

            <SectionCard title="Test email" description="Send a test email to verify your SMTP configuration.">
                <button type="button" onClick={ () => setTestOpen(!testOpen) } className={ btnSecondary }>
                    { testOpen ? 'Hide test form' : 'Send test email' }
                </button>
                { testOpen && (
                    <form onSubmit={ submitTest((d) => testMutation.mutate(d)) } className="mt-4 flex flex-col gap-3">
                        <div className="grid grid-cols-2 gap-4">
                            <label className={ labelClass() }>
                                <span className="text-neutral-400">To email</span>
                                <input { ...regTest('toEmail', { required: true }) } type="email" className={ inputClass } />
                            </label>
                            <label className={ labelClass() }>
                                <span className="text-neutral-400">Template</span>
                                <select { ...regTest('template') } className={ inputClass }>
                                    { templateOptions.map((t) => (
                                        <option key={ t.value } value={ t.value }>{ t.label }</option>
                                    )) }
                                </select>
                            </label>
                        </div>
                        <label className={ labelClass() }>
                            <span className="text-neutral-400">Auth collection</span>
                            <input { ...regTest('collection') } className={ inputClass } placeholder="_superusers" />
                        </label>
                        <div className="flex items-center gap-2">
                            <button type="submit" disabled={ fsTest.isSubmitting || testMutation.isPending } className={ btnPrimary }>
                                { testMutation.isPending ? 'Sending...' : 'Send' }
                            </button>
                            { testStatus && (
                                <span className={ 'text-xs ' + (testMutation.isError ? 'text-red-400' : 'text-green-400') }>
                                    { testStatus }
                                </span>
                            ) }
                        </div>
                    </form>
                ) }
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/smtp')({
    component: SmtpSettings,
});

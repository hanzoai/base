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

    if (settings.isPending) return <div className="muted">Loading...</div>;
    if (settings.error) return <div className="danger">{ String(settings.error) }</div>;

    return (
        <div className="page">
            <SectionCard title="Sender" description="Default sender name and address for outgoing emails.">
                <form onSubmit={ submitMeta(() => {}) } className="grid">
                    <label className="field">
                        <span className="field__label">Sender name</span>
                        <input { ...regMeta('senderName', { required: true }) } className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Sender address</span>
                        <input { ...regMeta('senderAddress', { required: true }) } type="email" className="input" />
                    </label>
                </form>
            </SectionCard>

            <SectionCard title="SMTP" description="Configure SMTP mail server for sending emails.">
                <form onSubmit={ submitSmtp(handleSave) } className="stack">
                    <label className="field field--inline">
                        <input { ...regSmtp('enabled') } type="checkbox"  />
                        <span>Use SMTP mail server (recommended)</span>
                    </label>

                    { smtpEnabled && (
                        <>
                            <div className="grid" style={{ '--cols': 4 } as React.CSSProperties}>
                                <label className="field span-2">
                                    <span className="field__label">Host</span>
                                    <input { ...regSmtp('host', { required: true }) } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Port</span>
                                    <input { ...regSmtp('port', { required: true, valueAsNumber: true }) } type="number" className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">TLS</span>
                                    <select { ...regSmtp('tls') } className="input">
                                        <option value="">Auto (StartTLS)</option>
                                        <option value="true">Always</option>
                                    </select>
                                </label>
                            </div>
                            <div className="grid">
                                <label className="field">
                                    <span className="field__label">Username</span>
                                    <input { ...regSmtp('username') } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Password</span>
                                    <input { ...regSmtp('password') } type="password" className="input" />
                                </label>
                            </div>
                            <div className="grid">
                                <label className="field">
                                    <span className="field__label">AUTH method</span>
                                    <select { ...regSmtp('authMethod') } className="input">
                                        { authMethods.map((m) => (
                                            <option key={ m.value } value={ m.value }>{ m.label }</option>
                                        )) }
                                    </select>
                                </label>
                                <label className="field">
                                    <span className="field__label">EHLO/HELO domain</span>
                                    <input { ...regSmtp('localName') } placeholder="localhost" className="input" />
                                </label>
                            </div>
                        </>
                    ) }

                    <div className="row">
                        <button type="submit" disabled={ !fsSmtp.isDirty || saveMutation.isPending } className="btn">
                            { saveMutation.isPending ? 'Saving...' : 'Save changes' }
                        </button>
                        { saveMutation.isSuccess && <span className="ok small">Saved.</span> }
                        { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>

            <SectionCard title="Test email" description="Send a test email to verify your SMTP configuration.">
                <button type="button" onClick={ () => setTestOpen(!testOpen) } className="btn btn--outline">
                    { testOpen ? 'Hide test form' : 'Send test email' }
                </button>
                { testOpen && (
                    <form onSubmit={ submitTest((d) => testMutation.mutate(d)) } className="stack">
                        <div className="grid">
                            <label className="field">
                                <span className="field__label">To email</span>
                                <input { ...regTest('toEmail', { required: true }) } type="email" className="input" />
                            </label>
                            <label className="field">
                                <span className="field__label">Template</span>
                                <select { ...regTest('template') } className="input">
                                    { templateOptions.map((t) => (
                                        <option key={ t.value } value={ t.value }>{ t.label }</option>
                                    )) }
                                </select>
                            </label>
                        </div>
                        <label className="field">
                            <span className="field__label">Auth collection</span>
                            <input { ...regTest('collection') } className="input" placeholder="_superusers" />
                        </label>
                        <div className="row">
                            <button type="submit" disabled={ fsTest.isSubmitting || testMutation.isPending } className="btn">
                                { testMutation.isPending ? 'Sending...' : 'Send' }
                            </button>
                            { testStatus && (
                                <span className={ testMutation.isError ? 'danger small' : 'ok small' }>
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

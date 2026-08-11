import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState, useMemo } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface TemplateForm {
    subject: string;
    body: string;
    actionUrl: string;
}

const templateKeys = [
    { key: 'verificationTemplate', label: 'Verification' },
    { key: 'resetPasswordTemplate', label: 'Password reset' },
    { key: 'confirmEmailChangeTemplate', label: 'Email change' },
] as const;

type TemplateKey = typeof templateKeys[number]['key'];

function MailSettings() {
    const qc = useQueryClient();
    const [selectedCollection, setSelectedCollection] = useState<string>('');
    const [activeTemplate, setActiveTemplate] = useState<TemplateKey>('verificationTemplate');

    const authCollections = useQuery({
        queryKey: ['collections', 'auth'],
        queryFn: () => base.collections.getFullList({ filter: "type='auth'" }),
    });

    // Select first auth collection by default
    const collections = authCollections.data ?? [];
    const collectionId = selectedCollection || collections[0]?.id || '';
    const collection = collections.find((c) => c.id === collectionId);

    const templateData = collection
        ? (collection as Record<string, unknown>)[activeTemplate] as { subject?: string; body?: string; actionUrl?: string } | undefined
        : undefined;

    const { register, handleSubmit, formState, watch, reset } = useForm<TemplateForm>({
        values: {
            subject: templateData?.subject ?? '',
            body: templateData?.body ?? '',
            actionUrl: templateData?.actionUrl ?? '',
        },
    });

    const bodyValue = watch('body');

    const previewHtml = useMemo(() => {
        return bodyValue
            .replace(/\{APP_NAME\}/g, 'App Name')
            .replace(/\{APP_URL\}/g, 'https://example.com')
            .replace(/\{TOKEN\}/g, 'test-token-xxx')
            .replace(/\{ACTION_URL\}/g, '#')
            .replace(/\{OTP\}/g, '123456')
            .replace(/\{RECORD:.*?\}/g, 'value');
    }, [bodyValue]);

    const saveMutation = useMutation({
        mutationFn: async (data: TemplateForm) => {
            await base.collections.update(collectionId, {
                [activeTemplate]: {
                    subject: data.subject,
                    body: data.body,
                    actionUrl: data.actionUrl,
                },
            });
        },
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ['collections', 'auth'] });
        },
    });

    if (authCollections.isPending) return <div className="muted">Loading...</div>;

    if (collections.length === 0) {
        return <div className="muted">No auth collections found.</div>;
    }

    return (
        <div className="page">
            <SectionCard title="Mail templates" description="Edit email templates for auth collections.">
                <div className="row">
                    <select
                        value={ collectionId }
                        onChange={ (e) => { setSelectedCollection(e.target.value); reset(); } }
                        className="select"
                    >
                        { collections.map((c) => (
                            <option key={ c.id } value={ c.id }>{ c.name }</option>
                        )) }
                    </select>

                    <div className="row row--tight">
                        { templateKeys.map((t) => (
                            <button
                                key={ t.key }
                                type="button"
                                onClick={ () => { setActiveTemplate(t.key); reset(); } }
                                className="chip"
                                data-active={ activeTemplate === t.key ? 'true' : undefined }
                            >
                                { t.label }
                            </button>
                        )) }
                    </div>
                </div>

                <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) }>
                    <div className="grid">
                        <div className="stack">
                            <label className="field">
                                <span className="field__label">Subject</span>
                                <input { ...register('subject', { required: true }) } className="input" />
                            </label>
                            <label className="field">
                                <span className="field__label">Action URL</span>
                                <input { ...register('actionUrl') } className="input" placeholder="{APP_URL}" />
                            </label>
                            <label className="field">
                                <span className="field__label">Body (HTML)</span>
                                <textarea
                                    { ...register('body', { required: true }) }
                                    rows={ 14 }
                                    spellCheck={ false }
                                    className="textarea textarea--mono"
                                />
                            </label>
                            <div className="muted small">
                                Placeholders: {'{'} APP_NAME {'}'}, {'{'} APP_URL {'}'}, {'{'} TOKEN {'}'}, {'{'} ACTION_URL {'}'}, {'{'} OTP {'}'}
                            </div>
                        </div>

                        <div className="field">
                            <span className="muted">Preview</span>
                            <iframe
                                sandbox=""
                                srcDoc={ previewHtml }
                                className="preview"
                                title="Mail template preview"
                                style={ { minHeight: '320px' } }
                            />
                        </div>
                    </div>

                    <div className="row">
                        <button type="submit" disabled={ !formState.isDirty || saveMutation.isPending } className="btn">
                            { saveMutation.isPending ? 'Saving...' : 'Save template' }
                        </button>
                        { saveMutation.isSuccess && <span className="ok small">Saved.</span> }
                        { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/mail')({
    component: MailSettings,
});

import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

const tokenTypes = [
    { key: 'authToken', label: 'Auth token', scope: 'collection' },
    { key: 'verificationToken', label: 'Verification token', scope: 'collection' },
    { key: 'passwordResetToken', label: 'Password reset token', scope: 'collection' },
    { key: 'emailChangeToken', label: 'Email change token', scope: 'collection' },
    { key: 'fileToken', label: 'File token', scope: 'collection' },
] as const;

type TokenTypeKey = typeof tokenTypes[number]['key'];

interface TokenForm {
    duration: number;
}

function TokenSettings() {
    const qc = useQueryClient();
    const [selectedCollection, setSelectedCollection] = useState<string>('');
    const [activeToken, setActiveToken] = useState<TokenTypeKey>('authToken');

    const authCollections = useQuery({
        queryKey: ['collections', 'auth'],
        queryFn: () => base.collections.getFullList({ filter: "type='auth'" }),
    });

    const collections = authCollections.data ?? [];
    const collectionId = selectedCollection || collections[0]?.id || '';
    const collection = collections.find((c) => c.id === collectionId);

    const tokenConfig = collection
        ? (collection as Record<string, unknown>)[activeToken] as { duration?: number; secret?: string } | undefined
        : undefined;

    const { register, handleSubmit, formState } = useForm<TokenForm>({
        values: { duration: tokenConfig?.duration ?? 0 },
    });

    const saveMutation = useMutation({
        mutationFn: async (data: TokenForm) => {
            await base.collections.update(collectionId, {
                [activeToken]: { duration: data.duration },
            });
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['collections', 'auth'] }); },
    });

    const regenerateMutation = useMutation({
        mutationFn: async () => {
            // Generate a random secret and update the token config
            const randomBytes = new Uint8Array(32);
            crypto.getRandomValues(randomBytes);
            const secret = Array.from(randomBytes).map((b) => b.toString(16).padStart(2, '0')).join('');
            await base.collections.update(collectionId, {
                [activeToken]: { secret },
            });
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['collections', 'auth'] }); },
    });

    if (authCollections.isPending) return <div className="muted">Loading...</div>;

    if (collections.length === 0) {
        return <div className="muted">No auth collections found.</div>;
    }

    return (
        <div className="page">
            <SectionCard title="Token options" description="Configure token duration and secrets per auth collection.">
                <div className="row">
                    <select
                        value={ collectionId }
                        onChange={ (e) => setSelectedCollection(e.target.value) }
                        className="select"
                    >
                        { collections.map((c) => (
                            <option key={ c.id } value={ c.id }>{ c.name }</option>
                        )) }
                    </select>
                </div>

                <div className="row row--wrap row--tight">
                    { tokenTypes.map((t) => (
                        <button
                            key={ t.key }
                            type="button"
                            onClick={ () => setActiveToken(t.key) }
                            className={
                                'rounded px-3 py-1 text-xs transition-colors ' +
                                (activeToken === t.key
                                    ? 'bg-neutral-700 text-neutral-100'
                                    : 'text-neutral-400 hover:bg-neutral-800')
                            }
                        >
                            { t.label }
                        </button>
                    )) }
                </div>

                <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="stack">
                    <label className="field">
                        <span className="field__label">Duration (seconds)</span>
                        <input
                            { ...register('duration', { required: true, valueAsNumber: true, min: 0 }) }
                            type="number"
                            className="input"
                        />
                        <span className="muted small">
                            0 = use system default
                        </span>
                    </label>

                    { tokenConfig?.secret && (
                        <div className="field field--inline">
                            <span className="field__label">Secret:</span>
                            <code className="code">
                                { tokenConfig.secret.slice(0, 8) }...
                            </code>
                        </div>
                    ) }

                    <div className="row">
                        <button type="submit" disabled={ !formState.isDirty || saveMutation.isPending } className="btn">
                            { saveMutation.isPending ? 'Saving...' : 'Save duration' }
                        </button>
                        <button
                            type="button"
                            onClick={ () => {
                                if (confirm('Regenerate secret? All existing tokens of this type will be invalidated.')) {
                                    regenerateMutation.mutate();
                                }
                            } }
                            disabled={ regenerateMutation.isPending }
                            className="btn btn--danger btn--sm"
                        >
                            { regenerateMutation.isPending ? 'Regenerating...' : 'Regenerate secret' }
                        </button>
                        { saveMutation.isSuccess && <span className="ok small">Saved.</span> }
                        { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                        { regenerateMutation.isSuccess && <span className="ok small">Secret regenerated.</span> }
                        { regenerateMutation.error && <span className="danger small">{ regenerateMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/tokens')({
    component: TokenSettings,
});

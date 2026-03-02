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

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';
const btnDanger = 'rounded bg-red-700 px-3 py-1 text-xs hover:bg-red-600 disabled:opacity-50';

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

    if (authCollections.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;

    if (collections.length === 0) {
        return <div className="text-sm text-neutral-500">No auth collections found.</div>;
    }

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Token options" description="Configure token duration and secrets per auth collection.">
                <div className="mb-4 flex items-center gap-3">
                    <select
                        value={ collectionId }
                        onChange={ (e) => setSelectedCollection(e.target.value) }
                        className={ inputClass + ' max-w-xs' }
                    >
                        { collections.map((c) => (
                            <option key={ c.id } value={ c.id }>{ c.name }</option>
                        )) }
                    </select>
                </div>

                <div className="mb-4 flex flex-wrap gap-1">
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

                <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="flex flex-col gap-4">
                    <label className="flex flex-col gap-1 text-sm max-w-xs">
                        <span className="text-neutral-400">Duration (seconds)</span>
                        <input
                            { ...register('duration', { required: true, valueAsNumber: true, min: 0 }) }
                            type="number"
                            className={ inputClass }
                        />
                        <span className="text-xs text-neutral-600">
                            0 = use system default
                        </span>
                    </label>

                    { tokenConfig?.secret && (
                        <div className="flex items-center gap-2 text-sm">
                            <span className="text-neutral-500">Secret:</span>
                            <code className="rounded bg-neutral-800 px-2 py-0.5 text-xs text-neutral-400">
                                { tokenConfig.secret.slice(0, 8) }...
                            </code>
                        </div>
                    ) }

                    <div className="flex items-center gap-2 pt-2">
                        <button type="submit" disabled={ !formState.isDirty || saveMutation.isPending } className={ btnPrimary }>
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
                            className={ btnDanger }
                        >
                            { regenerateMutation.isPending ? 'Regenerating...' : 'Regenerate secret' }
                        </button>
                        { saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span> }
                        { saveMutation.error && <span className="text-xs text-red-400">{ saveMutation.error.message }</span> }
                        { regenerateMutation.isSuccess && <span className="text-xs text-green-400">Secret regenerated.</span> }
                        { regenerateMutation.error && <span className="text-xs text-red-400">{ regenerateMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/tokens')({
    component: TokenSettings,
});

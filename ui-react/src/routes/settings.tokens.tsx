import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

// The tokens Base issues. `auth` comes back from a sign-in refresh, `file`
// from POST /v1/files/token. Their signing secrets are server material — the
// server mints them and never sends them here, so this page sets durations and
// asks for a rotation.
const tokens = [
    { key: 'authToken', label: 'Auth token' },
    { key: 'fileToken', label: 'File token' },
] as const;

type TokenKey = typeof tokens[number]['key'];

interface DurationForm {
    duration: number;
}

function TokenSettings() {
    const qc = useQueryClient();
    const [selectedCollection, setSelectedCollection] = useState<string>('');
    const [active, setActive] = useState<TokenKey>('authToken');

    const authCollections = useQuery({
        queryKey: ['collections', 'auth'],
        queryFn: () => base.collections.getFullList({ filter: "type='auth'" }),
    });

    const collections = authCollections.data ?? [];
    const collectionId = selectedCollection || collections[0]?.id || '';
    const collection = collections.find((c) => c.id === collectionId);

    const config = collection
        ? (collection as Record<string, unknown>)[active] as { duration?: number } | undefined
        : undefined;

    const { register, handleSubmit, formState } = useForm<DurationForm>({
        values: { duration: config?.duration ?? 0 },
    });

    const saveDuration = useMutation({
        mutationFn: async (data: DurationForm) => {
            await base.collections.update(collectionId, { [active]: { duration: data.duration } });
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['collections', 'auth'] }); },
    });

    const rotate = useMutation({
        mutationFn: () => base.collections.rotate(collectionId),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['collections', 'auth'] }); },
    });

    if (authCollections.isPending) return <div className="muted">Loading...</div>;

    if (collections.length === 0) {
        return <div className="muted">No auth collections found.</div>;
    }

    return (
        <div className="page">
            <SectionCard title="Token options" description="How long each token stays valid, per auth collection.">
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
                    { tokens.map((t) => (
                        <button
                            key={ t.key }
                            type="button"
                            onClick={ () => setActive(t.key) }
                            className="chip"
                            data-active={ active === t.key ? 'true' : undefined }
                        >
                            { t.label }
                        </button>
                    )) }
                </div>

                <form onSubmit={ handleSubmit((d) => saveDuration.mutate(d)) } className="stack">
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

                    <div className="row">
                        <button type="submit" disabled={ !formState.isDirty || saveDuration.isPending } className="btn">
                            { saveDuration.isPending ? 'Saving...' : 'Save duration' }
                        </button>
                        { saveDuration.isSuccess && <span className="ok small">Saved.</span> }
                        { saveDuration.error && <span className="danger small">{ saveDuration.error.message }</span> }
                    </div>
                </form>
            </SectionCard>

            <SectionCard
                title="Signing secrets"
                description="The server mints the secrets that sign this collection's tokens. Rotating them is how you invalidate what is already out."
            >
                <div className="row">
                    <button
                        type="button"
                        onClick={ () => {
                            if (confirm(`Rotate the signing secrets for "${collection?.name}"? Every token it has issued stops working.`)) {
                                rotate.mutate();
                            }
                        } }
                        disabled={ rotate.isPending }
                        className="btn btn--danger btn--sm"
                    >
                        { rotate.isPending ? 'Rotating...' : 'Rotate secrets' }
                    </button>
                    { rotate.isSuccess && <span className="ok small">Rotated.</span> }
                    { rotate.error && <span className="danger small">{ rotate.error.message }</span> }
                </div>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/tokens')({
    component: TokenSettings,
});

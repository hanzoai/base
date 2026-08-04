import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface ProviderForm {
    clientId: string;
    clientSecret: string;
    authURL: string;
    tokenURL: string;
    userInfoURL: string;
    displayName: string;
}

const knownProviders = [
    'google', 'github', 'apple', 'discord', 'microsoft', 'facebook',
    'gitlab', 'twitter', 'spotify', 'twitch', 'strava', 'kakao',
    'livechat', 'gitee', 'gitea', 'bitbucket', 'patreon', 'mailcow',
    'vk', 'yandex', 'oidc', 'oidc2', 'oidc3',
] as const;

function AuthSettings() {
    const qc = useQueryClient();
    const [editing, setEditing] = useState<string | null>(null);

    const settings = useQuery({
        queryKey: ['settings'],
        queryFn: () => base.settings.getAll(),
    });

    // Auth collections to find configured providers
    const authCollections = useQuery({
        queryKey: ['collections', 'auth'],
        queryFn: () => base.collections.getFullList({ filter: "type='auth'" }),
    });

    // Gather all provider configs across all auth collections
    const providerMap = new Map<string, { collectionId: string; collectionName: string; config: Record<string, unknown> }>();
    for (const col of authCollections.data ?? []) {
        if (col.type !== 'auth') continue;
        const oauth2 = (col as Record<string, unknown>).oauth2 as { enabled?: boolean; providers?: Array<Record<string, unknown>> } | undefined;
        if (!oauth2?.providers) continue;
        for (const p of oauth2.providers) {
            const name = p.name as string;
            if (name) {
                providerMap.set(`${col.name}:${name}`, {
                    collectionId: col.id,
                    collectionName: col.name,
                    config: p,
                });
            }
        }
    }

    if (settings.isPending || authCollections.isPending) {
        return <div className="muted">Loading...</div>;
    }

    return (
        <div className="page">
            <SectionCard title="OAuth2 providers" description="Configure OAuth2 / OIDC providers for auth collections.">
                { authCollections.data?.filter((c) => c.type === 'auth').map((col) => {
                    const oauth2 = (col as Record<string, unknown>).oauth2 as { enabled?: boolean; providers?: Array<Record<string, unknown>> } | undefined;
                    const providers = oauth2?.providers ?? [];

                    return (
                        <div key={ col.id } className="stack">
                            <h3 className="card__title">
                                { col.name }
                                <span className="muted small">
                                    OAuth2 { oauth2?.enabled ? 'enabled' : 'disabled' }
                                </span>
                            </h3>

                            <div className="chips">
                                { knownProviders.map((provName) => {
                                    const existing = providers.find((p) => p.name === provName);
                                    const configured = existing && (existing.clientId as string);
                                    const key = `${col.name}:${provName}`;

                                    return (
                                        <button
                                            key={ provName }
                                            type="button"
                                            onClick={ () => setEditing(editing === key ? null : key) }
                                            className="chip"
                                            data-active={ editing === key ? 'true' : undefined }
                                            data-configured={ configured ? 'true' : undefined }
                                        >
                                            { provName }
                                        </button>
                                    );
                                }) }
                            </div>

                            { editing && editing.startsWith(col.name + ':') && (
                                <ProviderEditor
                                    collectionId={ col.id }
                                    collectionName={ col.name }
                                    providerName={ editing.split(':')[1] }
                                    existing={ providers.find((p) => p.name === editing.split(':')[1]) }
                                    onClose={ () => setEditing(null) }
                                    onSaved={ () => {
                                        void qc.invalidateQueries({ queryKey: ['collections', 'auth'] });
                                        setEditing(null);
                                    } }
                                />
                            ) }
                        </div>
                    );
                }) }

                { (!authCollections.data || authCollections.data.filter((c) => c.type === 'auth').length === 0) && (
                    <div className="muted">No auth collections found.</div>
                ) }
            </SectionCard>
        </div>
    );
}

function ProviderEditor({
    collectionId,
    collectionName,
    providerName,
    existing,
    onClose,
    onSaved,
}: {
    collectionId: string;
    collectionName: string;
    providerName: string;
    existing: Record<string, unknown> | undefined;
    onClose: () => void;
    onSaved: () => void;
}) {
    const { register, handleSubmit } = useForm<ProviderForm>({
        defaultValues: {
            clientId: (existing?.clientId as string) ?? '',
            clientSecret: (existing?.clientSecret as string) ?? '',
            authURL: (existing?.authURL as string) ?? '',
            tokenURL: (existing?.tokenURL as string) ?? '',
            userInfoURL: (existing?.userInfoURL as string) ?? '',
            displayName: (existing?.displayName as string) ?? providerName,
        },
    });

    const saveMutation = useMutation({
        mutationFn: async (data: ProviderForm) => {
            // Fetch the current collection, update the provider entry, save back
            const col = await base.collections.getOne(collectionId);
            const oauth2 = (col as Record<string, unknown>).oauth2 as {
                enabled?: boolean;
                providers?: Array<Record<string, unknown>>;
                mappedFields?: Record<string, string>;
            };

            const providers = [...(oauth2?.providers ?? [])];
            const idx = providers.findIndex((p) => p.name === providerName);
            const entry: Record<string, unknown> = {
                name: providerName,
                clientId: data.clientId,
                authURL: data.authURL,
                tokenURL: data.tokenURL,
                userInfoURL: data.userInfoURL,
                displayName: data.displayName,
            };
            // Never send the redacted placeholder back — omit to preserve the real secret.
            if (data.clientSecret !== (existing?.clientSecret as string)) {
                entry.clientSecret = data.clientSecret;
            }

            if (idx >= 0) {
                providers[idx] = { ...providers[idx], ...entry };
            } else {
                providers.push(entry);
            }

            await base.collections.update(collectionId, {
                oauth2: {
                    ...oauth2,
                    enabled: true,
                    providers,
                },
            });
        },
        onSuccess: onSaved,
    });

    return (
        <div className="card stack">
            <div className="row">
                <h4>
                    { collectionName } / { providerName }
                </h4>
                <button onClick={ onClose } className="link small">
                    Close
                </button>
            </div>
            <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="stack">
                <div className="grid">
                    <label className="field">
                        <span className="field__label">Client ID</span>
                        <input { ...register('clientId', { required: true }) } className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Client secret</span>
                        <input { ...register('clientSecret', { required: true }) } type="password" className="input" />
                    </label>
                </div>
                <div className="grid">
                    <label className="field">
                        <span className="field__label">Auth URL</span>
                        <input { ...register('authURL') } className="input" placeholder="Auto-detected if empty" />
                    </label>
                    <label className="field">
                        <span className="field__label">Token URL</span>
                        <input { ...register('tokenURL') } className="input" placeholder="Auto-detected if empty" />
                    </label>
                </div>
                <label className="field">
                    <span className="field__label">User info URL</span>
                    <input { ...register('userInfoURL') } className="input" placeholder="Auto-detected if empty" />
                </label>
                <label className="field">
                    <span className="field__label">Display name</span>
                    <input { ...register('displayName') } className="input" />
                </label>
                <div className="row">
                    <button type="submit" disabled={ saveMutation.isPending } className="btn">
                        { saveMutation.isPending ? 'Saving...' : 'Save provider' }
                    </button>
                    { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                </div>
            </form>
        </div>
    );
}

export const Route = createFileRoute('/settings/auth')({
    component: AuthSettings,
});

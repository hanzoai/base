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

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm w-full';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';

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
        return <div className="text-sm text-neutral-400">Loading...</div>;
    }

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="OAuth2 providers" description="Configure OAuth2 / OIDC providers for auth collections.">
                { authCollections.data?.filter((c) => c.type === 'auth').map((col) => {
                    const oauth2 = (col as Record<string, unknown>).oauth2 as { enabled?: boolean; providers?: Array<Record<string, unknown>> } | undefined;
                    const providers = oauth2?.providers ?? [];

                    return (
                        <div key={ col.id } className="mb-6">
                            <h3 className="mb-2 text-sm font-medium text-neutral-300">
                                { col.name }
                                <span className="ml-2 text-xs text-neutral-500">
                                    OAuth2 { oauth2?.enabled ? 'enabled' : 'disabled' }
                                </span>
                            </h3>

                            <div className="grid grid-cols-3 gap-2 sm:grid-cols-4 lg:grid-cols-6">
                                { knownProviders.map((provName) => {
                                    const existing = providers.find((p) => p.name === provName);
                                    const configured = existing && (existing.clientId as string);
                                    const key = `${col.name}:${provName}`;

                                    return (
                                        <button
                                            key={ provName }
                                            type="button"
                                            onClick={ () => setEditing(editing === key ? null : key) }
                                            className={
                                                'rounded border px-3 py-2 text-xs transition-colors ' +
                                                (configured
                                                    ? 'border-green-700 bg-green-900/30 text-green-300 hover:bg-green-900/50'
                                                    : 'border-neutral-700 text-neutral-400 hover:bg-neutral-800')
                                            }
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
                    <div className="text-sm text-neutral-500">No auth collections found.</div>
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
        <div className="mt-3 rounded border border-neutral-700 bg-neutral-900 p-4">
            <div className="mb-3 flex items-center justify-between">
                <h4 className="text-sm font-medium">
                    { collectionName } / { providerName }
                </h4>
                <button onClick={ onClose } className="text-xs text-neutral-500 hover:text-neutral-300">
                    Close
                </button>
            </div>
            <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="flex flex-col gap-3">
                <div className="grid grid-cols-2 gap-3">
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Client ID</span>
                        <input { ...register('clientId', { required: true }) } className={ inputClass } />
                    </label>
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Client secret</span>
                        <input { ...register('clientSecret', { required: true }) } type="password" className={ inputClass } />
                    </label>
                </div>
                <div className="grid grid-cols-2 gap-3">
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Auth URL</span>
                        <input { ...register('authURL') } className={ inputClass } placeholder="Auto-detected if empty" />
                    </label>
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Token URL</span>
                        <input { ...register('tokenURL') } className={ inputClass } placeholder="Auto-detected if empty" />
                    </label>
                </div>
                <label className="flex flex-col gap-1 text-sm">
                    <span className="text-neutral-400">User info URL</span>
                    <input { ...register('userInfoURL') } className={ inputClass } placeholder="Auto-detected if empty" />
                </label>
                <label className="flex flex-col gap-1 text-sm">
                    <span className="text-neutral-400">Display name</span>
                    <input { ...register('displayName') } className={ inputClass } />
                </label>
                <div className="flex items-center gap-2 pt-1">
                    <button type="submit" disabled={ saveMutation.isPending } className={ btnPrimary }>
                        { saveMutation.isPending ? 'Saving...' : 'Save provider' }
                    </button>
                    { saveMutation.error && <span className="text-xs text-red-400">{ saveMutation.error.message }</span> }
                </div>
            </form>
        </div>
    );
}

export const Route = createFileRoute('/settings/auth')({
    component: AuthSettings,
});

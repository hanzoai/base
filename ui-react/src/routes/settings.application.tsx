import { createFileRoute, redirect } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { useFieldArray, useForm } from 'react-hook-form';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

// Application-level config: trusted-proxy headers + batch options.
// Split off from settings.tsx so it doesn't grow without bound.

interface AppForm {
    trustedProxy: {
        headers: string[];
        useLeftmostIp: boolean;
    };
    batch: {
        enabled: boolean;
        maxRequests: number;
        timeout: number;
        maxBodySize: number;
    };
}

function Application() {
    const qc = useQueryClient();
    const settings = useQuery({
        queryKey: [ 'settings' ],
        queryFn: () => base.settings.getAll(),
    });

    const { register, handleSubmit, reset, control, watch, formState } = useForm<AppForm>({
        defaultValues: {
            trustedProxy: { headers: [], useLeftmostIp: false },
            batch: { enabled: false, maxRequests: 50, timeout: 3, maxBodySize: 0 },
        },
    });

    useEffect(() => {
        if (!settings.data) return;
        const s = settings.data as unknown as { trustedProxy?: AppForm['trustedProxy']; batch?: AppForm['batch'] };
        reset({
            trustedProxy: {
                headers: s.trustedProxy?.headers ?? [],
                useLeftmostIp: s.trustedProxy?.useLeftmostIp ?? false,
            },
            batch: {
                enabled: s.batch?.enabled ?? false,
                maxRequests: s.batch?.maxRequests ?? 50,
                timeout: s.batch?.timeout ?? 3,
                maxBodySize: s.batch?.maxBodySize ?? 0,
            },
        });
    }, [ settings.data, reset ]);

    const headersFA = useFieldArray({ control, name: 'trustedProxy.headers' as never });
    const batchEnabled = watch('batch.enabled');

    const save = useMutation({
        mutationFn: (data: AppForm) => base.settings.update({
            trustedProxy: data.trustedProxy,
            batch: data.batch,
        }),
        onSuccess: () => qc.invalidateQueries({ queryKey: [ 'settings' ] }),
    });

    return (
        <form onSubmit={ handleSubmit((data) => save.mutate(data)) } className="page">
            <SectionCard title="Trusted proxy">
                <p className="muted small">
                    When Base sits behind a reverse proxy, it must know which request
                    headers carry the real client IP. Only list headers your proxy is
                    guaranteed to set — trusting a header your proxy doesn't overwrite
                    lets any client spoof its IP.
                </p>

                <div className="stack stack--tight">
                    <div className="muted">Trusted IP headers</div>
                    { headersFA.fields.length === 0 && (
                        <div className="muted small">None. Header-based rate limiting will fall back to RemoteAddr.</div>
                    ) }
                    { headersFA.fields.map((field, i) => (
                        <div key={ field.id } className="row">
                            <input
                                { ...register(`trustedProxy.headers.${ i }` as const) }
                                placeholder="X-Forwarded-For"
                                className="input input--mono grow"
                            />
                            <button
                                type="button"
                                onClick={ () => headersFA.remove(i) }
                                className="link link--danger small"
                            >
                                Remove
                            </button>
                        </div>
                    )) }
                    <button
                        type="button"
                        onClick={ () => headersFA.append('' as never) }
                        className="btn btn--outline btn--sm"
                    >
                        Add header
                    </button>
                </div>

                <label className="field field--inline">
                    <input type="checkbox" { ...register('trustedProxy.useLeftmostIp') } />
                    <span>Use leftmost IP (vs rightmost) when the header lists multiple</span>
                </label>
            </SectionCard>

            <SectionCard title="Batch requests">
                <p className="muted small">
                    Clients can bundle multiple API calls into one HTTP request against
                    <code className="mx-1">/v1/batch</code>. Limits apply to the bundle
                    size; each sub-request still uses its own auth and rate limit.
                </p>

                <label className="field field--inline">
                    <input type="checkbox" { ...register('batch.enabled') } />
                    <span>Enable batch endpoint</span>
                </label>

                <div className="grid" data-disabled={ batchEnabled ? undefined : 'true' }>
                    <label className="field">
                        <span className="field__label">Max requests per bundle</span>
                        <input type="number" { ...register('batch.maxRequests', { valueAsNumber: true }) }
                            className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Timeout (seconds)</span>
                        <input type="number" { ...register('batch.timeout', { valueAsNumber: true }) }
                            className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Max body size (bytes, 0=unlimited)</span>
                        <input type="number" { ...register('batch.maxBodySize', { valueAsNumber: true }) }
                            className="input" />
                    </label>
                </div>
            </SectionCard>

            <div className="row">
                <button
                    type="submit"
                    disabled={ !formState.isDirty || save.isPending }
                    className="btn"
                >
                    { save.isPending ? 'Saving…' : 'Save application settings' }
                </button>
                { save.isSuccess && <span className="ok small">Saved.</span> }
                { save.error && <span className="danger small">{ String(save.error) }</span> }
            </div>
        </form>
    );
}

export const Route = createFileRoute('/settings/application')({
    beforeLoad: () => {
        if (!base.authStore.isValid || !base.authStore.isSuperuser) throw redirect({ to: '/login' });
    },
    component: Application,
});

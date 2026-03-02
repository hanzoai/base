import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface RateLimitRule {
    label: string;
    maxRequests: number;
    duration: number;
    audience: string;
}

interface RateLimitSettings {
    enabled: boolean;
    rules: RateLimitRule[];
}

const audienceOptions = [
    { value: '', label: 'All' },
    { value: '@guest', label: 'Guest only' },
    { value: '@auth', label: 'Auth only' },
];

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';
const btnSecondary = 'rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800';

function emptyRule(): RateLimitRule {
    return { label: '', maxRequests: 300, duration: 10, audience: '' };
}

function RateLimitsSettings() {
    const qc = useQueryClient();

    const settings = useQuery({
        queryKey: ['settings'],
        queryFn: () => base.settings.getAll(),
    });

    const initial = settings.data?.rateLimits as RateLimitSettings | undefined;

    const [enabled, setEnabled] = useState<boolean>(initial?.enabled ?? false);
    const [rules, setRules] = useState<RateLimitRule[]>(initial?.rules ?? []);
    const [dirty, setDirty] = useState(false);

    // Sync on fresh fetch
    const prevRef = useState({ synced: false })[0];
    if (initial && !prevRef.synced) {
        prevRef.synced = true;
        setEnabled(initial.enabled ?? false);
        setRules(initial.rules ?? []);
    }

    function addRule() {
        setRules([...rules, emptyRule()]);
        setDirty(true);
        if (rules.length === 0) setEnabled(true);
    }

    function removeRule(i: number) {
        const next = rules.filter((_, idx) => idx !== i);
        setRules(next);
        setDirty(true);
        if (next.length === 0) setEnabled(false);
    }

    function updateRule(i: number, field: keyof RateLimitRule, value: string | number) {
        const next = [...rules];
        next[i] = { ...next[i], [field]: value };
        setRules(next);
        setDirty(true);
    }

    const saveMutation = useMutation({
        mutationFn: () =>
            base.settings.update({
                rateLimits: { enabled, rules },
            }),
        onSuccess: () => {
            setDirty(false);
            void qc.invalidateQueries({ queryKey: ['settings'] });
        },
    });

    if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;
    if (settings.error) return <div className="text-sm text-red-400">{ String(settings.error) }</div>;

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Rate limits" description="Configure per-route request rate limiting.">
                <label className="mb-4 flex items-center gap-2 text-sm">
                    <input
                        type="checkbox"
                        checked={ enabled }
                        onChange={ (e) => { setEnabled(e.target.checked); setDirty(true); } }
                        className="accent-indigo-500"
                    />
                    <span>Enable rate limiting</span>
                    <span className="text-xs text-neutral-600">(experimental)</span>
                </label>

                { rules.length > 0 && (
                    <table className="mb-4 w-full text-sm">
                        <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
                            <tr>
                                <th className="py-2">Label</th>
                                <th className="py-2">Max requests</th>
                                <th className="py-2">Interval (sec)</th>
                                <th className="py-2">Audience</th>
                                <th className="py-2" />
                            </tr>
                        </thead>
                        <tbody>
                            { rules.map((rule, i) => (
                                <tr key={ i } className="border-t border-neutral-800">
                                    <td className="py-1.5">
                                        <input
                                            value={ rule.label }
                                            onChange={ (e) => updateRule(i, 'label', e.target.value) }
                                            placeholder="tag or /path/"
                                            className={ inputClass + ' w-full' }
                                        />
                                    </td>
                                    <td className="py-1.5 px-1">
                                        <input
                                            type="number"
                                            min="1"
                                            value={ rule.maxRequests }
                                            onChange={ (e) => updateRule(i, 'maxRequests', parseInt(e.target.value, 10) || 1) }
                                            className={ inputClass + ' w-24' }
                                        />
                                    </td>
                                    <td className="py-1.5 px-1">
                                        <input
                                            type="number"
                                            min="1"
                                            value={ rule.duration }
                                            onChange={ (e) => updateRule(i, 'duration', parseInt(e.target.value, 10) || 1) }
                                            className={ inputClass + ' w-24' }
                                        />
                                    </td>
                                    <td className="py-1.5 px-1">
                                        <select
                                            value={ rule.audience }
                                            onChange={ (e) => updateRule(i, 'audience', e.target.value) }
                                            className={ inputClass }
                                        >
                                            { audienceOptions.map((o) => (
                                                <option key={ o.value } value={ o.value }>{ o.label }</option>
                                            )) }
                                        </select>
                                    </td>
                                    <td className="py-1.5 text-right">
                                        <button
                                            onClick={ () => removeRule(i) }
                                            className="text-xs text-red-400 hover:text-red-300"
                                            title="Remove rule"
                                        >
                                            Remove
                                        </button>
                                    </td>
                                </tr>
                            )) }
                        </tbody>
                    </table>
                ) }

                <div className="flex items-center gap-2">
                    <button type="button" onClick={ addRule } className={ btnSecondary }>
                        Add rule
                    </button>
                    <button
                        type="button"
                        onClick={ () => saveMutation.mutate() }
                        disabled={ !dirty || saveMutation.isPending }
                        className={ btnPrimary }
                    >
                        { saveMutation.isPending ? 'Saving...' : 'Save changes' }
                    </button>
                    { saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span> }
                    { saveMutation.error && <span className="text-xs text-red-400">{ saveMutation.error.message }</span> }
                </div>

                <div className="mt-4 rounded border border-neutral-800 p-3 text-xs text-neutral-500">
                    <p className="mb-1 font-medium text-neutral-400">Label format (resolved in order):</p>
                    <ol className="list-decimal pl-4 leading-relaxed">
                        <li>Exact tag: <code>users:create</code></li>
                        <li>Wildcard tag: <code>*:create</code></li>
                        <li>METHOD + exact path: <code>POST /v1/collections</code></li>
                        <li>METHOD + prefix path: <code>POST /v1/</code> (trailing slash)</li>
                        <li>Exact path: <code>/v1/collections</code></li>
                        <li>Prefix path: <code>/v1/</code> (trailing slash)</li>
                    </ol>
                </div>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/rate-limits')({
    component: RateLimitsSettings,
});

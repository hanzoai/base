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

    if (settings.isPending) return <div className="muted">Loading...</div>;
    if (settings.error) return <div className="danger">{ String(settings.error) }</div>;

    return (
        <div className="page">
            <SectionCard title="Rate limits" description="Configure per-route request rate limiting.">
                <label className="field field--inline">
                    <input
                        type="checkbox"
                        checked={ enabled }
                        onChange={ (e) => { setEnabled(e.target.checked); setDirty(true); } }
                        
                    />
                    <span>Enable rate limiting</span>
                    <span className="muted small">(experimental)</span>
                </label>

                { rules.length > 0 && (
                    <table className="table">
                        <thead>
                            <tr>
                                <th>Label</th>
                                <th>Max requests</th>
                                <th>Interval (sec)</th>
                                <th>Audience</th>
                                <th />
                            </tr>
                        </thead>
                        <tbody>
                            { rules.map((rule, i) => (
                                <tr key={ i }>
                                    <td>
                                        <input
                                            value={ rule.label }
                                            onChange={ (e) => updateRule(i, 'label', e.target.value) }
                                            placeholder="tag or /path/"
                                            className="input"
                                        />
                                    </td>
                                    <td>
                                        <input
                                            type="number"
                                            min="1"
                                            value={ rule.maxRequests }
                                            onChange={ (e) => updateRule(i, 'maxRequests', parseInt(e.target.value, 10) || 1) }
                                            className="input"
                                        />
                                    </td>
                                    <td>
                                        <input
                                            type="number"
                                            min="1"
                                            value={ rule.duration }
                                            onChange={ (e) => updateRule(i, 'duration', parseInt(e.target.value, 10) || 1) }
                                            className="input"
                                        />
                                    </td>
                                    <td>
                                        <select
                                            value={ rule.audience }
                                            onChange={ (e) => updateRule(i, 'audience', e.target.value) }
                                            className="input"
                                        >
                                            { audienceOptions.map((o) => (
                                                <option key={ o.value } value={ o.value }>{ o.label }</option>
                                            )) }
                                        </select>
                                    </td>
                                    <td align="right">
                                        <button
                                            onClick={ () => removeRule(i) }
                                            className="link link--danger small"
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

                <div className="row">
                    <button type="button" onClick={ addRule } className="btn btn--outline">
                        Add rule
                    </button>
                    <button
                        type="button"
                        onClick={ () => saveMutation.mutate() }
                        disabled={ !dirty || saveMutation.isPending }
                        className="btn"
                    >
                        { saveMutation.isPending ? 'Saving...' : 'Save changes' }
                    </button>
                    { saveMutation.isSuccess && <span className="ok small">Saved.</span> }
                    { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                </div>

                <div className="card muted small">
                    <p>Label format (resolved in order):</p>
                    <ol>
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

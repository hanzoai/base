import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface LogSettingsForm {
    logMaxDays: number;
    logMinLevel: number;
    logIP: boolean;
}

const logLevels = [
    { value: 0, label: 'Default' },
    { value: -4, label: 'DEBUG (-4)' },
    { value: 0, label: 'INFO (0)' },
    { value: 4, label: 'WARN (4)' },
    { value: 8, label: 'ERROR (8)' },
] as const;

function LogSettings() {
    const qc = useQueryClient();

    const settings = useQuery({
        queryKey: ['settings'],
        queryFn: () => base.settings.getAll(),
    });

    const logs = settings.data?.logs as Record<string, unknown> | undefined;

    const { register, handleSubmit, formState } = useForm<LogSettingsForm>({
        values: {
            logMaxDays: (logs?.maxDays as number) ?? 7,
            logMinLevel: (logs?.minLevel as number) ?? 0,
            logIP: (logs?.logIP as boolean) ?? true,
        },
    });

    const saveMutation = useMutation({
        mutationFn: (data: LogSettingsForm) =>
            base.settings.update({
                logs: {
                    maxDays: data.logMaxDays,
                    minLevel: data.logMinLevel,
                    logIP: data.logIP,
                },
            }),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['settings'] }); },
    });

    if (settings.isPending) return <div className="muted">Loading...</div>;
    if (settings.error) return <div className="danger">{ String(settings.error) }</div>;

    return (
        <div className="page">
            <SectionCard title="Log settings" description="Configure log retention and minimum log level.">
                <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="stack">
                    <label className="field">
                        <span className="field__label">Log retention (days)</span>
                        <input
                            { ...register('logMaxDays', { required: true, valueAsNumber: true, min: 1 }) }
                            type="number"
                            min="1"
                            className="input"
                        />
                        <span className="muted small">Logs older than this will be auto-deleted. Set to 0 to keep indefinitely.</span>
                    </label>

                    <label className="field">
                        <span className="field__label">Minimum log level</span>
                        <select { ...register('logMinLevel', { valueAsNumber: true }) } className="input">
                            { logLevels.map((l, i) => (
                                <option key={ i } value={ l.value }>{ l.label }</option>
                            )) }
                        </select>
                        <span className="muted small">Only log entries at or above this level will be persisted.</span>
                    </label>

                    <label className="field field--inline">
                        <input { ...register('logIP') } type="checkbox"  />
                        <span>Log client IP addresses</span>
                    </label>

                    <div className="row">
                        <button type="submit" disabled={ !formState.isDirty || saveMutation.isPending } className="btn">
                            { saveMutation.isPending ? 'Saving...' : 'Save log settings' }
                        </button>
                        { saveMutation.isSuccess && <span className="ok small">Saved.</span> }
                        { saveMutation.error && <span className="danger small">{ saveMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/logs')({
    component: LogSettings,
});

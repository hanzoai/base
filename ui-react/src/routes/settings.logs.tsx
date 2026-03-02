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

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';

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

    if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;
    if (settings.error) return <div className="text-sm text-red-400">{ String(settings.error) }</div>;

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Log settings" description="Configure log retention and minimum log level.">
                <form onSubmit={ handleSubmit((d) => saveMutation.mutate(d)) } className="flex flex-col gap-4 max-w-md">
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Log retention (days)</span>
                        <input
                            { ...register('logMaxDays', { required: true, valueAsNumber: true, min: 1 }) }
                            type="number"
                            min="1"
                            className={ inputClass }
                        />
                        <span className="text-xs text-neutral-600">Logs older than this will be auto-deleted. Set to 0 to keep indefinitely.</span>
                    </label>

                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Minimum log level</span>
                        <select { ...register('logMinLevel', { valueAsNumber: true }) } className={ inputClass }>
                            { logLevels.map((l, i) => (
                                <option key={ i } value={ l.value }>{ l.label }</option>
                            )) }
                        </select>
                        <span className="text-xs text-neutral-600">Only log entries at or above this level will be persisted.</span>
                    </label>

                    <label className="flex items-center gap-2 text-sm">
                        <input { ...register('logIP') } type="checkbox" className="accent-indigo-500" />
                        <span>Log client IP addresses</span>
                    </label>

                    <div className="flex items-center gap-2 pt-2">
                        <button type="submit" disabled={ !formState.isDirty || saveMutation.isPending } className={ btnPrimary }>
                            { saveMutation.isPending ? 'Saving...' : 'Save log settings' }
                        </button>
                        { saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span> }
                        { saveMutation.error && <span className="text-xs text-red-400">{ saveMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/logs')({
    component: LogSettings,
});

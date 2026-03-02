import { createFileRoute, redirect } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { base } from '~/lib/base';

// Cron job surface. PocketBase exposes base.crons.* for the app's
// registered jobs. Each job is read-only metadata + a "run now" trigger.
function Crons() {
    const qc = useQueryClient();

    const list = useQuery({
        queryKey: [ 'crons' ],
        queryFn: () => base.crons.getFullList(),
    });

    const run = useMutation({
        mutationFn: (jobId: string) => base.crons.run(jobId),
        onSuccess: () => qc.invalidateQueries({ queryKey: [ 'crons' ] }),
    });

    return (
        <div className="flex flex-col gap-4">
            <header>
                <h2 className="text-lg font-semibold">Cron jobs</h2>
                <p className="text-xs text-neutral-400">
                    Registered app jobs. Each runs on its configured schedule; the
                    "Run now" button triggers the handler out-of-band.
                </p>
            </header>

            { list.isPending && <div className="text-sm text-neutral-400">Loading…</div> }
            { list.error && <div className="text-sm text-red-400">{ String(list.error) }</div> }

            { !list.isPending && list.data?.length === 0 && (
                <div className="text-sm text-neutral-500">No cron jobs registered.</div>
            ) }

            <table className="w-full text-sm">
                <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
                    <tr>
                        <th className="py-2">ID</th>
                        <th className="py-2">Schedule</th>
                        <th className="py-2" />
                    </tr>
                </thead>
                <tbody>
                    { list.data?.map((job) => (
                        <tr key={ job.id } className="border-t border-neutral-800 hover:bg-neutral-900">
                            <td className="py-2 font-mono text-xs">{ job.id }</td>
                            <td className="py-2 font-mono text-xs text-neutral-400">{ job.expression }</td>
                            <td className="py-2 text-right">
                                <button
                                    onClick={ () => run.mutate(job.id) }
                                    disabled={ run.isPending }
                                    className="text-xs text-indigo-400 hover:text-indigo-300 disabled:text-neutral-600"
                                >
                                    Run now
                                </button>
                            </td>
                        </tr>
                    )) }
                </tbody>
            </table>

            { run.isSuccess && <div className="text-xs text-green-400">Triggered.</div> }
        </div>
    );
}

export const Route = createFileRoute('/settings/crons')({
    beforeLoad: () => {
        if (!base.authStore.isValid || !base.authStore.isSuperuser) throw redirect({ to: '/login' });
    },
    component: Crons,
});

import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { base } from '~/lib/base';

// Cron job surface. Hanzo Base exposes base.crons.* for the app's
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
        <div className="stack">
            <header>
                <h2 className="page__title">Cron jobs</h2>
                <p className="muted small">
                    Registered app jobs. Each runs on its configured schedule; the
                    "Run now" button triggers the handler out-of-band.
                </p>
            </header>

            { list.isPending && <div className="muted">Loading…</div> }
            { list.error && <div className="danger">{ String(list.error) }</div> }

            { !list.isPending && list.data?.length === 0 && (
                <div className="muted">No cron jobs registered.</div>
            ) }

            <table className="table">
                <thead>
                    <tr>
                        <th>ID</th>
                        <th>Schedule</th>
                        <th />
                    </tr>
                </thead>
                <tbody>
                    { list.data?.map((job) => (
                        <tr key={ job.id }>
                            <td className="mono">{ job.id }</td>
                            <td className="mono muted">{ job.expression }</td>
                            <td align="right">
                                <button
                                    onClick={ () => run.mutate(job.id) }
                                    disabled={ run.isPending }
                                    className="link small"
                                >
                                    Run now
                                </button>
                            </td>
                        </tr>
                    )) }
                </tbody>
            </table>

            { run.isSuccess && <div className="ok small">Triggered.</div> }
        </div>
    );
}

export const Route = createFileRoute('/settings/crons')({ component: Crons });

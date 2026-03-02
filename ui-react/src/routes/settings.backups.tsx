import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface S3Form {
    enabled: boolean;
    endpoint: string;
    bucket: string;
    region: string;
    accessKey: string;
    secret: string;
    forcePathStyle: boolean;
}

interface BackupOptionsForm {
    cron: string;
    cronMaxKeep: number;
    s3: S3Form;
}

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';
const btnSecondary = 'rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800 disabled:opacity-50';

function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function BackupsSettings() {
    const qc = useQueryClient();
    const [showOptions, setShowOptions] = useState(false);
    const [newBackupName, setNewBackupName] = useState('');
    const [restoreTarget, setRestoreTarget] = useState<string | null>(null);

    const settings = useQuery({
        queryKey: ['settings'],
        queryFn: () => base.settings.getAll(),
    });

    const backupsList = useQuery({
        queryKey: ['backups'],
        queryFn: () => base.backups.getFullList(),
        select: (data) => [...data].sort((a, b) => (a.key < b.key ? 1 : -1)),
    });

    const backupsSettings = settings.data?.backups as Record<string, unknown> | undefined;
    const s3Config = (backupsSettings?.s3 ?? {}) as Record<string, unknown>;

    const { register, handleSubmit, formState, watch } = useForm<BackupOptionsForm>({
        values: {
            cron: (backupsSettings?.cron as string) ?? '',
            cronMaxKeep: (backupsSettings?.cronMaxKeep as number) ?? 5,
            s3: {
                enabled: (s3Config.enabled as boolean) ?? false,
                endpoint: (s3Config.endpoint as string) ?? '',
                bucket: (s3Config.bucket as string) ?? '',
                region: (s3Config.region as string) ?? '',
                accessKey: (s3Config.accessKey as string) ?? '',
                secret: (s3Config.secret as string) ?? '',
                forcePathStyle: (s3Config.forcePathStyle as boolean) ?? false,
            },
        },
    });

    const s3Enabled = watch('s3.enabled');

    const saveOptions = useMutation({
        mutationFn: (data: BackupOptionsForm) => {
            const s3Payload: Record<string, unknown> = { ...data.s3 };
            // Never send the redacted placeholder back — omit to preserve the real secret.
            if (s3Payload.secret === (s3Config.secret as string)) delete s3Payload.secret;
            return base.settings.update({ backups: { ...data, s3: s3Payload } });
        },
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['settings'] }); },
    });

    const createBackup = useMutation({
        mutationFn: (name: string) => base.backups.create(name),
        onSuccess: () => {
            setNewBackupName('');
            void qc.invalidateQueries({ queryKey: ['backups'] });
        },
    });

    const deleteBackup = useMutation({
        mutationFn: (key: string) => base.backups.delete(key),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['backups'] }); },
    });

    const restoreBackup = useMutation({
        mutationFn: (key: string) => base.backups.restore(key),
        onSuccess: () => { setRestoreTarget(null); },
    });

    const downloadBackup = useMutation({
        mutationFn: async (key: string) => {
            const token = await base.files.getToken();
            const url = base.backups.getDownloadURL(token, key);
            window.open(url, '_blank');
        },
    });

    if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Backups" description="Create, restore, download, or delete database backups.">
                <div className="mb-4 flex items-center gap-2">
                    <input
                        value={ newBackupName }
                        onChange={ (e) => setNewBackupName(e.target.value) }
                        placeholder="backup-name.zip"
                        className={ inputClass + ' flex-1' }
                    />
                    <button
                        onClick={ () => createBackup.mutate(newBackupName || `backup-${Date.now()}.zip`) }
                        disabled={ createBackup.isPending }
                        className={ btnPrimary }
                    >
                        { createBackup.isPending ? 'Creating...' : 'New backup' }
                    </button>
                    <button
                        onClick={ () => void qc.invalidateQueries({ queryKey: ['backups'] }) }
                        className={ btnSecondary }
                    >
                        Refresh
                    </button>
                </div>

                { createBackup.error && <div className="mb-2 text-xs text-red-400">{ createBackup.error.message }</div> }

                { backupsList.isPending && <div className="text-sm text-neutral-400">Loading backups...</div> }

                { backupsList.data && backupsList.data.length === 0 && (
                    <div className="text-sm text-neutral-500">No backups yet.</div>
                ) }

                { backupsList.data && backupsList.data.length > 0 && (
                    <table className="w-full text-sm">
                        <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
                            <tr>
                                <th className="py-2">Name</th>
                                <th className="py-2">Size</th>
                                <th className="py-2 text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            { backupsList.data.map((b) => (
                                <tr key={ b.key } className="border-t border-neutral-800 hover:bg-neutral-900">
                                    <td className="py-2 font-mono text-xs">{ b.key }</td>
                                    <td className="py-2 text-neutral-400">{ formatBytes(b.size) }</td>
                                    <td className="py-2 text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <button
                                                onClick={ () => downloadBackup.mutate(b.key) }
                                                className="text-xs text-indigo-400 hover:text-indigo-300"
                                            >
                                                Download
                                            </button>
                                            { restoreTarget === b.key ? (
                                                <>
                                                    <span className="text-xs text-yellow-400">Confirm restore?</span>
                                                    <button
                                                        onClick={ () => restoreBackup.mutate(b.key) }
                                                        disabled={ restoreBackup.isPending }
                                                        className="text-xs text-yellow-400 hover:text-yellow-300"
                                                    >
                                                        Yes
                                                    </button>
                                                    <button
                                                        onClick={ () => setRestoreTarget(null) }
                                                        className="text-xs text-neutral-500 hover:text-neutral-300"
                                                    >
                                                        No
                                                    </button>
                                                </>
                                            ) : (
                                                <button
                                                    onClick={ () => setRestoreTarget(b.key) }
                                                    className="text-xs text-yellow-400 hover:text-yellow-300"
                                                >
                                                    Restore
                                                </button>
                                            ) }
                                            <button
                                                onClick={ () => {
                                                    if (confirm(`Delete backup "${b.key}"?`)) deleteBackup.mutate(b.key);
                                                } }
                                                disabled={ deleteBackup.isPending }
                                                className="text-xs text-red-400 hover:text-red-300"
                                            >
                                                Delete
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            )) }
                        </tbody>
                    </table>
                ) }

                { restoreBackup.error && <div className="mt-2 text-xs text-red-400">{ restoreBackup.error.message }</div> }
            </SectionCard>

            <SectionCard title="Backup options" description="Configure auto backups and S3 storage.">
                <button
                    type="button"
                    onClick={ () => setShowOptions(!showOptions) }
                    className={ btnSecondary }
                >
                    { showOptions ? 'Hide options' : 'Show options' }
                </button>

                { showOptions && (
                    <form onSubmit={ handleSubmit((d) => saveOptions.mutate(d)) } className="mt-4 flex flex-col gap-4">
                        <div className="grid grid-cols-2 gap-4">
                            <label className="flex flex-col gap-1 text-sm">
                                <span className="text-neutral-400">Cron expression (UTC)</span>
                                <input { ...register('cron') } placeholder="0 0 * * *" className={ inputClass + ' font-mono' } />
                                <span className="text-xs text-neutral-600">Leave empty to disable auto backups</span>
                            </label>
                            <label className="flex flex-col gap-1 text-sm">
                                <span className="text-neutral-400">Max auto backups to keep</span>
                                <input { ...register('cronMaxKeep', { valueAsNumber: true }) } type="number" min="1" className={ inputClass } />
                            </label>
                        </div>

                        <label className="flex items-center gap-2 text-sm">
                            <input { ...register('s3.enabled') } type="checkbox" className="accent-indigo-500" />
                            <span>Store backups in S3 storage</span>
                        </label>

                        { s3Enabled && (
                            <div className="grid grid-cols-2 gap-4">
                                <label className="flex flex-col gap-1 text-sm col-span-2">
                                    <span className="text-neutral-400">Endpoint</span>
                                    <input { ...register('s3.endpoint', { required: true }) } className={ inputClass } />
                                </label>
                                <label className="flex flex-col gap-1 text-sm">
                                    <span className="text-neutral-400">Bucket</span>
                                    <input { ...register('s3.bucket', { required: true }) } className={ inputClass } />
                                </label>
                                <label className="flex flex-col gap-1 text-sm">
                                    <span className="text-neutral-400">Region</span>
                                    <input { ...register('s3.region', { required: true }) } className={ inputClass } />
                                </label>
                                <label className="flex flex-col gap-1 text-sm">
                                    <span className="text-neutral-400">Access key</span>
                                    <input { ...register('s3.accessKey', { required: true }) } className={ inputClass } />
                                </label>
                                <label className="flex flex-col gap-1 text-sm">
                                    <span className="text-neutral-400">Secret</span>
                                    <input { ...register('s3.secret', { required: true }) } type="password" className={ inputClass } />
                                </label>
                                <label className="col-span-2 flex items-center gap-2 text-sm">
                                    <input { ...register('s3.forcePathStyle') } type="checkbox" className="accent-indigo-500" />
                                    <span>Force path-style addressing</span>
                                </label>
                            </div>
                        ) }

                        <div className="flex items-center gap-2 pt-2">
                            <button type="submit" disabled={ !formState.isDirty || saveOptions.isPending } className={ btnPrimary }>
                                { saveOptions.isPending ? 'Saving...' : 'Save options' }
                            </button>
                            { saveOptions.isSuccess && <span className="text-xs text-green-400">Saved.</span> }
                            { saveOptions.error && <span className="text-xs text-red-400">{ saveOptions.error.message }</span> }
                        </div>
                    </form>
                ) }
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/backups')({
    component: BackupsSettings,
});

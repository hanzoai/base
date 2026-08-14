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
            // The server never sends the stored secret back, so the box starts
            // empty whatever is configured and an empty box means "leave it".
            // Sending it erased the secret — including on a save made with S3
            // switched off, where the field is not even on screen.
            if (!s3Payload.secret) delete s3Payload.secret;
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
            window.open(base.backups.getDownloadURL(key, token), '_blank');
        },
    });

    if (settings.isPending) return <div className="muted">Loading...</div>;

    return (
        <div className="page">
            <SectionCard title="Backups" description="Create, restore, download, or delete database backups.">
                <div className="row">
                    <input
                        value={ newBackupName }
                        onChange={ (e) => setNewBackupName(e.target.value) }
                        placeholder="backup-name.zip"
                        className="input grow"
                    />
                    <button
                        onClick={ () => createBackup.mutate(newBackupName || `backup-${Date.now()}.zip`) }
                        disabled={ createBackup.isPending }
                        className="btn"
                    >
                        { createBackup.isPending ? 'Creating...' : 'New backup' }
                    </button>
                    <button
                        onClick={ () => void qc.invalidateQueries({ queryKey: ['backups'] }) }
                        className="btn btn--outline"
                    >
                        Refresh
                    </button>
                </div>

                { createBackup.error && <div className="danger small">{ createBackup.error.message }</div> }

                { backupsList.isPending && <div className="muted">Loading backups...</div> }

                { backupsList.data && backupsList.data.length === 0 && (
                    <div className="muted">No backups yet.</div>
                ) }

                { backupsList.data && backupsList.data.length > 0 && (
                    <table className="table">
                        <thead>
                            <tr>
                                <th>Name</th>
                                <th>Size</th>
                                <th align="right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            { backupsList.data.map((b) => (
                                <tr key={ b.key }>
                                    <td className="mono">{ b.key }</td>
                                    <td className="muted">{ formatBytes(b.size) }</td>
                                    <td align="right">
                                        <div className="row">
                                            <button
                                                onClick={ () => downloadBackup.mutate(b.key) }
                                                className="link small"
                                            >
                                                Download
                                            </button>
                                            { restoreTarget === b.key ? (
                                                <>
                                                    <span className="warn small">Confirm restore?</span>
                                                    <button
                                                        onClick={ () => restoreBackup.mutate(b.key) }
                                                        disabled={ restoreBackup.isPending }
                                                        className="link small warn"
                                                    >
                                                        Yes
                                                    </button>
                                                    <button
                                                        onClick={ () => setRestoreTarget(null) }
                                                        className="link small"
                                                    >
                                                        No
                                                    </button>
                                                </>
                                            ) : (
                                                <button
                                                    onClick={ () => setRestoreTarget(b.key) }
                                                    className="link small warn"
                                                >
                                                    Restore
                                                </button>
                                            ) }
                                            <button
                                                onClick={ () => {
                                                    if (confirm(`Delete backup "${b.key}"?`)) deleteBackup.mutate(b.key);
                                                } }
                                                disabled={ deleteBackup.isPending }
                                                className="link link--danger small"
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

                { restoreBackup.error && <div className="danger small">{ restoreBackup.error.message }</div> }
            </SectionCard>

            <SectionCard title="Backup options" description="Configure auto backups and S3 storage.">
                <button
                    type="button"
                    onClick={ () => setShowOptions(!showOptions) }
                    className="btn btn--outline"
                >
                    { showOptions ? 'Hide options' : 'Show options' }
                </button>

                { showOptions && (
                    <form onSubmit={ handleSubmit((d) => saveOptions.mutate(d)) } className="stack">
                        <div className="grid">
                            <label className="field">
                                <span className="field__label">Cron expression (UTC)</span>
                                <input { ...register('cron') } placeholder="0 0 * * *" className="input input--mono" />
                                <span className="muted small">Leave empty to disable auto backups</span>
                            </label>
                            <label className="field">
                                <span className="field__label">Max auto backups to keep</span>
                                <input { ...register('cronMaxKeep', { valueAsNumber: true }) } type="number" min="1" className="input" />
                            </label>
                        </div>

                        <label className="field field--inline">
                            <input { ...register('s3.enabled') } type="checkbox"  />
                            <span>Store backups in S3 storage</span>
                        </label>

                        { s3Enabled && (
                            <div className="grid">
                                <label className="field span-2">
                                    <span className="field__label">Endpoint</span>
                                    <input { ...register('s3.endpoint', { required: true }) } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Bucket</span>
                                    <input { ...register('s3.bucket', { required: true }) } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Region</span>
                                    <input { ...register('s3.region', { required: true }) } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Access key</span>
                                    <input { ...register('s3.accessKey', { required: true }) } className="input" />
                                </label>
                                <label className="field">
                                    <span className="field__label">Secret</span>
                                    <input { ...register('s3.secret') } type="password" className="input" />
                                    <span className="muted small">Leave empty to keep the stored secret.</span>
                                </label>
                                <label className="field field--inline span-2">
                                    <input { ...register('s3.forcePathStyle') } type="checkbox"  />
                                    <span>Force path-style addressing</span>
                                </label>
                            </div>
                        ) }

                        <div className="row">
                            <button type="submit" disabled={ !formState.isDirty || saveOptions.isPending } className="btn">
                                { saveOptions.isPending ? 'Saving...' : 'Save options' }
                            </button>
                            { saveOptions.isSuccess && <span className="ok small">Saved.</span> }
                            { saveOptions.error && <span className="danger small">{ saveOptions.error.message }</span> }
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

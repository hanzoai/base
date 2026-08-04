import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface CreateSuperuserForm {
    email: string;
    password: string;
    passwordConfirm: string;
}

interface ChangePasswordForm {
    password: string;
    passwordConfirm: string;
}

function SuperusersSettings() {
    const qc = useQueryClient();
    const [changingPw, setChangingPw] = useState<string | null>(null);

    const superusers = useQuery({
        queryKey: ['superusers'],
        queryFn: () => base.collection('_superusers').getFullList({ sort: 'email' }),
    });

    const { register: regCreate, handleSubmit: submitCreate, formState: fsCreate, reset: resetCreate } = useForm<CreateSuperuserForm>();

    const createMutation = useMutation({
        mutationFn: (data: CreateSuperuserForm) =>
            base.collection('_superusers').create({
                email: data.email,
                password: data.password,
                passwordConfirm: data.passwordConfirm,
            }),
        onSuccess: () => {
            resetCreate();
            void qc.invalidateQueries({ queryKey: ['superusers'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: string) => base.collection('_superusers').delete(id),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['superusers'] }); },
    });

    return (
        <div className="page">
            <SectionCard title="Superusers" description="Manage superuser accounts.">
                { superusers.isPending && <div className="muted">Loading...</div> }
                { superusers.error && <div className="danger">{ String(superusers.error) }</div> }

                { superusers.data && (
                    <table className="table">
                        <thead>
                            <tr>
                                <th>Email</th>
                                <th>ID</th>
                                <th>Created</th>
                                <th align="right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            { superusers.data.map((su) => (
                                <tr key={ su.id }>
                                    <td>{ String(su.email ?? '') }</td>
                                    <td className="muted small">{ su.id }</td>
                                    <td className="muted small">{ su.created }</td>
                                    <td align="right">
                                        <div className="row">
                                            <button
                                                onClick={ () => setChangingPw(changingPw === su.id ? null : su.id) }
                                                className="link small"
                                            >
                                                { changingPw === su.id ? 'Cancel' : 'Change password' }
                                            </button>
                                            <button
                                                onClick={ () => {
                                                    if (confirm(`Delete superuser "${su.email}"?`)) deleteMutation.mutate(su.id);
                                                } }
                                                disabled={ deleteMutation.isPending }
                                                className="link link--danger small"
                                            >
                                                Delete
                                            </button>
                                        </div>
                                        { changingPw === su.id && (
                                            <ChangePasswordPanel
                                                superuserId={ su.id }
                                                onDone={ () => setChangingPw(null) }
                                            />
                                        ) }
                                    </td>
                                </tr>
                            )) }
                        </tbody>
                    </table>
                ) }

                { deleteMutation.error && <div className="danger small">{ deleteMutation.error.message }</div> }
            </SectionCard>

            <SectionCard title="Create superuser" description="Add a new superuser account.">
                <form onSubmit={ submitCreate((d) => createMutation.mutate(d)) } className="stack">
                    <label className="field">
                        <span className="field__label">Email</span>
                        <input { ...regCreate('email', { required: true }) } type="email" className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Password</span>
                        <input { ...regCreate('password', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className="input" />
                    </label>
                    <label className="field">
                        <span className="field__label">Confirm password</span>
                        <input { ...regCreate('passwordConfirm', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className="input" />
                    </label>
                    <div className="row">
                        <button type="submit" disabled={ !fsCreate.isDirty || createMutation.isPending } className="btn">
                            { createMutation.isPending ? 'Creating...' : 'Create superuser' }
                        </button>
                        { createMutation.isSuccess && <span className="ok small">Created.</span> }
                        { createMutation.error && <span className="danger small">{ createMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

function ChangePasswordPanel({ superuserId, onDone }: { superuserId: string; onDone: () => void }) {
    const { register, handleSubmit, formState } = useForm<ChangePasswordForm>();

    const mutation = useMutation({
        mutationFn: (data: ChangePasswordForm) =>
            base.collection('_superusers').update(superuserId, {
                password: data.password,
                passwordConfirm: data.passwordConfirm,
            }),
        onSuccess: onDone,
    });

    return (
        <form
            onSubmit={ handleSubmit((d) => mutation.mutate(d)) }
            className="card stack stack--tight"
        >
            <label className="field">
                <span className="field__label">New password</span>
                <input { ...register('password', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className="input" />
            </label>
            <label className="field">
                <span className="field__label">Confirm</span>
                <input { ...register('passwordConfirm', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className="input" />
            </label>
            <div className="row">
                <button type="submit" disabled={ !formState.isDirty || mutation.isPending } className="btn">
                    { mutation.isPending ? 'Saving...' : 'Update password' }
                </button>
                { mutation.error && <span className="danger small">{ mutation.error.message }</span> }
            </div>
        </form>
    );
}

export const Route = createFileRoute('/settings/superusers')({
    component: SuperusersSettings,
});

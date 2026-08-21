import { useState } from 'react';
import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { FUNCTION_NAME } from '~/lib/api';
import { base } from '~/lib/base';
import { requireSuperuser } from '~/lib/guard';

// Functions — the code this Base keeps and runs.
//
// A function is a record in `_functions`: its name is its id and its source is
// its body, so this page is the record path wearing a nicer address. Every
// control here is one call to /v1/functions and nothing is arranged locally —
// what the server answers is what the page says, including when the answer is
// that it will not run the thing.

// The starter body states the contract rather than describing it. A function
// declares `handler`, is handed the payload and the host, and answers without
// waiting — the three things the runtime checks before it will run anything.
const STARTER = `function handler(payload, base) {
  return { hello: (payload && payload.name) || 'world' }
}
`;

// What the server said, as far as it can be read off a rejected promise. The
// fetch layer throws with the server's own message on it, and that message is
// the whole point here: "No runtime is linked for js functions" and "the run
// did not start" are different facts about a deployment, and flattening them
// into "failed" would throw away the only part worth reading.
function said(err: unknown): string {
  const e = err as { status?: number; message?: string };
  const status = e?.status ? `${ e.status } — ` : '';
  return status + (e?.message || String(err));
}

// One draft: which function is open, and the two things a person edits. Held
// together because they change together — picking a different function
// replaces all three, and letting them drift is how an edit gets saved onto
// the function that was open before it.
interface Draft {
  name: string;
  source: string;
  // A function that has never been saved is created; one that has is updated,
  // and its name is its primary key and so cannot move.
  fresh: boolean;
}

function Functions() {
  const qc = useQueryClient();
  const [ draft, setDraft ] = useState<Draft | null>(null);
  const [ payload, setPayload ] = useState('{}');
  const [ ran, setRan ] = useState<{ ok: boolean; body: string } | null>(null);

  const list = useQuery({
    queryKey: [ 'functions' ],
    queryFn: () => base.functions.getFullList(),
  });

  const done = () => {
    void qc.invalidateQueries({ queryKey: [ 'functions' ] });
  };

  const save = useMutation({
    mutationFn: (d: Draft) =>
      d.fresh ? base.functions.create(d.name, d.source) : base.functions.update(d.name, d.source),
    onSuccess: (rec) => {
      setDraft({ name: rec.id, source: rec.source, fresh: false });
      done();
    },
  });

  const drop = useMutation({
    mutationFn: (name: string) => base.functions.delete(name),
    onSuccess: () => {
      setDraft(null);
      setRan(null);
      done();
    },
  });

  // Running is not a mutation of anything this page holds, so its answer is
  // kept as the answer it is: the function's own JSON, or the server's account
  // of why there is none.
  const run = useMutation({
    mutationFn: (d: Draft) => base.functions.invoke(d.name, payload),
    onSuccess: (out) => setRan({ ok: true, body: JSON.stringify(out, null, 2) }),
    onError: (err) => setRan({ ok: false, body: said(err) }),
  });

  const rows = list.data ?? [];
  const named = draft ? FUNCTION_NAME.test(draft.name) : false;
  // A fresh function may not take a name that is already in use: the server
  // refuses it, and saying so here costs nothing and reads better.
  const taken = Boolean(draft?.fresh && rows.some((f) => f.id === draft.name));

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Functions</h1>
        <span className="chip">{ rows.length }</span>
        <button
          type="button"
          className="btn push"
          onClick={ () => {
            setRan(null);
            setPayload('{}');
            setDraft({ name: '', source: STARTER, fresh: true });
          } }
        >
          New function
        </button>
      </header>

      { list.isError && (
        <p className="empty danger">Could not read this Base's functions: { said(list.error) }</p>
      ) }

      { !list.isError && !list.isLoading && rows.length === 0 && !draft && (
        <p className="empty">
          This Base runs no functions yet. A function is a piece of JavaScript
          this Base keeps and runs when it is called, reading only what its
          caller may read.
        </p>
      ) }

      <div className="grid">
        { rows.length > 0 && (
          <div className="stack stack--tight">
            <span className="eyebrow">Saved here</span>
            <div className="list">
              { rows.map((f) => (
                <div
                  key={ f.id }
                  className="list__row list__row--clickable"
                  data-active={ draft && !draft.fresh && draft.name === f.id ? 'true' : undefined }
                  onClick={ () => {
                    setRan(null);
                    setPayload('{}');
                    setDraft({ name: f.id, source: f.source, fresh: false });
                  } }
                >
                  <span className="mono grow">{ f.id }</span>
                  <span className="muted small">{ f.updatedAt?.slice(0, 16) }</span>
                </div>
              )) }
            </div>
            <p className="muted small">
              POST to <span className="mono">/v1/functions/&lt;name&gt;</span> to call one.
            </p>
          </div>
        ) }

        { draft && (
          <div className="stack">
            <span className="eyebrow">{ draft.fresh ? 'New function' : draft.name }</span>

            { draft.fresh && (
              <label className="field">
                <span className="field__label">Name</span>
                <input
                  className="input input--mono"
                  value={ draft.name }
                  autoFocus
                  placeholder="greet"
                  onChange={ (ev) => setDraft({ ...draft, name: ev.target.value }) }
                />
                <span className={ draft.name && !named ? 'danger small' : 'muted small' }>
                  Lower-case letters, digits, <span className="mono">-</span> and{ ' ' }
                  <span className="mono">_</span>; it starts with a letter or digit. The name is
                  the address, so it cannot change later.
                </span>
                { taken && <span className="danger small">There is already a function called { draft.name }.</span> }
              </label>
            ) }

            <label className="field">
              <span className="field__label">Source</span>
              <textarea
                className="textarea textarea--mono"
                rows={ 14 }
                spellCheck={ false }
                value={ draft.source }
                onChange={ (ev) => setDraft({ ...draft, source: ev.target.value }) }
              />
            </label>

            <div className="row">
              <button
                type="button"
                className="btn"
                disabled={ !named || taken || save.isPending }
                onClick={ () => save.mutate(draft) }
              >
                { save.isPending ? 'Saving…' : draft.fresh ? 'Create' : 'Save' }
              </button>
              <button type="button" className="btn btn--ghost" onClick={ () => { setDraft(null); setRan(null); } }>
                Close
              </button>
              { !draft.fresh && (
                <button
                  type="button"
                  className="btn btn--danger push"
                  disabled={ drop.isPending }
                  onClick={ () => {
                    if (confirm(`Delete the function ${ draft.name }?`)) drop.mutate(draft.name);
                  } }
                >
                  Delete
                </button>
              ) }
            </div>

            { save.isError && <p className="danger small">{ said(save.error) }</p> }
            { drop.isError && <p className="danger small">{ said(drop.error) }</p> }
            { save.isSuccess && !save.isPending && <p className="ok small">Saved.</p> }

            {/* Running is offered only for a function that exists, because the
                source being edited is not what would run — the server reads the
                saved row. Offering it on an unsaved draft would report the last
                save as though it were this edit. */}
            { !draft.fresh && (
              <div className="stack stack--tight">
                <span className="eyebrow">Run it</span>
                <textarea
                  className="textarea textarea--mono"
                  rows={ 3 }
                  spellCheck={ false }
                  value={ payload }
                  onChange={ (ev) => setPayload(ev.target.value) }
                />
                <div className="row">
                  <button
                    type="button"
                    className="btn btn--outline"
                    disabled={ run.isPending }
                    onClick={ () => run.mutate(draft) }
                  >
                    { run.isPending ? 'Running…' : 'Run' }
                  </button>
                  <span className="muted small">
                    The payload is the JSON body; it arrives as the handler's first argument.
                  </span>
                </div>
                { ran && (
                  <pre className={ ran.ok ? 'result' : 'result danger' }>{ ran.body }</pre>
                ) }
              </div>
            ) }
          </div>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/functions')({
  // The collection ships with every rule unset, which is the server saying
  // superusers and nobody else. Asking the same question here spares the round
  // trip; the server is still the one that decides.
  beforeLoad: requireSuperuser,
  component: Functions,
});

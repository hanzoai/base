package core

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/hanzoai/base/tools/hook"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
	"github.com/spf13/cast"
)

// A record's history is a row in _history for every create, update and delete,
// for a base collection that asks for it (its History option). The row says
// which collection and record, what was done, who did it and which fields
// changed from what to what. It is written in the same transaction as the
// record, so the two commit or roll back together and no write goes unrecorded.
//
// The writer is the one the record save's context states ([WithActor]); the
// history row is saved with that same context, so its actor field stamps the
// same writer the record's own actor fields do.

// History actions.
const (
	HistoryCreate = "create"
	HistoryUpdate = "update"
	HistoryDelete = "delete"
)

// historyMergeWindow is how close together two updates by one writer to one
// record have to be for the second to fold into the first, so a person editing
// a record field by field leaves one entry rather than one per keystroke.
const historyMergeWindow = 10 * time.Minute

// historyMaxValue is the most bytes of JSON one value keeps in a history row. A
// longer value is recorded as having changed, without its content.
const historyMaxValue = 4 << 10

const systemHookIdHistory = "__hzRecordHistory__"

// historyHookPriority runs the history handler immediately before the system
// handler that writes the record (priority 99), so the record it sees is the one
// every other handler has finished with and the field interceptors are about to
// write.
const historyHookPriority = 98

func (app *BaseApp) registerHistoryHooks() {
	app.OnRecordCreateExecute().Bind(&hook.Handler[*RecordEvent]{
		Id:       systemHookIdHistory,
		Priority: historyHookPriority,
		Func: func(e *RecordEvent) error {
			if !keepsHistory(e.Record.Collection()) {
				return e.Next()
			}
			return inHistoryTx(e, func(txApp App) error {
				return writeHistory(txApp, e, HistoryCreate, historyChanges(nil, e.Record))
			})
		},
	})

	app.OnRecordUpdateExecute().Bind(&hook.Handler[*RecordEvent]{
		Id:       systemHookIdHistory,
		Priority: historyHookPriority,
		Func: func(e *RecordEvent) error {
			if !keepsHistory(e.Record.Collection()) {
				return e.Next()
			}

			var stored *Record
			return inHistoryTx(e, func(txApp App) error {
				return writeHistory(txApp, e, HistoryUpdate, historyChanges(stored, e.Record))
			}, func(txApp App) error {
				// the row as it stands, read in the transaction that replaces it
				var err error
				stored, err = txApp.FindRecordById(e.Record.Collection(), cast.ToString(e.Record.LastSavedPK()))
				return err
			})
		},
	})

	app.OnRecordDeleteExecute().Bind(&hook.Handler[*RecordEvent]{
		Id:       systemHookIdHistory,
		Priority: historyHookPriority,
		Func: func(e *RecordEvent) error {
			if !keepsHistory(e.Record.Collection()) {
				return e.Next()
			}
			changes := historyChanges(e.Record, nil)
			return inHistoryTx(e, func(txApp App) error {
				return writeHistory(txApp, e, HistoryDelete, changes)
			})
		},
	})
}

func keepsHistory(c *Collection) bool {
	return c != nil && c.IsBase() && c.History && c.Name != CollectionNameHistory
}

// inHistoryTx runs the record's write and then after, in one transaction. before
// runs inside the transaction ahead of the write.
func inHistoryTx(e *RecordEvent, after func(txApp App) error, before ...func(txApp App) error) error {
	originalApp := e.App
	defer func() { e.App = originalApp }()

	return e.App.RunInTransaction(func(txApp App) error {
		e.App = txApp

		for _, fn := range before {
			if err := fn(txApp); err != nil {
				return err
			}
		}

		if err := e.Next(); err != nil {
			return err
		}

		return after(txApp)
	})
}

// historyChange is one field's change. A create has only To, a delete only From.
type historyChange struct {
	From json.RawMessage `json:"from,omitempty"`
	To   json.RawMessage `json:"to,omitempty"`
}

// historyChanges compares the record as it was (nil for a create) with the
// record as it is (nil for a delete), field by field. id, autodate and actor
// fields and hidden fields are left out: the first three say nothing a person
// did, and a hidden field is not the reader's to see.
func historyChanges(was, is *Record) map[string]historyChange {
	ref := is
	if ref == nil {
		ref = was
	}

	changes := map[string]historyChange{}

	for _, field := range ref.Collection().Fields {
		name := field.GetName()
		switch {
		case name == FieldNameId, field.GetHidden():
			continue
		case field.Type() == FieldTypeAutodate, field.Type() == FieldTypeActor:
			continue
		}

		var from, to json.RawMessage
		if was != nil {
			from = historyValue(was.Get(name))
		}
		if is != nil {
			to = historyValue(is.Get(name))
		}

		switch {
		case was == nil:
			if !isEmptyHistoryValue(to) {
				changes[name] = historyChange{To: to}
			}
		case is == nil:
			if !isEmptyHistoryValue(from) {
				changes[name] = historyChange{From: from}
			}
		case !bytes.Equal(from, to):
			changes[name] = historyChange{From: from, To: to}
		}
	}

	return changes
}

// historyValue is v as JSON, or {"truncated":true} when it is too long to keep.
func historyValue(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	if len(raw) > historyMaxValue {
		return json.RawMessage(`{"truncated":true}`)
	}
	return raw
}

// isEmptyHistoryValue reports a value a create or a delete does not list: one
// that holds nothing.
func isEmptyHistoryValue(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", `""`, "[]", "{}", "0", "false":
		return true
	}
	return false
}

// writeHistory records one write, or folds an update into the writer's previous
// one.
func writeHistory(txApp App, e *RecordEvent, action string, changes map[string]historyChange) error {
	if action == HistoryUpdate && len(changes) == 0 {
		return nil // nothing a reader would see changed
	}

	history, err := txApp.FindCachedCollectionByNameOrId(CollectionNameHistory)
	if err != nil {
		return err
	}

	collectionId := e.Record.Collection().Id
	recordId := e.Record.Id
	if action != HistoryCreate {
		recordId = cast.ToString(e.Record.LastSavedPK())
	}

	if action == HistoryUpdate {
		merged, err := mergeIntoLastUpdate(txApp, e, history, collectionId, recordId, changes)
		if err != nil || merged {
			return err
		}
	}

	row := NewRecord(history)
	row.Set("collection", collectionId)
	row.Set("record", recordId)
	row.Set("action", action)
	row.Set("changes", changes)

	return txApp.SaveNoValidateWithContext(e.Context, row)
}

// mergeIntoLastUpdate folds changes into the record's latest history row when
// that row is an update by the same writer less than historyMergeWindow ago.
// The earlier "from" is kept and the later "to" taken; a field back where it
// started drops out, and a row left with no change is deleted.
func mergeIntoLastUpdate(
	txApp App,
	e *RecordEvent,
	history *Collection,
	collectionId, recordId string,
	changes map[string]historyChange,
) (bool, error) {
	actor := ActorFrom(e.Context)
	if actor.Id == "" {
		return false, nil // a writer with no id cannot be told apart from another
	}

	order := []string{"created DESC"}
	if row := txApp.Dialect().Row(); row != "" {
		order = append(order, row+" DESC") // two rows in one millisecond
	}

	last := &Record{}
	err := txApp.RecordQuery(history).
		AndWhere(dbx.HashExp{"collection": collectionId, "record": recordId}).
		OrderBy(order...).
		Limit(1).
		One(last)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	lastActor, _ := last.Get("actor").(types.Actor)
	if last.GetString("action") != HistoryUpdate ||
		lastActor.Id != actor.Id || lastActor.Source != actor.Source ||
		time.Since(last.GetDateTime("created").Time()) >= historyMergeWindow {
		return false, nil
	}

	merged := map[string]historyChange{}
	if err := json.Unmarshal([]byte(last.GetString("changes")), &merged); err != nil {
		return false, nil // a row this cannot read is left as it is, and a new one written
	}
	for name, c := range changes {
		if prev, ok := merged[name]; ok {
			c.From = prev.From
		}
		if bytes.Equal(c.From, c.To) {
			delete(merged, name)
			continue
		}
		merged[name] = c
	}

	if len(merged) == 0 {
		return true, txApp.DeleteWithContext(e.Context, last)
	}

	last.Set("changes", merged)
	return true, txApp.SaveNoValidateWithContext(e.Context, last)
}

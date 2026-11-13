package core

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hanzoai/dbx"
	"github.com/hanzoai/orm/dialect"
)

// savepoints names the nested transactions of an engine that has them.
var savepoints atomic.Int64

// RunInTransaction wraps fn into a transaction for the regular app database.
//
// It is safe to nest RunInTransaction calls as long as you use the callback's txApp.
func (app *BaseApp) RunInTransaction(fn func(txApp App) error) error {
	return app.runInTransaction(app.NonconcurrentDB(), fn, false)
}

// AuxRunInTransaction wraps fn into a transaction for the auxiliary app database.
//
// It is safe to nest RunInTransaction calls as long as you use the callback's txApp.
func (app *BaseApp) AuxRunInTransaction(fn func(txApp App) error) error {
	return app.runInTransaction(app.AuxNonconcurrentDB(), fn, true)
}

func (app *BaseApp) runInTransaction(db dbx.Builder, fn func(txApp App) error, isForAuxDB bool) error {
	switch txOrDB := db.(type) {
	case *dbx.Tx:
		// run as part of the already existing transaction
		if _, local := app.Dialect().(dialect.SQLite); local {
			return fn(app)
		}
		// PostgreSQL aborts the whole transaction at the first failed statement,
		// so a nested call whose error the caller handles would leave the outer
		// one unusable. A savepoint is the nested transaction: its failure rolls
		// back its own statements and nothing else.
		name := fmt.Sprintf("hz_sp_%d", savepoints.Add(1))
		if _, err := txOrDB.NewQuery("SAVEPOINT " + name).Execute(); err != nil {
			return err
		}
		if err := fn(app); err != nil {
			if _, rerr := txOrDB.NewQuery("ROLLBACK TO SAVEPOINT " + name).Execute(); rerr != nil {
				return errors.Join(err, rerr)
			}
			_, _ = txOrDB.NewQuery("RELEASE SAVEPOINT " + name).Execute()
			return err
		}
		_, err := txOrDB.NewQuery("RELEASE SAVEPOINT " + name).Execute()
		return err
	case *dbx.DB:
		// an engine that rolls a transaction back for conflicting with a
		// concurrent one is asking for that transaction, and so for fn, to run
		// again from its first statement; each attempt begins a transaction of
		// its own, on its own tx app, and settles its own after calls
		return retry(func(int) (error, bool) {
			watched, conflicted := watchConflict(txOrDB)

			var txApp *BaseApp
			txErr := watched.Transactional(func(tx *dbx.Tx) error {
				txApp = app.createTxApp(tx, isForAuxDB)
				return fn(txApp)
			})

			// execute all after event calls on transaction complete
			if txApp != nil && txApp.txInfo != nil {
				afterFuncErr := txApp.txInfo.runAfterFuncs(txErr)
				if afterFuncErr != nil {
					return errors.Join(txErr, afterFuncErr), false
				}
			}

			return txErr, conflicted()
		}, defaultMaxLockRetries)
	default:
		return errors.New("failed to start transaction (unknown db type)")
	}
}

// createTxApp shallow clones the current app and assigns a new tx state.
func (app *BaseApp) createTxApp(tx *dbx.Tx, isForAuxDB bool) *BaseApp {
	clone := *app

	if isForAuxDB {
		clone.auxConcurrentDB = tx
		clone.auxNonconcurrentDB = tx
	} else {
		clone.concurrentDB = tx
		clone.nonconcurrentDB = tx
	}

	clone.txInfo = &TxAppInfo{
		parent:     app,
		isForAuxDB: isForAuxDB,
	}

	return &clone
}

// TxAppInfo represents an active transaction context associated to an existing app instance.
type TxAppInfo struct {
	parent     *BaseApp
	afterFuncs []func(txErr error) error
	mu         sync.Mutex
	isForAuxDB bool
}

// OnComplete registers the provided callback that will be invoked
// once the related transaction ends (either completes successfully or rollbacked with an error).
//
// The callback receives the transaction error (if any) as its argument.
// Any additional errors returned by the OnComplete callbacks will be
// joined together with txErr when returning the final transaction result.
func (a *TxAppInfo) OnComplete(fn func(txErr error) error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.afterFuncs = append(a.afterFuncs, fn)
}

// note: can be called only once because TxAppInfo is cleared
func (a *TxAppInfo) runAfterFuncs(txErr error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var errs []error

	for _, call := range a.afterFuncs {
		if err := call(txErr); err != nil {
			errs = append(errs, err)
		}
	}

	a.afterFuncs = nil

	if len(errs) > 0 {
		return fmt.Errorf("transaction afterFunc errors: %w", errors.Join(errs...))
	}

	return nil
}

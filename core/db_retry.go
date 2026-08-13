package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hanzoai/dbx"
)

// default retries intervals (in ms)
var defaultRetryIntervals = []int{50, 100, 150, 200, 300, 400, 500, 700, 1000}

// default max retry attempts
const defaultMaxLockRetries = 12

func execLockRetry(timeout time.Duration, maxRetries int) dbx.ExecHookFunc {
	return func(q *dbx.Query, op func() error) error {
		if q.Context() == nil {
			cancelCtx, cancel := context.WithTimeout(context.Background(), timeout)
			defer func() {
				cancel()
				//nolint:staticcheck
				q.WithContext(nil) // reset
			}()
			q.WithContext(cancelCtx)
		}

		execErr := baseLockRetry(func(attempt int) error {
			return op()
		}, maxRetries)
		if execErr != nil && !errors.Is(execErr, sql.ErrNoRows) {
			execErr = fmt.Errorf("%w; failed query: %s", execErr, q.SQL())
		}

		return execErr
	}
}

// retry runs attempt until it succeeds, until attempt stops asking to be run
// again, or until the attempts run out. The pause between attempts grows along
// defaultRetryIntervals.
func retry(attempt func(n int) (error, bool), maxRetries int) error {
	n := 1

	for {
		err, again := attempt(n)
		if err == nil || !again || n > maxRetries {
			return err
		}

		time.Sleep(getDefaultRetryInterval(n))
		n++
	}
}

// baseLockRetry runs op again while the engine holds the database busy.
func baseLockRetry(op func(attempt int) error, maxRetries int) error {
	return retry(func(n int) (error, bool) {
		err := op(n)
		return err, err != nil && isLocked(err)
	}, maxRetries)
}

// isLocked reports whether err is the engine saying the database is held by
// another writer for the moment.
//
// We are checking the error against the plain error texts since the codes could
// vary between drivers.
func isLocked(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "table is locked")
}

// watchConflict returns a copy of db that reports whether the engine refused a
// statement run through it because it had rolled the transaction back for
// conflicting with a concurrent one.
//
// The engine answers the conflict on the statement that lost, and everything
// between that statement and the caller is free to return an error of its own
// shape, so the transaction reads the conflict where it is said rather than out
// of whatever finally comes back. The copy carries the same pool and the same
// log functions, so a statement is executed and observed exactly as before.
func watchConflict(db *dbx.DB) (*dbx.DB, func() bool) {
	watched := db.WithContext(db.Context())

	var seen atomic.Bool

	query := watched.QueryLogFunc
	watched.QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {
		if err != nil && isSerializationFailure(err) {
			seen.Store(true)
		}
		if query != nil {
			query(ctx, t, sql, rows, err)
		}
	}

	exec := watched.ExecLogFunc
	watched.ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {
		if err != nil && isSerializationFailure(err) {
			seen.Store(true)
		}
		if exec != nil {
			exec(ctx, t, sql, result, err)
		}
	}

	return watched, seen.Load
}

// isSerializationFailure reports whether err is the engine rolling a transaction
// back because it conflicted with a concurrent one — SQLSTATE class 40, which is
// the engine asking for that transaction to be run again.
//
// The code travels in the message the way a duplicate key's does (see
// validators.IsUniqueViolation), so this reads the code rather than an engine's
// prose and stays free of any driver import.
func isSerializationFailure(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "sqlstate 40001") || // serialization_failure
		strings.Contains(msg, "sqlstate 40p01") // deadlock_detected
}

func getDefaultRetryInterval(attempt int) time.Duration {
	if attempt < 0 || attempt > len(defaultRetryIntervals)-1 {
		return time.Duration(defaultRetryIntervals[len(defaultRetryIntervals)-1]) * time.Millisecond
	}

	return time.Duration(defaultRetryIntervals[attempt]) * time.Millisecond
}

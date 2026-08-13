package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hanzoai/dbx"
)

// The two engines word a conflict completely differently, and only one of them
// has a lock message at all, so a transaction that must run again is recognized
// by the SQLSTATE the way a duplicate key is.
func TestIsSerializationFailure(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		err      error
		expected bool
	}{
		{errors.New("ERROR: deadlock detected (SQLSTATE 40P01)"), true},
		{errors.New("ERROR: could not serialize access due to concurrent update (SQLSTATE 40001)"), true},
		{fmt.Errorf("%w; failed query: UPDATE x", errors.New("ERROR: deadlock detected (SQLSTATE 40P01)")), true},

		// the statement that follows a conflict on the same transaction, which
		// says only that the transaction is over and is not itself retryable
		{errors.New("ERROR: current transaction is aborted, commands ignored until end of transaction block (SQLSTATE 25P02)"), false},

		{errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"), false},
		{errors.New("database is locked"), false},
		{errors.New("test"), false},
	}

	for _, s := range scenarios {
		t.Run(s.err.Error(), func(t *testing.T) {
			if got := isSerializationFailure(s.err); got != s.expected {
				t.Fatalf("Expected %v, got %v", s.expected, got)
			}
		})
	}
}

// The engine answers a conflict on the statement that lost, far below the error
// the transaction body finally returns, so the transaction reads it from the
// statement — while the statement is still executed and logged as before.
func TestWatchConflict(t *testing.T) {
	t.Parallel()

	deadlock := errors.New("ERROR: deadlock detected (SQLSTATE 40P01)")

	t.Run("an exec that lost", func(t *testing.T) {
		db := dbx.NewFromDB(new(sql.DB), "sqlite")

		logged := 0
		db.ExecLogFunc = func(context.Context, time.Duration, string, sql.Result, error) { logged++ }

		watched, conflicted := watchConflict(db)

		watched.ExecLogFunc(context.Background(), 0, "UPDATE x", nil, nil)
		if conflicted() {
			t.Fatal("Expected no conflict before one is answered")
		}

		watched.ExecLogFunc(context.Background(), 0, "UPDATE x", nil, deadlock)
		if !conflicted() {
			t.Fatal("Expected the conflict to be seen")
		}

		if logged != 2 {
			t.Fatalf("Expected both statements to reach the original log func, got %d", logged)
		}
	})

	t.Run("a query that lost", func(t *testing.T) {
		db := dbx.NewFromDB(new(sql.DB), "sqlite")

		watched, conflicted := watchConflict(db)

		watched.QueryLogFunc(context.Background(), 0, "SELECT 1", nil, deadlock)
		if !conflicted() {
			t.Fatal("Expected the conflict to be seen")
		}
	})

	t.Run("a failure of another kind", func(t *testing.T) {
		db := dbx.NewFromDB(new(sql.DB), "sqlite")

		watched, conflicted := watchConflict(db)

		watched.ExecLogFunc(context.Background(), 0, "UPDATE x", nil, errors.New("no such table"))
		if conflicted() {
			t.Fatal("Expected only a conflict to be seen")
		}
	})
}

// A conflict is settled by running the whole thing again, so the retry runs the
// attempt itself rather than handing back the one that already lost.
func TestRetryRunsTheAttemptAgain(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name             string
		conflictUntil    int
		maxRetries       int
		expectedAttempts int
		expectedErr      bool
	}{
		{"no conflict", 0, 3, 1, false},
		{"settles on the third", 3, 5, 3, false},
		{"attempts run out", 99, 2, 3, true},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			attempts := 0

			err := retry(func(n int) (error, bool) {
				attempts++

				if n < s.conflictUntil {
					return errors.New("ERROR: deadlock detected (SQLSTATE 40P01)"), true
				}

				return nil, false
			}, s.maxRetries)

			if attempts != s.expectedAttempts {
				t.Fatalf("Expected %d attempts, got %d", s.expectedAttempts, attempts)
			}

			if (err != nil) != s.expectedErr {
				t.Fatalf("Expected error %v, got %v", s.expectedErr, err)
			}
		})
	}
}

func TestGetDefaultRetryInterval(t *testing.T) {
	t.Parallel()

	if i := getDefaultRetryInterval(-1); i.Milliseconds() != 1000 {
		t.Fatalf("Expected 1000ms, got %v", i)
	}

	if i := getDefaultRetryInterval(999); i.Milliseconds() != 1000 {
		t.Fatalf("Expected 1000ms, got %v", i)
	}

	if i := getDefaultRetryInterval(3); i.Milliseconds() != 200 {
		t.Fatalf("Expected 500ms, got %v", i)
	}
}

func TestBaseLockRetry(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		err              error
		failUntilAttempt int
		expectedAttempts int
	}{
		{nil, 3, 1},
		{errors.New("test"), 3, 1},
		{errors.New("database is locked"), 3, 3},
		{errors.New("table is locked"), 3, 3},
	}

	for i, s := range scenarios {
		t.Run(fmt.Sprintf("%d_%#v", i, s.err), func(t *testing.T) {
			lastAttempt := 0

			err := baseLockRetry(func(attempt int) error {
				lastAttempt = attempt

				if attempt < s.failUntilAttempt {
					return s.err
				}

				return nil
			}, s.failUntilAttempt+2)

			if lastAttempt != s.expectedAttempts {
				t.Errorf("Expected lastAttempt to be %d, got %d", s.expectedAttempts, lastAttempt)
			}

			if s.failUntilAttempt == s.expectedAttempts && err != nil {
				t.Fatalf("Expected nil, got err %v", err)
			}

			if s.failUntilAttempt != s.expectedAttempts && s.err != nil && err == nil {
				t.Fatalf("Expected error %q, got nil", s.err)
			}
		})
	}
}

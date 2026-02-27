package scheduler

import (
	"time"

	"github.com/dop251/goja"
	"github.com/hanzoai/base/core"
)

// RegisterJSVM registers the scheduler JS bindings into a goja runtime.
//
// This adds the following global functions:
//
//   - scheduleAfter(delayMs, functionName, args?) - schedule execution after a delay
//   - scheduleAt(isoDateString, functionName, args?) - schedule execution at a specific time
//   - cancelScheduled(scheduledId) - cancel a pending scheduled function
//   - listScheduled(status?) - list scheduled functions, optionally filtered by status
//
// Example JS usage:
//
//	onRecordAfterCreateSuccess((e) => {
//	    scheduleAfter(0, "sendNotification", { recordId: e.record.id })
//	}, "tasks")
//
//	cronAdd("cleanup", "0 3 * * *", () => {
//	    scheduleAfter(0, "cleanupExpired", {})
//	})
//
//	// Schedule something 30 minutes from now
//	scheduleAfter(1800000, "generateReport", { type: "daily" })
//
//	// Schedule at a specific time
//	scheduleAt("2026-03-01T09:00:00Z", "sendReminder", { userId: "abc123" })
//
//	// Cancel a scheduled function
//	cancelScheduled("scheduled_record_id")
//
//	// List all pending scheduled functions
//	const pending = listScheduled("pending")
func RegisterJSVM(app core.App, vm *goja.Runtime) {
	vm.Set("scheduleAfter", func(call goja.FunctionCall) goja.Value {
		delayMs := int64(0)
		if arg := call.Argument(0).Export(); arg != nil {
			switch v := arg.(type) {
			case int64:
				delayMs = v
			case float64:
				delayMs = int64(v)
			}
		}

		functionName := ""
		if arg := call.Argument(1).Export(); arg != nil {
			if s, ok := arg.(string); ok {
				functionName = s
			}
		}
		if functionName == "" {
			panic(vm.NewGoError(errMissingFunctionName))
		}

		var args any
		if arg := call.Argument(2); arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			args = arg.Export()
		}

		id, err := ScheduleAfter(app, delayMs, functionName, args)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return vm.ToValue(id)
	})

	vm.Set("scheduleAt", func(call goja.FunctionCall) goja.Value {
		isoDate := ""
		if arg := call.Argument(0).Export(); arg != nil {
			if s, ok := arg.(string); ok {
				isoDate = s
			}
		}
		if isoDate == "" {
			panic(vm.NewGoError(errMissingTimestamp))
		}

		t, err := time.Parse(time.RFC3339, isoDate)
		if err != nil {
			// fallback: try parsing as datetime string (YYYY-MM-DD HH:MM:SS)
			t, err = time.Parse("2006-01-02 15:04:05", isoDate)
			if err != nil {
				panic(vm.NewGoError(err))
			}
		}

		functionName := ""
		if arg := call.Argument(1).Export(); arg != nil {
			if s, ok := arg.(string); ok {
				functionName = s
			}
		}
		if functionName == "" {
			panic(vm.NewGoError(errMissingFunctionName))
		}

		var args any
		if arg := call.Argument(2); arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			args = arg.Export()
		}

		id, err := ScheduleAt(app, t, functionName, args)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return vm.ToValue(id)
	})

	vm.Set("cancelScheduled", func(call goja.FunctionCall) goja.Value {
		id := ""
		if arg := call.Argument(0).Export(); arg != nil {
			if s, ok := arg.(string); ok {
				id = s
			}
		}
		if id == "" {
			panic(vm.NewGoError(errMissingScheduledId))
		}

		if err := CancelScheduled(app, id); err != nil {
			panic(vm.NewGoError(err))
		}

		return goja.Undefined()
	})

	vm.Set("listScheduled", func(call goja.FunctionCall) goja.Value {
		status := ""
		if arg := call.Argument(0); arg != nil && !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			if s, ok := arg.Export().(string); ok {
				status = s
			}
		}

		records, err := ListScheduled(app, status)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return vm.ToValue(records)
	})
}

// sentinel errors for JS binding argument validation
var (
	errMissingFunctionName = &schedulerError{"scheduleAfter/scheduleAt requires a non-empty functionName"}
	errMissingTimestamp    = &schedulerError{"scheduleAt requires a non-empty ISO date string"}
	errMissingScheduledId = &schedulerError{"cancelScheduled requires a non-empty scheduled record id"}
)

type schedulerError struct {
	message string
}

func (e *schedulerError) Error() string {
	return e.message
}

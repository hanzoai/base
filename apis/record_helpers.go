package apis

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hanzoai/dbx"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/search"
)

const (
	expandQueryParam = "expand"
	fieldsQueryParam = "fields"
)

// RecordAuthResponse writes standardized json record auth response
// into the specified request context.
//
// The authMethod argument specifies the name of the current authentication
// method (e.g. oauth2) and is forwarded to the OnRecordAuthRequest hook so
// callers can observe it. Hanzo IAM is the only auth source — Base no
// longer issues credentials or runs MFA/OTP/login-alert flows itself.
func RecordAuthResponse(e *core.RequestEvent, authRecord *core.Record, authMethod string, meta any) error {
	token, tokenErr := authRecord.NewAuthToken()
	if tokenErr != nil {
		return e.InternalServerError("Failed to create auth token.", tokenErr)
	}

	return recordAuthResponse(e, authRecord, token, authMethod, meta)
}

func recordAuthResponse(e *core.RequestEvent, authRecord *core.Record, token string, authMethod string, meta any) error {
	originalRequestInfo, err := e.RequestInfo()
	if err != nil {
		return err
	}

	ok, err := e.App.CanAccessRecord(authRecord, originalRequestInfo, authRecord.Collection().AuthRule)
	if !ok {
		return firstApiError(err, e.ForbiddenError("The request doesn't satisfy the collection requirements to authenticate.", err))
	}

	event := new(core.RecordAuthRequestEvent)
	event.RequestEvent = e
	event.Collection = authRecord.Collection()
	event.Record = authRecord
	event.Token = token
	event.Meta = meta
	event.AuthMethod = authMethod

	return e.App.OnRecordAuthRequest().Trigger(event, func(e *core.RecordAuthRequestEvent) error {
		if e.Written() {
			return nil
		}

		// create a shallow copy of the cached request data and adjust it to the current auth record
		requestInfo := *originalRequestInfo
		requestInfo.Auth = e.Record

		err = triggerRecordEnrichHooks(e.App, &requestInfo, []*core.Record{e.Record}, func() error {
			if e.Record.IsSuperuser() {
				e.Record.Unhide(e.Record.Collection().Fields.FieldNames()...)
			}

			// allow always returning the email address of the authenticated model
			e.Record.IgnoreEmailVisibility(true)

			// expand record relations
			expands := strings.Split(e.Request.URL.Query().Get(expandQueryParam), ",")
			if len(expands) > 0 {
				failed := e.App.ExpandRecord(e.Record, expands, expandFetch(e.App, &requestInfo))
				if len(failed) > 0 {
					e.App.Logger().Warn("[recordAuthResponse] Failed to expand relations", "error", failed)
				}
			}

			return nil
		})
		if err != nil {
			return err
		}

		result := struct {
			Meta   any          `json:"meta,omitempty"`
			Record *core.Record `json:"record"`
			Token  string       `json:"token"`
		}{
			Token:  e.Token,
			Record: e.Record,
		}

		if e.Meta != nil {
			result.Meta = e.Meta
		}

		return execAfterSuccessTx(true, e.App, func() error {
			return e.JSON(http.StatusOK, result)
		})
	})
}

// EnrichRecord parses the request context and enrich the provided record:
//   - expands relations (if defaultExpands and/or ?expand query param is set)
//   - ensures that the emails of the auth record and its expanded auth relations
//     are visible only for the current logged superuser, record owner or record with manage access
func EnrichRecord(e *core.RequestEvent, record *core.Record, defaultExpands ...string) error {
	return EnrichRecords(e, []*core.Record{record}, defaultExpands...)
}

// EnrichRecords parses the request context and enriches the provided records:
//   - expands relations (if defaultExpands and/or ?expand query param is set)
//   - ensures that the emails of the auth records and their expanded auth relations
//     are visible only for the current logged superuser, record owner or record with manage access
//
// Note: Expects all records to be from the same collection!
func EnrichRecords(e *core.RequestEvent, records []*core.Record, defaultExpands ...string) error {
	if len(records) == 0 {
		return nil
	}

	info, err := e.RequestInfo()
	if err != nil {
		return err
	}

	return triggerRecordEnrichHooks(e.App, info, records, func() error {
		expands := defaultExpands
		if param := info.Query[expandQueryParam]; param != "" {
			expands = append(expands, strings.Split(param, ",")...)
		}

		err := defaultEnrichRecords(e.App, info, records, expands...)
		if err != nil {
			// only log because it is not critical
			e.App.Logger().Warn("failed to apply default enriching", "error", err)
		}

		return nil
	})
}

type iterator[T any] struct {
	items []T
	index int
}

func (ri *iterator[T]) next() T {
	var item T

	if ri.index < len(ri.items) {
		item = ri.items[ri.index]
		ri.index++
	}

	return item
}

func triggerRecordEnrichHooks(app core.App, requestInfo *core.RequestInfo, records []*core.Record, finalizer func() error) error {
	it := iterator[*core.Record]{items: records}

	enrichHook := app.OnRecordEnrich()

	event := new(core.RecordEnrichEvent)
	event.App = app
	event.RequestInfo = requestInfo

	var iterate func(record *core.Record) error
	iterate = func(record *core.Record) error {
		if record == nil {
			return nil
		}

		event.Record = record

		return enrichHook.Trigger(event, func(ee *core.RecordEnrichEvent) error {
			next := it.next()
			if next == nil {
				if finalizer != nil {
					return finalizer()
				}
				return nil
			}

			event.App = ee.App // in case it was replaced with a transaction
			event.Record = next

			err := iterate(next)

			event.App = app
			event.Record = record

			return err
		})
	}

	return iterate(it.next())
}

func defaultEnrichRecords(app core.App, requestInfo *core.RequestInfo, records []*core.Record, expands ...string) error {
	err := autoResolveRecordsFlags(app, records, requestInfo)
	if err != nil {
		return fmt.Errorf("failed to resolve records flags: %w", err)
	}

	if len(expands) > 0 {
		expandErrs := app.ExpandRecords(records, expands, expandFetch(app, requestInfo))
		if len(expandErrs) > 0 {
			errsSlice := make([]error, 0, len(expandErrs))
			for key, err := range expandErrs {
				errsSlice = append(errsSlice, fmt.Errorf("failed to expand %q: %w", key, err))
			}
			return fmt.Errorf("failed to expand records: %w", errors.Join(errsSlice...))
		}
	}

	return nil
}

// expandFetch is the records fetch function that is used to expand related records.
func expandFetch(app core.App, originalRequestInfo *core.RequestInfo) core.ExpandFetchFunc {
	// shallow clone the provided request info to set an "expand" context
	requestInfoClone := *originalRequestInfo
	requestInfoPtr := &requestInfoClone
	requestInfoPtr.Context = core.RequestInfoContextExpand

	return func(relCollection *core.Collection, relIds []string) ([]*core.Record, error) {
		records, findErr := app.FindRecordsByIds(relCollection.Id, relIds, func(q *dbx.SelectQuery) error {
			if requestInfoPtr.Auth != nil && requestInfoPtr.Auth.IsSuperuser() {
				return nil // superusers can access everything
			}

			if relCollection.ViewRule == nil {
				return fmt.Errorf("only superusers can view collection %q records", relCollection.Name)
			}

			if *relCollection.ViewRule != "" {
				resolver := core.NewRecordFieldResolver(app, relCollection, requestInfoPtr, true)

				expr, err := search.FilterData(*(relCollection.ViewRule)).BuildExpr(resolver)
				if err != nil {
					return err
				}

				q.AndWhere(expr)

				err = resolver.UpdateQuery(q)
				if err != nil {
					return err
				}
			}

			return nil
		})
		if findErr != nil {
			return nil, findErr
		}

		enrichErr := triggerRecordEnrichHooks(app, requestInfoPtr, records, func() error {
			if err := autoResolveRecordsFlags(app, records, requestInfoPtr); err != nil {
				// non-critical error
				app.Logger().Warn("Failed to apply autoResolveRecordsFlags for the expanded records", "error", err)
			}

			return nil
		})
		if enrichErr != nil {
			return nil, enrichErr
		}

		return records, nil
	}
}

// autoResolveRecordsFlags resolves various visibility flags of the provided records.
//
// Currently it enables:
// - export of hidden fields if the current auth model is a superuser
// - email export ignoring the emailVisibity checks if the current auth model is superuser, owner or a "manager".
//
// Note: Expects all records to be from the same collection!
func autoResolveRecordsFlags(app core.App, records []*core.Record, requestInfo *core.RequestInfo) error {
	if len(records) == 0 {
		return nil // nothing to resolve
	}

	if requestInfo.HasSuperuserAuth() {
		hiddenFields := records[0].Collection().Fields.FieldNames()
		for _, rec := range records {
			rec.Unhide(hiddenFields...)
			rec.IgnoreEmailVisibility(true)
		}
	}

	// additional emailVisibility checks
	// ---------------------------------------------------------------
	if !records[0].Collection().IsAuth() {
		return nil // not auth collection records
	}

	collection := records[0].Collection()

	mappedRecords := make(map[string]*core.Record, len(records))
	recordIds := make([]any, len(records))
	for i, rec := range records {
		mappedRecords[rec.Id] = rec
		recordIds[i] = rec.Id
	}

	if requestInfo.Auth != nil && mappedRecords[requestInfo.Auth.Id] != nil {
		mappedRecords[requestInfo.Auth.Id].IgnoreEmailVisibility(true)
	}

	if collection.ManageRule == nil || *collection.ManageRule == "" {
		return nil // no manage rule to check
	}

	// fetch the ids of the managed records
	// ---
	managedIds := []string{}

	query := app.RecordQuery(collection).
		Select(app.ConcurrentDB().QuoteSimpleColumnName(collection.Name) + ".id").
		AndWhere(dbx.In(app.ConcurrentDB().QuoteSimpleColumnName(collection.Name)+".id", recordIds...))

	resolver := core.NewRecordFieldResolver(app, collection, requestInfo, true)
	expr, err := search.FilterData(*collection.ManageRule).BuildExpr(resolver)
	if err != nil {
		return err
	}

	query.AndWhere(expr)

	err = resolver.UpdateQuery(query)
	if err != nil {
		return err
	}

	err = query.Column(&managedIds)
	if err != nil {
		return err
	}
	// ---

	// ignore the email visibility check for the managed records
	for _, id := range managedIds {
		if rec, ok := mappedRecords[id]; ok {
			rec.IgnoreEmailVisibility(true)
		}
	}

	return nil
}

var ruleQueryParams = []string{search.FilterQueryParam, search.SortQueryParam}
var superuserOnlyRuleFields = []string{"@collection.", "@request."}

// checkForSuperuserOnlyRuleFields loosely checks and returns an error if
// the provided RequestInfo contains rule fields that only the superuser can use.
func checkForSuperuserOnlyRuleFields(requestInfo *core.RequestInfo) error {
	if len(requestInfo.Query) == 0 || requestInfo.HasSuperuserAuth() {
		return nil // superuser or nothing to check
	}

	for _, param := range ruleQueryParams {
		v := requestInfo.Query[param]
		if v == "" {
			continue
		}

		for _, field := range superuserOnlyRuleFields {
			if strings.Contains(v, field) {
				return router.NewForbiddenError("Only superusers can filter by "+field, nil)
			}
		}
	}

	return nil
}

// firstApiError returns the first ApiError from the errors list
// (this is used usually to prevent unnecessary wraping and to allow bubling ApiError from nested hooks)
//
// If no ApiError is found, returns a default "Internal server" error.
func firstApiError(errs ...error) *router.ApiError {
	var apiErr *router.ApiError
	var ok bool

	for _, err := range errs {
		if err == nil {
			continue
		}

		// quick assert to avoid the reflection checks
		apiErr, ok = err.(*router.ApiError)
		if ok {
			return apiErr
		}

		// nested/wrapped errors
		if errors.As(err, &apiErr) {
			return apiErr
		}
	}

	return router.NewInternalServerError("", errors.Join(errs...))
}

// execAfterSuccessTx ensures that fn is executed only after a successful transaction.
//
// If the current app instance is not a transactional or checkTx is false,
// then fn is directly executed.
//
// It could be usually used to allow propagating an error or writing
// custom response from within the wrapped transaction block.
func execAfterSuccessTx(checkTx bool, app core.App, fn func() error) error {
	if txInfo := app.TxInfo(); txInfo != nil && checkTx {
		txInfo.OnComplete(func(txErr error) error {
			if txErr == nil {
				return fn()
			}
			return nil
		})
		return nil
	}

	return fn()
}


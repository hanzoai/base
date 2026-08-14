package apis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/extruntime"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/search"
)

// Functions: code a Base keeps, run inside that Base, under the authority of
// whoever called it.
//
// A function is a row in [core.CollectionNameFunctions] whose id is its name and
// whose source is its body — so an org's functions are in that org's file, and
// who may write one and who may call one are the rules on that collection rather
// than a second opinion stated here.
//
// Management is the record path with a nicer address. Every verb below except
// the invoke is the record handler itself, reached after the request is pointed
// at the collection: one collection lookup, one rule, one audit line. Writing
// these five out again to change the shape of a URL is how two doors come to
// disagree about who may edit a function, which is the disagreement that matters.
//
// The invoke is the one thing here that is genuinely new. It reads the function
// as the caller — a function the caller may not view is one that is not there —
// and then hands the source to a runtime with a [core.RequestEvent]-shaped host
// bound for the length of the call and nothing else bound at all.

// functionLang is what a function is written in. One language, so the record
// carries source and no field saying how to read it; a second language arrives
// with the field that chooses between them.
const functionLang = "js"

const (
	// functionTimeout is how long one call may hold a runtime. It is enforced by
	// the runtime rather than watched from here: a call that ignores it is one
	// the process is not serving anybody else during.
	functionTimeout = 5 * time.Second

	// functionMaxPayload and functionMaxResult bound what crosses the boundary
	// in each direction.
	functionMaxPayload = 512 << 10
	functionMaxResult  = 512 << 10

	// functionMaxRows is the most rows one host read answers with, whatever the
	// function asked for.
	functionMaxRows = 500
)

func bindFunctionsApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	// Unbound from the default limiter for the same reason the record routes
	// are: every handler below counts against its own collection and action.
	sub := rg.Group("/functions").Unbind(DefaultRateLimitMiddlewareId)

	// Bound on the group rather than wrapped around each handler, so a route
	// added here later cannot be the one that forgets. It runs before the
	// handlers and before anything else bound on this group reads the path.
	sub.BindFunc(onFunctions)

	sub.GET("", recordsList)
	sub.POST("", recordCreate(true, nil))
	sub.GET("/{name}", recordView)
	sub.PATCH("/{name}", recordUpdate(true, nil))
	sub.DELETE("/{name}", recordDelete(true, nil))

	sub.POST("/{name}", functionInvoke)
}

// onFunctions says what every address under /v1/functions is about: the
// collection functions live in, and the function the path names.
//
// The record handlers read both off the path, so stating it here is what lets
// them serve this address unchanged.
func onFunctions(e *core.RequestEvent) error {
	e.Request.SetPathValue("collection", core.CollectionNameFunctions)
	if name := e.Request.PathValue("name"); name != "" {
		e.Request.SetPathValue("id", name)
	}
	return e.Next()
}

func functionInvoke(e *core.RequestEvent) error {
	collection, err := e.App.FindCachedCollectionByNameOrId(core.CollectionNameFunctions)
	if err != nil || collection == nil {
		return e.NotFoundError("Missing collection context.", err)
	}

	if err := checkCollectionRateLimit(e, collection, "invoke"); err != nil {
		return err
	}

	// The payload is read before the request is parsed into rule data, and the
	// body rewound after, so both see the same bytes.
	payload, err := io.ReadAll(io.LimitReader(e.Request.Body, functionMaxPayload+1))
	if body, ok := e.Request.Body.(router.Rereader); ok {
		body.Reread()
	}
	if err != nil {
		return e.BadRequestError("Failed to read the invocation payload.", err)
	}
	if len(payload) > functionMaxPayload {
		return e.BadRequestError("The invocation payload is too large.", nil)
	}

	requestInfo, err := e.RequestInfo()
	if err != nil {
		return firstApiError(err, e.BadRequestError("", err))
	}

	if collection.ViewRule == nil && !requestInfo.HasSuperuserAuth() {
		return e.ForbiddenError("Only superusers can perform this action.", nil)
	}

	// Which functions exist is an answer about the data, so a caller who may not
	// view this one is told the same thing as a caller who named one that was
	// never written.
	record, err := e.App.FindRecordById(collection, e.Request.PathValue("name"), viewGate(e.App, collection, requestInfo))
	if err != nil || record == nil {
		return e.NotFoundError("", err)
	}

	run := extruntime.Lookup(functionLang)
	if run == nil {
		return e.InternalServerError("No runtime is linked for "+functionLang+" functions.", nil)
	}

	ctx, cancel := context.WithTimeout(e.Request.Context(), functionTimeout)
	defer cancel()

	out, err := run(ctx, record.GetString(core.FieldNameSource), payload, functionHost(e, requestInfo))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return e.Error(http.StatusGatewayTimeout,
			"The function did not finish within "+functionTimeout.String()+".", nil)
	case err != nil:
		// The source is the deployment's, so the failure is the deployment's:
		// it is logged whole and answered as the server error it is.
		e.App.Logger().Error("base: function failed", "function", record.Id, "error", err)
		return e.InternalServerError("The function failed.", nil)
	}

	if len(out) > functionMaxResult {
		return e.InternalServerError("The function answered with more than "+
			strconv.Itoa(functionMaxResult)+" bytes.", nil)
	}

	return e.JSON(http.StatusOK, json.RawMessage(out))
}

// functionHost is what a function may ask of the Base it is running in: that
// Base's own records, read as its caller would read them.
//
// Both reads resolve everything from the request — the collection from e.App,
// which the credential already moved onto this org's Base, and the rule from the
// caller's own identity — so a function renders what its caller may see and is
// never a way around it.
func functionHost(e *core.RequestEvent, requestInfo *core.RequestInfo) extruntime.Host {
	return extruntime.Host{
		"list": func(arg []byte) ([]byte, error) { return functionListRead(e.App, requestInfo, arg) },
		"one":  func(arg []byte) ([]byte, error) { return functionOneRead(e.App, requestInfo, arg) },
	}
}

// readInfo is the caller, presented as the GET this read is.
//
// The identity and the headers carry over unchanged, because they are the
// caller's and are exactly what an HTTP read of the same row would see. The
// method and the body do not: `@request.body.x` in a rule is talking about a
// write, so letting the invocation payload answer it would let a caller satisfy
// a read rule by naming a field in the body they sent. The query is what this
// read asked for, which is what makes the guard below the same guard a client
// filtering the same collection meets.
func readInfo(requestInfo *core.RequestInfo, query map[string]string) *core.RequestInfo {
	out := requestInfo.Clone()
	out.Method = http.MethodGet
	out.Context = core.RequestInfoContextDefault
	out.Body = map[string]any{}
	out.Query = query
	return out
}

type functionRead struct {
	Collection string `json:"collection"`
	Id         string `json:"id"`
	Filter     string `json:"filter"`
	Sort       string `json:"sort"`
	Page       int    `json:"page"`
	PerPage    int    `json:"perPage"`
}

func (r functionRead) collection(app core.App) (*core.Collection, error) {
	collection, err := app.FindCachedCollectionByNameOrId(r.Collection)
	if err != nil || collection == nil {
		return nil, fmt.Errorf("no collection named %q", r.Collection)
	}
	return collection, nil
}

// functionOneRead answers with one record, or with null for a record the caller
// may not view and for a record that is not there — the same two answers
// /v1/collections gives, and in the same words, so which one it was cannot be
// read off the result.
func functionOneRead(app core.App, requestInfo *core.RequestInfo, arg []byte) ([]byte, error) {
	var in functionRead
	if err := json.Unmarshal(arg, &in); err != nil {
		return nil, err
	}
	collection, err := in.collection(app)
	if err != nil {
		return nil, err
	}

	read := readInfo(requestInfo, nil)

	record, err := app.FindRecordById(collection, in.Id, viewGate(app, collection, read))
	if err != nil || record == nil {
		return []byte("null"), nil
	}

	if err := functionEnrich(app, read, []*core.Record{record}); err != nil {
		return nil, err
	}
	return json.Marshal(record)
}

// functionListRead answers with the rows of one page, under the collection's
// list rule.
func functionListRead(app core.App, requestInfo *core.RequestInfo, arg []byte) ([]byte, error) {
	var in functionRead
	if err := json.Unmarshal(arg, &in); err != nil {
		return nil, err
	}
	collection, err := in.collection(app)
	if err != nil {
		return nil, err
	}

	read := readInfo(requestInfo, map[string]string{
		search.FilterQueryParam: in.Filter,
		search.SortQueryParam:   in.Sort,
	})

	// The same refusal a client meets for the same filter. A function runs as
	// its caller, so what its caller may not filter on, it may not either.
	if err := checkForSuperuserOnlyRuleFields(read); err != nil {
		return nil, err
	}

	if collection.ListRule == nil && !read.HasSuperuserAuth() {
		return nil, fmt.Errorf("only superusers can list collection %q records", collection.Name)
	}

	query := app.RecordQuery(collection)
	resolver := core.NewRecordFieldResolver(app, collection, read, true)

	if !read.HasSuperuserAuth() && collection.ListRule != nil && *collection.ListRule != "" {
		expr, err := search.FilterData(*collection.ListRule).BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		query.AndWhere(expr)
	}

	resolver.SetAllowHiddenFields(read.HasSuperuserAuth())

	perPage := in.PerPage
	if perPage <= 0 || perPage > functionMaxRows {
		perPage = functionMaxRows
	}

	params := url.Values{}
	if in.Filter != "" {
		params.Set(search.FilterQueryParam, in.Filter)
	}
	if in.Sort != "" {
		params.Set(search.SortQueryParam, in.Sort)
	}
	if in.Page > 0 {
		params.Set(search.PageQueryParam, strconv.Itoa(in.Page))
	}
	params.Set(search.PerPageQueryParam, strconv.Itoa(perPage))
	// A function asked for rows, so the count is work nobody wants done.
	params.Set(search.SkipTotalQueryParam, "1")

	records := []*core.Record{}
	if _, err := search.NewProvider(resolver).Query(query).ParseAndExec(params.Encode(), &records); err != nil {
		return nil, err
	}

	if err := functionEnrich(app, read, records); err != nil {
		return nil, err
	}
	return json.Marshal(records)
}

// functionEnrich runs what the HTTP read runs before serializing: the
// deployment's own enrich hooks, and the visibility a superuser has and nobody
// else does. Skipping it would make a function's view of a row differ from the
// wire's view of the same row.
func functionEnrich(app core.App, requestInfo *core.RequestInfo, records []*core.Record) error {
	if len(records) == 0 {
		return nil
	}
	return triggerRecordEnrichHooks(app, requestInfo, records, func() error {
		return autoResolveRecordsFlags(app, records, requestInfo)
	})
}

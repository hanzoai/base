package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

	// functionMaxRuns is the most runs one call starts, whatever it asks for.
	//
	// A rule an operator writes is what prices a run; this is what holds when
	// nobody has written one, which is every Base out of the box — rate limits
	// ship disabled, and a run is far too expensive to be unbounded until an
	// operator gets around to saying so.
	//
	// A function decides what work one request implies, and a handful covers
	// that. Fanning out wider is a program in its own right, and it belongs
	// inside a run, which has minutes for it rather than the seconds a function
	// is given.
	functionMaxRuns = 8
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

	call := &invocation{ctx: ctx, e: e, info: requestInfo, collection: collection}

	out, err := run(ctx, record.GetString(core.FieldNameSource), payload, call.host())
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return e.Error(http.StatusGatewayTimeout,
			"The function did not finish within "+functionTimeout.String()+".", nil)
	case err != nil && call.refusal() != nil:
		// The host turned this caller down and the function did not handle it,
		// so what the host decided is the answer. Reporting a server failure
		// here would describe a limit doing exactly its job as the deployment
		// being broken.
		return call.refusal()
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

// invocation is one call: what the function running may ask for, and the one
// answer the host is allowed to decide on the caller's behalf.
type invocation struct {
	ctx        context.Context
	e          *core.RequestEvent
	info       *core.RequestInfo
	collection *core.Collection

	mu      sync.Mutex
	refused error
	runs    int
}

// count records that this call is about to start another run, and refuses once
// it has started [functionMaxRuns]. Only an ask that survived its own checking
// gets here, so a malformed one costs nothing and takes nothing.
func (c *invocation) count() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs >= functionMaxRuns {
		return fmt.Errorf("start: one call starts at most %d runs", functionMaxRuns)
	}
	c.runs++
	return nil
}

// host is what a function may ask of the Base it is running in: that Base's own
// records, read as its caller would read them, and somewhere that is not this
// process to run work.
//
// Both reads resolve everything from the request — the collection from e.App,
// which the credential already moved onto this org's Base, and the rule from the
// caller's own identity — so a function renders what its caller may see and is
// never a way around it. The third name is about cost rather than sight: it
// spends the deployment's machines, so it is counted, and it answers with a
// name instead of a result.
func (c *invocation) host() extruntime.Host {
	return extruntime.Host{
		"list":  func(arg []byte) ([]byte, error) { return functionListRead(c.e.App, c.info, arg) },
		"one":   func(arg []byte) ([]byte, error) { return functionOneRead(c.e.App, c.info, arg) },
		"start": c.start,
	}
}

// refuse records an answer the host reached about the CALLER — a limit they are
// over — rather than about the function. Only the first is kept, because that
// is the one the call stopped on.
func (c *invocation) refuse(err error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refused == nil {
		c.refused = err
	}
	return err
}

// refusal reports what the host decided about the caller, if it decided
// anything.
func (c *invocation) refusal() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refused
}

// reservedLabel prefixes what a deployment writes about a run of its own
// accord. Whose work a sandbox is gets stamped there by whoever starts it, so a
// function's own labels stop short of it: the org a run belongs to has one
// source, and it is not the source that asked for the run.
const reservedLabel = "hanzo.ai/"

// start begins one sandbox for the org this request is acting in.
//
// It is the only thing a function may ask for that is not a read, so it is
// counted on its own. A sandbox holds a machine for as long as its work runs,
// and the invoke that asked for it is over in seconds, so the invoke cannot
// stand in for it.
//
// What comes back is a name. The work is not waited for and its result does not
// return through here: a function is answering a request, a run is not, and
// pretending they are the same call would make every run as short as a request.
func (c *invocation) start(arg []byte) ([]byte, error) {
	if err := checkCollectionRateLimit(c.e, c.collection, "start"); err != nil {
		return nil, c.refuse(err)
	}

	sandboxes, _ := c.e.App.Store().Get(StoreKeySandboxes).(Sandboxes)
	if sandboxes == nil {
		return nil, errors.New("start: this deployment has nowhere to run work")
	}

	// What a function hands over is built inside the runtime, so it is not the
	// invocation body and none of that body's bound reached it. It gets the
	// same one: an env or a label list larger than a whole request is not a
	// description of a run, it is the request being made twice.
	if len(arg) > functionMaxPayload {
		return nil, errors.New("start: the ask is too large")
	}

	// The fields of [Sandbox] are the whole vocabulary, so anything else is
	// refused rather than quietly dropped: a caller that wrote a field is a
	// caller that expected it to mean something, and whose work a run is is
	// exactly the meaning that is not on offer.
	dec := json.NewDecoder(bytes.NewReader(arg))
	dec.DisallowUnknownFields()

	var s Sandbox
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	if s.Snapshot == "" {
		return nil, errors.New("start: a run needs a snapshot to run as")
	}
	for name := range s.Labels {
		if strings.HasPrefix(name, reservedLabel) {
			return nil, fmt.Errorf("start: %q is the deployment's to write", name)
		}
	}

	if err := c.count(); err != nil {
		return nil, err
	}

	// The org is read off the request, where the credential resolved it before
	// any function ran, and passed beside what was asked for rather than folded
	// into it.
	//
	// A deployment that tells orgs apart is one where a run belongs to somebody,
	// so an unnamed org is refused there. A Base serving one tenant names no org
	// on any request and never has — there is nobody for a run to be confused
	// with — so the question is asked of the deployment rather than the request.
	org, _ := c.e.Get(RequestEventKeyOrg).(string)
	if org == "" && c.e.Deployment().Store().Get(StoreKeyBases) != nil {
		return nil, errors.New("start: a run needs an org on a Base that serves many")
	}

	id, err := sandboxes.Start(c.ctx, org, s)
	if err != nil {
		// Where work runs is the deployment's, so what went wrong there is the
		// deployment's: it is logged whole and the function is told only that
		// the run did not start. This is the rule functionInvoke already applies
		// to a failed function, applied one call earlier — a function may catch
		// what start raises and answer its own caller with it.
		c.e.App.Logger().Error("base: start failed",
			"function", c.e.Request.PathValue("name"), "org", org, "error", err)
		return nil, errors.New("start: the run did not start")
	}

	return json.Marshal(struct {
		Id string `json:"id"`
	}{Id: id})
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

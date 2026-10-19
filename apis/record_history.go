package apis

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// historyMaxPerPage bounds one page of a record's history.
const historyMaxPerPage = 200

// bindRecordHistoryApi registers the route that reads one record's history.
func bindRecordHistoryApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	rg.GET(recordsPath+"/{id}/history", recordHistory)
}

// historyEntry is one write to a record, as a reader sees it.
type historyEntry struct {
	Id      string          `json:"id"`
	Action  string          `json:"action"`
	Actor   types.Actor     `json:"actor"`
	Changes json.RawMessage `json:"changes"`
	Created types.DateTime  `json:"created"`
}

// recordHistory answers one record's history, newest first.
//
// Whoever may view the record may read its history, and nobody else: the
// record has to exist and pass its collection's view rule for the caller, the
// same gate GET .../records/{id} applies. A record that is gone has no rule to
// ask, so only a superuser reads the history of a deleted one (from _history).
func recordHistory(e *core.RequestEvent) error {
	collection, err := e.App.FindCachedCollectionByNameOrId(e.Request.PathValue("collection"))
	if err != nil || collection == nil {
		return e.NotFoundError("Missing collection context.", err)
	}

	if err := checkCollectionRateLimit(e, collection, "view"); err != nil {
		return err
	}

	requestInfo, err := e.RequestInfo()
	if err != nil {
		return firstApiError(err, e.BadRequestError("", err))
	}

	if collection.ViewRule == nil && !requestInfo.HasSuperuserAuth() {
		return e.ForbiddenError("Only superusers can perform this action.", nil)
	}

	record, err := e.App.FindRecordById(collection, e.Request.PathValue("id"), viewGate(e.App, collection, requestInfo))
	if err != nil || record == nil {
		return e.NotFoundError("", err)
	}

	history, err := e.App.FindCachedCollectionByNameOrId(core.CollectionNameHistory)
	if err != nil {
		return e.InternalServerError("This Base keeps no history.", err)
	}

	page, perPage := pageOf(e, historyMaxPerPage)

	where := dbx.HashExp{"collection": collection.Id, "record": record.Id}

	var total int
	if err := e.App.RecordQuery(history).Select("COUNT(*)").AndWhere(where).Row(&total); err != nil {
		return e.InternalServerError("", err)
	}

	order := []string{"created DESC"}
	if row := e.App.Dialect().Row(); row != "" {
		order = append(order, row+" DESC")
	}

	rows := []*core.Record{}
	err = e.App.RecordQuery(history).
		AndWhere(where).
		OrderBy(order...).
		Limit(int64(perPage)).
		Offset(int64(perPage * (page - 1))).
		All(&rows)
	if err != nil {
		return e.InternalServerError("", err)
	}

	items := make([]historyEntry, len(rows))
	for i, r := range rows {
		actor, _ := r.Get("actor").(types.Actor)
		items[i] = historyEntry{
			Id:      r.Id,
			Action:  r.GetString("action"),
			Actor:   actor,
			Changes: visibleChanges(collection, requestInfo, r.GetString("changes")),
			Created: r.GetDateTime("created"),
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"items":      items,
		"page":       page,
		"perPage":    perPage,
		"totalItems": total,
		"totalPages": int(math.Ceil(float64(total) / float64(perPage))),
	})
}

// visibleChanges drops the changes of fields the collection hides now, for a
// caller who is not a superuser. A field hidden when it changed was never
// recorded.
func visibleChanges(collection *core.Collection, requestInfo *core.RequestInfo, raw string) json.RawMessage {
	if requestInfo.HasSuperuserAuth() || raw == "" {
		return json.RawMessage(nonEmptyJSON(raw))
	}

	changes := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		return json.RawMessage(`{}`)
	}
	for name := range changes {
		if f := collection.Fields.GetByName(name); f != nil && f.GetHidden() {
			delete(changes, name)
		}
	}

	out, _ := json.Marshal(changes)
	return out
}

func nonEmptyJSON(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

// pageOf reads page and perPage off the query: page from 1, perPage 1..max,
// 30 when unstated.
func pageOf(e *core.RequestEvent, max int) (page, perPage int) {
	q := e.Request.URL.Query()

	page, _ = strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	perPage, _ = strconv.Atoi(q.Get("perPage"))
	switch {
	case perPage < 1:
		perPage = 30
	case perPage > max:
		perPage = max
	}

	return page, perPage
}

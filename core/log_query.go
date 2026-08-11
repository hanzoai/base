package core

import (
	"time"

	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// LogQuery returns a new Log select query.
func (app *BaseApp) LogQuery() *dbx.SelectQuery {
	return app.AuxModelQuery(&Log{})
}

// FindLogById finds a single Log entry by its id.
func (app *BaseApp) FindLogById(id string) (*Log, error) {
	model := &Log{}

	err := app.LogQuery().
		AndWhere(dbx.HashExp{"id": id}).
		Limit(1).
		One(model)

	if err != nil {
		return nil, err
	}

	return model, nil
}

// LogHour buckets a stored instant to the hour it falls in. An instant is
// stored as text in a fixed layout, so the bucket is the leading characters
// of that text rather than a calendar function — which is one expression on
// every engine, and one an index can be built on.
const LogHour = `(substr([[created]], 1, 13) || ':00:00')`

// LogsStatsItem defines the total number of logs for a specific time period.
type LogsStatsItem struct {
	Date  types.DateTime `db:"date" json:"date"`
	Total int            `db:"total" json:"total"`
}

// LogsStats returns hourly grouped logs statistics.
func (app *BaseApp) LogsStats(expr dbx.Expression) ([]*LogsStatsItem, error) {
	result := []*LogsStatsItem{}

	query := app.LogQuery().
		Select("count(id) as total", LogHour+" as date").
		GroupBy("date")

	if expr != nil {
		query.AndWhere(expr)
	}

	err := query.All(&result)

	return result, err
}

// DeleteOldLogs delete all logs that are created before createdBefore.
//
// For better performance the logs delete is executed as plain SQL statement,
// aka. no delete model hook events will be fired.
func (app *BaseApp) DeleteOldLogs(createdBefore time.Time) error {
	formattedDate := createdBefore.UTC().Format(types.DefaultDateLayout)
	expr := dbx.NewExp("[[created]] <= {:date}", dbx.Params{"date": formattedDate})

	_, err := app.auxNonconcurrentDB.Delete((&Log{}).TableName(), expr).Execute()

	return err
}

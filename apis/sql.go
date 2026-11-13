package apis

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

const (
	runSQLMaxRows    = 1000
	runSQLMaxTimeout = 3 * time.Minute
)

// bindSQLApi registers the SQL api endpoints.
func bindSQLApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/sql").Bind(RequireSuperuserAuth())
	subGroup.POST("", runSQL)
}

func runSQL(e *core.RequestEvent) error {
	// extra precaution in case manually invoked from somewhere else
	if !e.HasSuperuserAuth() {
		return e.ForbiddenError("", nil)
	}

	form := runSQLForm{}

	err := e.BindBody(&form)
	if err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}

	err = form.validate()
	if err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	result, err := executeQuery(e.App, form.Query, runSQLMaxRows)
	if err != nil {
		return firstApiError(err, e.BadRequestError("Failed to execute query. Raw error:\n"+err.Error(), nil))
	}

	return e.JSON(http.StatusOK, result)
}

type runSQLForm struct {
	Query string `form:"query" json:"query"`
}

func (form *runSQLForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(&form.Query, validation.Required, validation.Length(0, 5000)),
	)
}

type runSQLResultColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type runSQLResult struct {
	ExecTime     int64                `json:"execTime"`
	AffectedRows int64                `json:"affectedRows"`
	Columns      []runSQLResultColumn `json:"columns"`
	Rows         [][]any              `json:"rows"`
}

var knownWriteQueryPrefixes = []string{"INSERT", "CREATE", "UPDATE", "DELETE", "DROP", "DETACH"}

func executeQuery(app core.App, query string, maxRows int) (*runSQLResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		// see https://github.com/mattn/go-sqlite3/issues/950
		return nil, errors.New("empty query")
	}

	var isPossibleWriteQuery bool

	// loosely check the query type
	ucQuery := strings.ToUpper(query)
	if !strings.HasPrefix(ucQuery, "SELECT") {
		for _, prefix := range knownWriteQueryPrefixes {
			if strings.HasPrefix(ucQuery, prefix) {
				isPossibleWriteQuery = true
				break
			}
		}
	}

	// note: don't extend the request context to minimize the risk of
	// causing integrity issues with custom non-transaction mutations
	ctx, cancelFunc := context.WithTimeout(context.Background(), runSQLMaxTimeout)
	defer cancelFunc()

	result := &runSQLResult{
		// init empty slices to ensure "[]" serialization
		Columns: []runSQLResultColumn{},
		Rows:    [][]any{},
	}

	now := time.Now()
	defer func() {
		result.ExecTime = time.Since(now).Milliseconds()
	}()

	// assume write/mutation query
	// ---------------------------------------------------------------
	if isPossibleWriteQuery {
		// auto wrap in transaction in case there are multiple inline queries
		txErr := app.RunInTransaction(func(txApp core.App) error {
			for _, statement := range statementsOf(txApp, query) {
				execResult, err := txApp.NonconcurrentDB().NewQuery(statement).WithContext(ctx).Execute()
				if err != nil {
					return err
				}

				affected, err := execResult.RowsAffected()
				if err != nil {
					// non-critical error (e.g. not supported by the driver)
					txApp.Logger().Debug("Unable to fetch affected rows", slog.String("error", err.Error()))
				}
				result.AffectedRows = affected
			}

			return nil
		})
		if txErr != nil {
			return nil, txErr
		}

		return result, nil
	}

	// assume query returning rows; of several, the last one answers
	// ---------------------------------------------------------------
	statements := statementsOf(app, query)
	for _, statement := range statements[:len(statements)-1] {
		if _, err := app.ConcurrentDB().NewQuery(statement).WithContext(ctx).Execute(); err != nil {
			return nil, err
		}
	}
	query = statements[len(statements)-1]

	rows, err := app.ConcurrentDB().NewQuery(query).WithContext(ctx).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// populate columns info
	// ---
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	for _, colType := range colTypes {
		col := runSQLResultColumn{
			Name: colType.Name(),
			Type: colType.DatabaseTypeName(),
		}
		col.Nullable, _ = colType.Nullable()

		result.Columns = append(result.Columns, col)
	}

	// populate rows
	// ---
	for rows.Next() {
		if len(result.Rows) >= maxRows {
			break
		}

		rowData := make([]any, len(colTypes))
		for i := 0; i < len(colTypes); i++ {
			var v *string
			rowData[i] = &v
		}

		err := rows.Scan(rowData...)
		if err != nil {
			return nil, err
		}

		result.Rows = append(result.Rows, rowData)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return result, nil
}

// statementsOf is the query as the statements it holds. SQLite runs several at
// once; a server engine takes one to a prepared statement, so it is cut at the
// semicolons that are not inside a string, a quoted name, a dollar-quoted body
// or a comment. An engine that takes them whole gets them whole.
func statementsOf(app core.App, query string) []string {
	if core.LocalDatabase(app.Dialect()) {
		return []string{query}
	}

	var statements []string
	start := 0

	for i := 0; i < len(query); i++ {
		switch c := query[i]; {
		case c == '\'' || c == '"':
			i = skipPast(query, i+1, string(c))
		case c == '-' && strings.HasPrefix(query[i:], "--"):
			i = skipPast(query, i+2, "\n")
		case c == '/' && strings.HasPrefix(query[i:], "/*"):
			i = skipPast(query, i+2, "*/")
		case c == '$':
			end := strings.IndexByte(query[i+1:], '$')
			if end >= 0 && isDollarTag(query[i+1:i+1+end]) {
				tag := query[i : i+end+2]
				i = skipPast(query, i+end+2, tag)
			}
		case c == ';':
			if statement := strings.TrimSpace(query[start:i]); statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
	}

	if statement := strings.TrimSpace(query[start:]); statement != "" {
		statements = append(statements, statement)
	}
	if len(statements) == 0 {
		statements = []string{query}
	}

	return statements
}

// skipPast is the index of the last byte of the first end at or after from, or
// of the query when it is never closed.
func skipPast(query string, from int, end string) int {
	if from > len(query) {
		return len(query)
	}
	if at := strings.Index(query[from:], end); at >= 0 {
		return from + at + len(end) - 1
	}

	return len(query)
}

// isDollarTag reports whether tag can sit between the two dollars of a
// dollar-quote: empty, or a name that does not start with a digit.
func isDollarTag(tag string) bool {
	for i, r := range tag {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r > 127:
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}

	return true
}

package apis

import (
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/router"
)

// bindDatabaseApi registers the database api endpoints.
func bindDatabaseApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/database").Bind(RequireSuperuserAuth())
	subGroup.GET("", databaseRead)
	subGroup.POST("/reclaim", databaseReclaim)
}

func databaseRead(e *core.RequestEvent) error {
	db, err := core.DescribeDatabase(e.App)
	if err != nil {
		return firstApiError(err, e.InternalServerError("Failed to read the database.", err))
	}

	return e.JSON(http.StatusOK, db)
}

func databaseReclaim(e *core.RequestEvent) error {
	before, after, err := core.ReclaimDatabase(e.App)
	if err != nil {
		return firstApiError(err, e.InternalServerError("Failed to reclaim the unused space. Raw error:\n"+err.Error(), nil))
	}

	return e.JSON(http.StatusOK, struct {
		Before int64 `json:"before"`
		After  int64 `json:"after"`
	}{before, after})
}

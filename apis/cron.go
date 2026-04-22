package apis

import (
	"net/http"
	"slices"
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/cron"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/routine"
)

// bindCronApi registers the crons api endpoint.
func bindCronApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	subGroup := rg.Group("/crons").Bind(RequireSuperuserAuth())
	subGroup.GET("", cronsList)
	subGroup.POST("/{id}", cronRun)
}

func cronsList(e *core.RequestEvent) error {
	jobs := e.App.Cron().Jobs()

	slices.SortStableFunc(jobs, func(a, b *cron.Job) int {
		if strings.HasPrefix(a.Id(), "__hz") {
			return 1
		}
		if strings.HasPrefix(b.Id(), "__hz") {
			return -1
		}
		return strings.Compare(a.Id(), b.Id())
	})

	return e.JSON(http.StatusOK, jobs)
}

func cronRun(e *core.RequestEvent) error {
	cronId := e.Request.PathValue("id")

	if !e.App.Cron().HasJob(cronId) {
		return e.NotFoundError("Missing or invalid cron job", nil)
	}

	var foundJob *cron.Job
	for _, j := range e.App.Cron().Jobs() {
		if j.Id() == cronId {
			foundJob = j
			break
		}
	}
	if foundJob == nil {
		return e.NotFoundError("Missing or invalid cron job", nil)
	}

	routine.FireAndForget(func() {
		foundJob.Run()
	})

	return e.NoContent(http.StatusNoContent)
}

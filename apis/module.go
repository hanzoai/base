package apis

import (
	"errors"
	"net/http"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/modules"
	"github.com/hanzoai/base/tools/router"
	"github.com/spf13/cast"
)

// bindModuleApi registers the module routes: which modules this Base offers,
// and installing one. Both are schema routes, a superuser's or the org admin's.
func bindModuleApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/modules").Bind(RequireSchemaAuth())
	sub.GET("", modulesList)
	sub.POST("/{name}", moduleInstall)
}

// moduleInfo is one module as the list route answers it.
type moduleInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

func modulesList(e *core.RequestEvent) error {
	list := modules.List()
	out := make([]moduleInfo, len(list))
	for i, m := range list {
		out[i] = moduleInfo{Name: m.Name, Title: m.Title, Description: m.Description, Installed: m.Installed(e.App)}
	}
	return e.JSON(http.StatusOK, out)
}

// moduleInstall grows the named module into the request's Base. Installing a
// module twice changes nothing the second time; installing a newer version adds
// what it adds and keeps everything the org changed.
func moduleInstall(e *core.RequestEvent) error {
	name := e.Request.PathValue("name")

	m, ok := modules.Get(name)
	if !ok {
		return e.NotFoundError("No such module.", nil)
	}

	// An org admin installs into its own Base only collections it could have
	// made itself, so a module can never be the way an org admin reaches a
	// collection the schema routes keep for a superuser.
	if !e.HasSuperuserAuth() {
		for _, doc := range m.Collections {
			if err := orgAdminMayGrow(e.App, doc); err != nil {
				return err
			}
		}
	}

	growth, err := modules.Install(e.App, name)
	if err != nil {
		if errors.Is(err, modules.ErrUnknownModule) {
			return e.NotFoundError("No such module.", err)
		}
		return e.BadRequestError("Failed to install the module.", err)
	}

	if !e.HasSuperuserAuth() {
		org, _ := e.Get(RequestEventKeyOrg).(string)
		sub, _ := e.Get(RequestEventKeySub).(string)
		e.App.Logger().Info("base: schema change by an org admin",
			"org", org, "sub", sub, "action", "module install", "module", name,
			"created", growth.Created, "grown", growth.Grown)
	}

	return e.JSON(http.StatusOK, struct {
		Module string `json:"module"`
		*core.Growth
	}{name, growth})
}

// orgAdminMayGrow answers whether an org admin may grow a collection document:
// the collection it names, as it is and as the document describes it, has to be
// one the org admin manages.
func orgAdminMayGrow(app core.App, doc map[string]any) error {
	name := cast.ToString(doc["name"])

	described := core.NewCollection(cast.ToString(doc["type"]), name)
	described.System = cast.ToBool(doc["system"])
	if !SchemaManageable(described) {
		return router.NewForbiddenError("The module describes a collection only a superuser manages: "+name+".", nil)
	}

	if existing, err := app.FindCachedCollectionByNameOrId(name); err == nil && !SchemaManageable(existing) {
		return router.NewForbiddenError("The module grows a collection only a superuser manages: "+name+".", nil)
	}

	return nil
}

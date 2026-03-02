package functions

import (
	"log/slog"

	"github.com/hanzoai/base/core"
)

const collectionFunctions = "_functions"

func (p *plugin) ensureCollections() error {
	_, err := p.app.FindCollectionByNameOrId(collectionFunctions)
	if err == nil {
		return nil // already exists
	}

	c := core.NewBaseCollection(collectionFunctions)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "name", Required: true, Max: 255},
		&core.TextField{Name: "qualifiedName", Required: true, Max: 512},
		&core.TextField{Name: "tenantId", Required: true, Max: 255},
		&core.TextField{Name: "image", Max: 1024},
		&core.TextField{Name: "runtime", Max: 64},
		&core.TextField{Name: "handler", Max: 512},
		&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"deployed", "building", "error", "deleted"},
		},
		&core.JSONField{Name: "envVars"},
		&core.JSONField{Name: "metadata"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	p.app.Logger().Info("creating functions system collection", slog.String("name", collectionFunctions))
	return p.app.Save(c)
}

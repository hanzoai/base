package apis

import (
	"errors"
	"net/http"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core"
)

func collectionsImport(e *core.RequestEvent) error {
	form := new(collectionsImportForm)

	err := e.BindBody(form)
	if err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while loading the submitted data.", err))
	}

	err = form.validate()
	if err != nil {
		return firstApiError(err, e.BadRequestError("An error occurred while validating the submitted data.", err))
	}

	// An org admin grows and changes its own base collections. It deletes none
	// by omission: deleteMissing drops every collection the document leaves
	// out, the ones that are not the org's to touch among them.
	var names []string
	if !e.HasSuperuserAuth() {
		if form.DeleteMissing {
			return e.ForbiddenError("An org admin cannot import with deleteMissing.", nil)
		}

		current, after, err := importedCollections(e.App, form.Collections)
		if err != nil {
			return e.BadRequestError("An error occurred while loading the submitted data.", err)
		}
		if err := refuseUnmanageable(e, "import", append(current, after...)...); err != nil {
			return err
		}
		for _, c := range after {
			names = append(names, c.Name)
		}
	}

	event := new(core.CollectionsImportRequestEvent)
	event.RequestEvent = e
	event.CollectionsData = form.Collections
	event.DeleteMissing = form.DeleteMissing

	return event.App.OnCollectionsImportRequest().Trigger(event, func(e *core.CollectionsImportRequestEvent) error {
		importErr := e.App.ImportCollections(e.CollectionsData, form.DeleteMissing)
		if importErr == nil {
			logOrgAdminSchemaChange(e.RequestEvent, "import", names...)

			return execAfterSuccessTx(true, e.App, func() error {
				return e.NoContent(http.StatusNoContent)
			})
		}

		// validation failure
		var validationErrors validation.Errors
		if errors.As(importErr, &validationErrors) {
			return e.BadRequestError("Failed to import collections.", validationErrors)
		}

		// generic/db failure
		return e.BadRequestError("Failed to import collections.", validation.Errors{"collections": validation.NewError(
			"validation_collections_import_failure",
			"Failed to import the collections configuration. Raw error:\n"+importErr.Error(),
		)})
	})
}

// -------------------------------------------------------------------

type collectionsImportForm struct {
	Collections   []map[string]any `form:"collections" json:"collections"`
	DeleteMissing bool             `form:"deleteMissing" json:"deleteMissing"`
}

func (form *collectionsImportForm) validate() error {
	return validation.ValidateStruct(form,
		validation.Field(&form.Collections, validation.Required),
	)
}

package platform

import (
	"fmt"
	"strings"

	"github.com/hanzoai/base/core"
)

// CollectionTemplate defines a collection schema that gets cloned per org.
type CollectionTemplate struct {
	// Name is the base collection name (without org prefix).
	// The actual collection will be created as t_{slug}_{Name}.
	Name string

	// Type is the collection type: "base", "auth", or "view".
	// Defaults to "base" if empty.
	Type string

	// Fields defines the fields for the collection.
	Fields []core.Field
}

// OrgPrefix returns the collection name prefix for an org slug.
// Format: t_{slug}_
func OrgPrefix(slug string) string {
	return "t_" + slug + "_"
}

// ScopedQuery returns the prefixed collection name for an org.
// Example: ScopedQuery("acme", "tasks") returns "t_acme_tasks".
func ScopedQuery(orgSlug, collection string) string {
	return OrgPrefix(orgSlug) + collection
}

// CreateOrgCollections creates prefixed collections for an org from the
// given templates. Each template's Name is prefixed with t_{slug}_.
//
// Collections that already exist are skipped.
func CreateOrgCollections(app core.App, orgSlug string, templates []CollectionTemplate) error {
	prefix := OrgPrefix(orgSlug)
	var errs []string

	for _, tmpl := range templates {
		if tmpl.Name == "" {
			continue
		}

		fullName := prefix + tmpl.Name

		// Skip if already exists.
		_, err := app.FindCollectionByNameOrId(fullName)
		if err == nil {
			continue
		}

		colType := tmpl.Type
		if colType == "" {
			colType = core.CollectionTypeBase
		}

		collection := core.NewCollection(colType, fullName)

		// Add template fields.
		for _, f := range tmpl.Fields {
			collection.Fields.Add(f)
		}

		if err := app.Save(collection); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", fullName, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to create org collections: %s", strings.Join(errs, "; "))
	}

	return nil
}

// DeleteOrgCollections removes all collections with the org's prefix.
func DeleteOrgCollections(app core.App, orgSlug string) error {
	prefix := OrgPrefix(orgSlug)
	allCollections, err := app.FindAllCollections()
	if err != nil {
		return fmt.Errorf("failed to list collections: %w", err)
	}

	var errs []string
	for _, col := range allCollections {
		if strings.HasPrefix(col.Name, prefix) {
			if err := app.Delete(col); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", col.Name, err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete org collections: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ListOrgCollections returns all collection names belonging to an org.
func ListOrgCollections(app core.App, orgSlug string) ([]string, error) {
	prefix := OrgPrefix(orgSlug)
	allCollections, err := app.FindAllCollections()
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	var names []string
	for _, col := range allCollections {
		if strings.HasPrefix(col.Name, prefix) {
			// Return the name without prefix for cleaner display.
			names = append(names, strings.TrimPrefix(col.Name, prefix))
		}
	}

	return names, nil
}

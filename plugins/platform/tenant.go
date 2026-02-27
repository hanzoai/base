package platform

import (
	"fmt"
	"strings"

	"github.com/hanzoai/base/core"
)

// CollectionTemplate defines a collection schema that gets cloned per tenant.
type CollectionTemplate struct {
	// Name is the base collection name (without tenant prefix).
	// The actual collection will be created as t_{slug}_{Name}.
	Name string

	// Type is the collection type: "base", "auth", or "view".
	// Defaults to "base" if empty.
	Type string

	// Fields defines the fields for the collection.
	Fields []core.Field
}

// TenantPrefix returns the collection name prefix for a tenant slug.
// Format: t_{slug}_
func TenantPrefix(slug string) string {
	return "t_" + slug + "_"
}

// ScopedQuery returns the prefixed collection name for a tenant.
// Example: ScopedQuery("acme", "tasks") returns "t_acme_tasks".
func ScopedQuery(tenantSlug, collection string) string {
	return TenantPrefix(tenantSlug) + collection
}

// CreateTenantCollections creates prefixed collections for a tenant from the
// given templates. Each template's Name is prefixed with t_{slug}_.
//
// Collections that already exist are skipped.
func CreateTenantCollections(app core.App, tenantSlug string, templates []CollectionTemplate) error {
	prefix := TenantPrefix(tenantSlug)
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
		return fmt.Errorf("failed to create tenant collections: %s", strings.Join(errs, "; "))
	}

	return nil
}

// DeleteTenantCollections removes all collections with the tenant's prefix.
func DeleteTenantCollections(app core.App, tenantSlug string) error {
	prefix := TenantPrefix(tenantSlug)
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
		return fmt.Errorf("failed to delete tenant collections: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ListTenantCollections returns all collection names belonging to a tenant.
func ListTenantCollections(app core.App, tenantSlug string) ([]string, error) {
	prefix := TenantPrefix(tenantSlug)
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

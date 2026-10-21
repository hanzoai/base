package core

import (
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

var _ optionsValidator = (*collectionBaseOptions)(nil)

// collectionBaseOptions defines the options for the "base" type collection.
type collectionBaseOptions struct {
	// History keeps one _history row for every create, update and delete of a
	// record in this collection, written in the record's own transaction: who
	// did it, when, and which fields changed from what to what. Off unless a
	// collection asks for it.
	History bool `form:"history" json:"history,omitempty"`

	// Visitors admits a visitor to the collection: a person IAM vouched for
	// whose token acts in another org than the one whose Base this is. Off,
	// which is every collection until one asks, a visitor is refused here as
	// anywhere else. On, its rules decide, reading the visitor as
	// `@request.auth.role = "visitor"`. See [Collection.AdmitsVisitors].
	Visitors bool `form:"visitors" json:"visitors,omitempty"`

	// Label is what a person calls one record ("Person"), Plural what they
	// call many ("People"). Empty means the collection's name.
	Label  string `form:"label" json:"label,omitempty"`
	Plural string `form:"plural" json:"plural,omitempty"`

	// Icon names the icon drawn for the collection, in the same vocabulary
	// as a field's.
	Icon string `form:"icon" json:"icon,omitempty"`

	// Description says what the collection holds.
	Description string `form:"description" json:"description,omitempty"`
}

// AdmitsVisitors reports whether a visitor may reach the collection's records
// and files. Only a base collection can, and only when it says so.
func (c *Collection) AdmitsVisitors() bool {
	return c != nil && c.IsBase() && c.Visitors
}

func (o *collectionBaseOptions) validate(cv *collectionValidator) error {
	return validation.ValidateStruct(o,
		validation.Field(&o.Label, validation.Length(0, 255)),
		validation.Field(&o.Plural, validation.Length(0, 255)),
		validation.Field(&o.Icon, validation.Match(iconRegex)),
		validation.Field(&o.Description, validation.Length(0, 1000)),
	)
}

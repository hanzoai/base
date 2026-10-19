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

func (o *collectionBaseOptions) validate(cv *collectionValidator) error {
	return validation.ValidateStruct(o,
		validation.Field(&o.Label, validation.Length(0, 255)),
		validation.Field(&o.Plural, validation.Length(0, 255)),
		validation.Field(&o.Icon, validation.Match(iconRegex)),
		validation.Field(&o.Description, validation.Length(0, 1000)),
	)
}

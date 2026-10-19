package core

import (
	"regexp"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// FieldMeta is what a person reads about a field: the label it is shown under,
// what it is for, the icon drawn beside it and where it sits among the others.
// It changes nothing about the value, the column or the rules.
//
// Every field type embeds it, so it travels with the field in the collection's
// own schema (an export, an import and a module carry it) and there is no second
// table of presentation rows to keep in step with the fields it describes.
type FieldMeta struct {
	// Label is the name a person reads. Empty means the field's name.
	Label string `form:"label" json:"label,omitempty"`

	// Description says what the field holds, for the person filling it in.
	Description string `form:"description" json:"description,omitempty"`

	// Icon names the icon drawn beside the field (a Tabler icon name, such as
	// "IconUser" or "user").
	Icon string `form:"icon" json:"icon,omitempty"`

	// Position orders the field among the collection's fields where a client
	// lays them out. Ties fall back to the schema's own order.
	Position int `form:"position" json:"position,omitempty"`
}

// Meta returns the field's presentation, for a caller holding the Field
// interface (every field type embeds FieldMeta, so every field answers it).
func (m *FieldMeta) Meta() *FieldMeta {
	return m
}

// FieldMetaHolder is a field that carries [FieldMeta].
type FieldMetaHolder interface {
	Meta() *FieldMeta
}

var iconRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Validate checks the presentation's bounds.
func (m *FieldMeta) Validate() error {
	return validation.ValidateStruct(m,
		validation.Field(&m.Label, validation.Length(0, 255)),
		validation.Field(&m.Description, validation.Length(0, 1000)),
		validation.Field(&m.Icon, validation.Match(iconRegex)),
		validation.Field(&m.Position, validation.Min(0)),
	)
}

// TagColors is the palette a select option, a collection or a view may be
// coloured from. Gray is the default.
var TagColors = []string{
	"red", "ruby", "crimson", "tomato", "orange", "amber", "yellow", "lime",
	"grass", "green", "jade", "mint", "turquoise", "cyan", "sky", "blue",
	"iris", "violet", "purple", "plum", "pink", "bronze", "gold", "brown", "gray",
}

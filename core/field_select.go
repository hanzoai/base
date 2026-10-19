package core

import (
	"context"
	"database/sql/driver"
	"slices"
	"strconv"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/tools/list"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeSelect] = func() Field {
		return &SelectField{}
	}
}

const FieldTypeSelect = "select"

var (
	_ Field        = (*SelectField)(nil)
	_ MultiValuer  = (*SelectField)(nil)
	_ DriverValuer = (*SelectField)(nil)
	_ SetterFinder = (*SelectField)(nil)
)

// SelectField defines "select" type field for storing single or
// multiple string values from a predefined list.
//
// Requires the Values option to be set.
//
// If MaxSelect is not set or <= 1, then the field value is expected to be a single Values element.
//
// If MaxSelect is > 1, then the field value is expected to be a subset of Values slice.
//
// The respective zero record field value is either empty string (single) or empty string slice (multiple).
//
// ---
//
// The following additional setter keys are available:
//
//   - "fieldName+" - append one or more values to the existing record one. For example:
//
//     record.Set("roles+", []string{"new1", "new2"}) // []string{"old1", "old2", "new1", "new2"}
//
//   - "+fieldName" - prepend one or more values to the existing record one. For example:
//
//     record.Set("+roles", []string{"new1", "new2"}) // []string{"new1", "new2", "old1", "old2"}
//
//   - "fieldName-" - subtract one or more values from the existing record one. For example:
//
//     record.Set("roles-", "old1") // []string{"old2"}
type SelectField struct {
	// Name (required) is the unique name of the field.
	Name string `form:"name" json:"name"`

	// Id is the unique stable field identifier.
	//
	// It is automatically generated from the name when adding to a collection FieldsList.
	Id string `form:"id" json:"id"`

	// System prevents the renaming and removal of the field.
	System bool `form:"system" json:"system"`

	// Hidden hides the field from the API response.
	Hidden bool `form:"hidden" json:"hidden"`

	// Presentable hints the Dashboard UI to use the underlying
	// field record value in the relation preview label.
	Presentable bool `form:"presentable" json:"presentable"`

	// FieldMeta is how a person sees the field: label, description, icon
	// and position. It changes nothing about the value.
	FieldMeta

	// ---

	// Values specifies the list of accepted values.
	Values []string `form:"values" json:"values"`

	// MaxSelect specifies the max allowed selected values.
	//
	// For multiple select the value must be > 1, otherwise fallbacks to single (default).
	MaxSelect int `form:"maxSelect" json:"maxSelect"`

	// Required will require the field value to be non-empty.
	Required bool `form:"required" json:"required"`

	// Options says how each value is shown: the label a person reads, its
	// colour and its place in a list (a board's columns, a picker's order). A
	// value without an option reads as itself, in gray, in the order of Values.
	// An option names a value of Values and nothing else, so what may be stored
	// stays Values' to say.
	Options []SelectOption `form:"options" json:"options,omitempty"`
}

// SelectOption is how one value of a select field is shown.
type SelectOption struct {
	// Value is the value of Values this option is for.
	Value string `form:"value" json:"value"`

	// Label is what a person reads. Empty means the value itself.
	Label string `form:"label" json:"label,omitempty"`

	// Color is one of [TagColors]. Empty means gray.
	Color string `form:"color" json:"color,omitempty"`

	// Position orders the value among the others.
	Position int `form:"position" json:"position,omitempty"`
}

// Type implements [Field.Type] interface method.
func (f *SelectField) Type() string {
	return FieldTypeSelect
}

// GetId implements [Field.GetId] interface method.
func (f *SelectField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *SelectField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *SelectField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *SelectField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *SelectField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *SelectField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *SelectField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *SelectField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// IsMultiple implements [MultiValuer] interface and checks whether the
// current field options support multiple values.
func (f *SelectField) IsMultiple() bool {
	return f.MaxSelect > 1
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *SelectField) ColumnType(app App) string {
	if f.IsMultiple() {
		return app.Dialect().Json() + " DEFAULT '[]' NOT NULL"
	}

	return "TEXT DEFAULT '' NOT NULL"
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *SelectField) PrepareValue(record *Record, raw any) (any, error) {
	return f.normalizeValue(raw), nil
}

func (f *SelectField) normalizeValue(raw any) any {
	val := list.ToUniqueStringSlice(raw)

	if !f.IsMultiple() {
		if len(val) > 0 {
			return val[len(val)-1] // the last selected
		}
		return ""
	}

	return val
}

// DriverValue implements the [DriverValuer] interface.
func (f *SelectField) DriverValue(record *Record) (driver.Value, error) {
	val := list.ToUniqueStringSlice(record.GetRaw(f.Name))

	if !f.IsMultiple() {
		if len(val) > 0 {
			return val[len(val)-1], nil // the last selected
		}
		return "", nil
	}

	// serialize as json string array
	return append(types.JSONArray[string]{}, val...), nil
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *SelectField) ValidateValue(ctx context.Context, app App, record *Record) error {
	normalizedVal := list.ToUniqueStringSlice(record.GetRaw(f.Name))
	if len(normalizedVal) == 0 {
		if f.Required {
			return validation.ErrRequired
		}
		return nil // nothing to check
	}

	maxSelect := max(f.MaxSelect, 1)

	// check max selected items
	if len(normalizedVal) > maxSelect {
		return validation.NewError("validation_too_many_values", "Select no more than {{.maxSelect}}").
			SetParams(map[string]any{"maxSelect": maxSelect})
	}

	// check against the allowed values
	for _, val := range normalizedVal {
		if !slices.Contains(f.Values, val) {
			return validation.NewError("validation_invalid_value", "Invalid value {{.value}}").
				SetParams(map[string]any{"value": val})
		}
	}

	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *SelectField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	max := len(f.Values)
	if max == 0 {
		max = 1
	}

	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(&f.Values, validation.Required),
		validation.Field(&f.MaxSelect, validation.Min(0), validation.Max(max)),
		validation.Field(&f.Options, validation.By(f.checkOptions)),
	)
}

// checkOptions accepts one option per value, each for a value of Values, with a
// colour from the palette.
func (f *SelectField) checkOptions(value any) error {
	seen := make(map[string]struct{}, len(f.Options))

	for i, o := range f.Options {
		var err error
		switch _, dup := seen[o.Value]; {
		case !slices.Contains(f.Values, o.Value):
			err = validation.NewError("validation_unknown_option", "The option is for a value that is not one of the field's values.")
		case dup:
			err = validation.NewError("validation_duplicated_option", "The value has more than one option.")
		case o.Color != "" && !slices.Contains(TagColors, o.Color):
			err = validation.NewError("validation_invalid_color", "The color is not one of the palette's.")
		case len(o.Label) > 255:
			err = validation.NewError("validation_label_too_long", "The label is longer than 255 characters.")
		case o.Position < 0:
			err = validation.NewError("validation_invalid_position", "The position must not be negative.")
		}
		if err != nil {
			return validation.Errors{strconv.Itoa(i): err}
		}
		seen[o.Value] = struct{}{}
	}

	return nil
}

// FindSetter implements the [SetterFinder] interface.
func (f *SelectField) FindSetter(key string) SetterFunc {
	switch key {
	case f.Name:
		return f.setValue
	case "+" + f.Name:
		return f.prependValue
	case f.Name + "+":
		return f.appendValue
	case f.Name + "-":
		return f.subtractValue
	default:
		return nil
	}
}

func (f *SelectField) setValue(record *Record, raw any) {
	record.SetRaw(f.Name, f.normalizeValue(raw))
}

func (f *SelectField) appendValue(record *Record, modifierValue any) {
	val := record.GetRaw(f.Name)

	val = append(
		list.ToUniqueStringSlice(val),
		list.ToUniqueStringSlice(modifierValue)...,
	)

	f.setValue(record, val)
}

func (f *SelectField) prependValue(record *Record, modifierValue any) {
	val := record.GetRaw(f.Name)

	val = append(
		list.ToUniqueStringSlice(modifierValue),
		list.ToUniqueStringSlice(val)...,
	)

	f.setValue(record, val)
}

func (f *SelectField) subtractValue(record *Record, modifierValue any) {
	val := record.GetRaw(f.Name)

	val = list.SubtractSlice(
		list.ToUniqueStringSlice(val),
		list.ToUniqueStringSlice(modifierValue),
	)

	f.setValue(record, val)
}

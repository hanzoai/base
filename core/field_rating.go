package core

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/spf13/cast"
)

func init() {
	Fields[FieldTypeRating] = func() Field {
		return &RatingField{}
	}
}

const FieldTypeRating = "rating"

// defaultRatingMax is how many levels a rating has when its field sets none.
const defaultRatingMax = 5

var (
	_ Field = (*RatingField)(nil)
)

// RatingField defines "rating" type field for storing one of 1..Max levels (five
// stars by default) as an integer, with 0 meaning not rated. Filters, sorts and
// aggregates read it as a number (`rating >= 4`).
type RatingField struct {
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

	// Max is the number of levels, 1 to 10. 0 means 5.
	Max int `form:"max" json:"max"`

	// Required will require a rating of at least 1.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *RatingField) Type() string {
	return FieldTypeRating
}

// GetId implements [Field.GetId] interface method.
func (f *RatingField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *RatingField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *RatingField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *RatingField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *RatingField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *RatingField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *RatingField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *RatingField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *RatingField) ColumnType(app App) string {
	return "INTEGER DEFAULT 0 NOT NULL"
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *RatingField) PrepareValue(record *Record, raw any) (any, error) {
	return cast.ToInt(raw), nil
}

// Levels returns the field's number of levels.
func (f *RatingField) Levels() int {
	if f.Max <= 0 {
		return defaultRatingMax
	}
	return f.Max
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *RatingField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(int)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v == 0 {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	if v < 0 || v > f.Levels() {
		return validation.NewError("validation_rating_range", "Must be a rating from 1 to {{.max}}.").
			SetParams(map[string]any{"max": f.Levels()})
	}

	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *RatingField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(&f.Max, validation.Min(0), validation.Max(10)),
	)
}

package core

import (
	"context"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeFullName] = func() Field {
		return &FullNameField{}
	}
}

const FieldTypeFullName = "fullName"

var (
	_ Field          = (*FullNameField)(nil)
	_ CompositeField = (*FullNameField)(nil)
	_ TextParts      = (*FullNameField)(nil)
)

// FullNameField defines "fullName" type field for storing a person's name as a
// first and a last name, as [types.FullName]. Each part is trimmed and holds at
// most 255 characters. Sort by either part (`sort=name.lastName`).
type FullNameField struct {
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

	// Required will require at least one of the two parts.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *FullNameField) Type() string {
	return FieldTypeFullName
}

// Parts implements [CompositeField.Parts] interface method.
func (f *FullNameField) Parts() []string {
	return []string{"firstName", "lastName"}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *FullNameField) TextParts() []string {
	return f.Parts()
}

// GetId implements [Field.GetId] interface method.
func (f *FullNameField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *FullNameField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *FullNameField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *FullNameField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *FullNameField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *FullNameField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *FullNameField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *FullNameField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *FullNameField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *FullNameField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.FullName{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	v.FirstName = strings.TrimSpace(v.FirstName)
	v.LastName = strings.TrimSpace(v.LastName)

	return v, nil
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *FullNameField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.FullName)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v.IsZero() {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	return validation.ValidateStruct(&v,
		validation.Field(&v.FirstName, validation.Length(0, 255)),
		validation.Field(&v.LastName, validation.Length(0, 255)),
	)
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *FullNameField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
	)
}

package core

import (
	"context"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeAddress] = func() Field {
		return &AddressField{}
	}
}

const FieldTypeAddress = "address"

var (
	_ Field          = (*AddressField)(nil)
	_ CompositeField = (*AddressField)(nil)
	_ TextParts      = (*AddressField)(nil)
)

// AddressField defines "address" type field for storing a postal address, and
// its coordinates when they are known, as [types.Address].
//
// Every text part is trimmed and holds at most 255 characters. A latitude is
// within ±90 and a longitude within ±180.
type AddressField struct {
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

	// DefaultCountry is the country an address written without one is in.
	DefaultCountry string `form:"defaultCountry" json:"defaultCountry"`

	// Required will require the field value to be non-empty.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *AddressField) Type() string {
	return FieldTypeAddress
}

// Parts implements [CompositeField.Parts] interface method.
func (f *AddressField) Parts() []string {
	return []string{
		"addressStreet1", "addressStreet2", "addressCity", "addressState",
		"addressPostcode", "addressCountry", "addressLat", "addressLng",
	}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *AddressField) TextParts() []string {
	return f.Parts()[:6]
}

// GetId implements [Field.GetId] interface method.
func (f *AddressField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *AddressField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *AddressField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *AddressField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *AddressField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *AddressField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *AddressField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *AddressField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *AddressField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *AddressField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Address{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	for _, s := range []*string{
		&v.AddressStreet1, &v.AddressStreet2, &v.AddressCity,
		&v.AddressState, &v.AddressPostcode, &v.AddressCountry,
	} {
		*s = strings.TrimSpace(*s)
	}
	if v.AddressCountry == "" && !v.IsZero() {
		v.AddressCountry = f.DefaultCountry
	}

	return v, nil
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *AddressField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.Address)
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
		validation.Field(&v.AddressStreet1, validation.Length(0, 255)),
		validation.Field(&v.AddressStreet2, validation.Length(0, 255)),
		validation.Field(&v.AddressCity, validation.Length(0, 255)),
		validation.Field(&v.AddressState, validation.Length(0, 255)),
		validation.Field(&v.AddressPostcode, validation.Length(0, 255)),
		validation.Field(&v.AddressCountry, validation.Length(0, 255)),
		validation.Field(&v.AddressLat, validation.Min(-90.0), validation.Max(90.0)),
		validation.Field(&v.AddressLng, validation.Min(-180.0), validation.Max(180.0)),
	)
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *AddressField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(&f.DefaultCountry, validation.Length(0, 255)),
	)
}

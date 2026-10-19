package core

import (
	"context"
	"slices"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeEmails] = func() Field {
		return &EmailsField{}
	}
}

const FieldTypeEmails = "emails"

var (
	_ Field          = (*EmailsField)(nil)
	_ CompositeField = (*EmailsField)(nil)
	_ TextParts      = (*EmailsField)(nil)
)

// EmailsField defines "emails" type field for storing a primary email address
// and any others, as [types.Emails].
//
// Addresses are trimmed and lower-cased, blanks and repeats are dropped, and a
// value with no primary address promotes its first other one. With Unique set,
// no two records of the collection share a primary address.
//
// Examples of setting the value programmatically:
//
//	record.Set("emails", types.Emails{PrimaryEmail: "ada@example.com"})
//	record.Set("emails", map[string]any{"primaryEmail": "ada@example.com", "additionalEmails": []string{"ada@work.example"}})
type EmailsField struct {
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

	// MaxValues is how many addresses the value may hold, the primary one
	// included. 0 means 10.
	MaxValues int `form:"maxValues" json:"maxValues"`

	// Unique refuses a primary address another record already has.
	Unique bool `form:"unique" json:"unique"`

	// Required will require the field value to hold at least one address.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *EmailsField) Type() string {
	return FieldTypeEmails
}

// Parts implements [CompositeField.Parts] interface method.
func (f *EmailsField) Parts() []string {
	return []string{"primaryEmail", "additionalEmails"}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *EmailsField) TextParts() []string {
	return f.Parts()
}

// GetId implements [Field.GetId] interface method.
func (f *EmailsField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *EmailsField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *EmailsField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *EmailsField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *EmailsField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *EmailsField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *EmailsField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *EmailsField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *EmailsField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *EmailsField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Emails{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	all := normalizeList(append([]string{v.PrimaryEmail}, v.AdditionalEmails...), func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	})
	if len(all) == 0 {
		return types.Emails{}, nil
	}

	return types.Emails{PrimaryEmail: all[0], AdditionalEmails: all[1:]}, nil
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *EmailsField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.Emails)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v.IsZero() {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	if 1+len(v.AdditionalEmails) > maxValuesOf(f.MaxValues) {
		return validation.NewError("validation_too_many_values", "Holds more than {{.max}} values.").
			SetParams(map[string]any{"max": maxValuesOf(f.MaxValues)})
	}

	if err := is.EmailFormat.Validate(v.PrimaryEmail); err != nil {
		return validation.Errors{"primaryEmail": err}
	}
	for i, e := range v.AdditionalEmails {
		if err := is.EmailFormat.Validate(e); err != nil {
			return validation.Errors{"additionalEmails": validation.Errors{strconv.Itoa(i): err}}
		}
	}

	if f.Unique {
		if err := checkUniquePart(ctx, app, record, f.Name, "primaryEmail", v.PrimaryEmail); err != nil {
			return validation.Errors{"primaryEmail": err}
		}
	}

	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *EmailsField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validateMaxValues(&f.MaxValues),
	)
}

// normalizeList applies norm to every item and drops the blanks and the repeats,
// keeping the first of each in order.
func normalizeList(items []string, norm func(string) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = norm(item)
		if item == "" || slices.Contains(out, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

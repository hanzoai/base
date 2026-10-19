package core

import (
	"context"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
	"golang.org/x/text/currency"
)

func init() {
	Fields[FieldTypeCurrency] = func() Field {
		return &CurrencyField{}
	}
}

const FieldTypeCurrency = "currency"

var (
	_ Field          = (*CurrencyField)(nil)
	_ CompositeField = (*CurrencyField)(nil)
)

// CurrencyField defines "currency" type field for storing an amount of money in
// one currency, as [types.Currency]: amountMicros, the amount in millionths of
// the currency's major unit, and currencyCode, its ISO 4217 code.
//
// A value with an amount and no code takes the field's DefaultCurrency, then
// USD. The code is upper-cased. Filters and sorts read the parts
// (`amount.amountMicros >= 1000000000`, `amount.currencyCode = "EUR"`).
//
// Examples of setting the value programmatically:
//
//	record.Set("amount", types.Currency{AmountMicros: 1_500_000, CurrencyCode: "USD"})
//	record.Set("amount", map[string]any{"amountMicros": 1500000, "currencyCode": "USD"})
type CurrencyField struct {
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

	// DefaultCurrency is the ISO 4217 code a value with no code of its own
	// takes. Empty means USD.
	DefaultCurrency string `form:"defaultCurrency" json:"defaultCurrency"`

	// Required will require the field value to be non-empty.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *CurrencyField) Type() string {
	return FieldTypeCurrency
}

// Parts implements [CompositeField.Parts] interface method.
func (f *CurrencyField) Parts() []string {
	return []string{"amountMicros", "currencyCode"}
}

// GetId implements [Field.GetId] interface method.
func (f *CurrencyField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *CurrencyField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *CurrencyField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *CurrencyField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *CurrencyField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *CurrencyField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *CurrencyField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *CurrencyField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *CurrencyField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *CurrencyField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Currency{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	v.CurrencyCode = strings.ToUpper(strings.TrimSpace(v.CurrencyCode))
	if v.CurrencyCode == "" && v.AmountMicros != 0 {
		v.CurrencyCode = f.defaultCurrency()
	}

	return v, nil
}

func (f *CurrencyField) defaultCurrency() string {
	if f.DefaultCurrency != "" {
		return f.DefaultCurrency
	}
	return "USD"
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *CurrencyField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.Currency)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v.IsZero() {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	if v.AmountMicros > maxSafeJSONInt || v.AmountMicros < -maxSafeJSONInt {
		return validation.NewError("validation_currency_amount", "The amount is outside the range a JSON number holds exactly.")
	}

	return validation.Validate(v.CurrencyCode, validation.By(isCurrencyCode))
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *CurrencyField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(&f.DefaultCurrency, validation.By(isCurrencyCode)),
	)
}

// isCurrencyCode accepts an empty string or an ISO 4217 code in upper case.
func isCurrencyCode(value any) error {
	code, _ := value.(string)
	if code == "" {
		return nil
	}

	if code != strings.ToUpper(code) {
		return validation.NewError("validation_currency_code", "The currency code must be an ISO 4217 code in upper case.")
	}

	if _, err := currency.ParseISO(code); err != nil {
		return validation.NewError("validation_currency_code", "Unknown ISO 4217 currency code.")
	}

	return nil
}

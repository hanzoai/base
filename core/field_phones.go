package core

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypePhones] = func() Field {
		return &PhonesField{}
	}
}

const FieldTypePhones = "phones"

var (
	_ Field          = (*PhonesField)(nil)
	_ CompositeField = (*PhonesField)(nil)
	_ TextParts      = (*PhonesField)(nil)
)

var (
	phoneNumberRegex = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
	callingCodeRegex = regexp.MustCompile(`^\+[0-9]{1,4}$`)
	countryCodeRegex = regexp.MustCompile(`^[A-Z]{2}$`)
)

// PhonesField defines "phones" type field for storing a primary phone number and
// any others, as [types.Phones].
//
// A number keeps its digits and nothing else ("(415) 555-0100" is "4155550100"),
// or a leading + when it is written whole in E.164 with no calling code beside
// it. A country code is ISO 3166-1 alpha-2 in upper case and a calling code is
// "+" and its digits. A number with no country takes DefaultCountry. Blanks and
// repeats are dropped, and a value with no primary number promotes its first
// other one. With Unique set, no two records share a primary number.
type PhonesField struct {
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

	// DefaultCountry is the ISO 3166-1 alpha-2 country a number with none of
	// its own is dialled in.
	DefaultCountry string `form:"defaultCountry" json:"defaultCountry"`

	// MaxValues is how many numbers the value may hold, the primary one
	// included. 0 means 10.
	MaxValues int `form:"maxValues" json:"maxValues"`

	// Unique refuses a primary number another record already has.
	Unique bool `form:"unique" json:"unique"`

	// Required will require the field value to hold at least one number.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *PhonesField) Type() string {
	return FieldTypePhones
}

// Parts implements [CompositeField.Parts] interface method.
func (f *PhonesField) Parts() []string {
	return []string{"primaryPhoneNumber", "primaryPhoneCountryCode", "primaryPhoneCallingCode", "additionalPhones"}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *PhonesField) TextParts() []string {
	return []string{"primaryPhoneNumber", "additionalPhones"}
}

// GetId implements [Field.GetId] interface method.
func (f *PhonesField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *PhonesField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *PhonesField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *PhonesField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *PhonesField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *PhonesField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *PhonesField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *PhonesField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *PhonesField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *PhonesField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Phones{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	all := make([]types.Phone, 0, 1+len(v.AdditionalPhones))
	for _, p := range append([]types.Phone{{
		Number:      v.PrimaryPhoneNumber,
		CountryCode: v.PrimaryPhoneCountryCode,
		CallingCode: v.PrimaryPhoneCallingCode,
	}}, v.AdditionalPhones...) {
		p = f.normalize(p)
		if p.Number == "" || containsPhone(all, p) {
			continue
		}
		all = append(all, p)
	}
	if len(all) == 0 {
		return types.Phones{}, nil
	}

	return types.Phones{
		PrimaryPhoneNumber:      all[0].Number,
		PrimaryPhoneCountryCode: all[0].CountryCode,
		PrimaryPhoneCallingCode: all[0].CallingCode,
		AdditionalPhones:        all[1:],
	}, nil
}

func (f *PhonesField) normalize(p types.Phone) types.Phone {
	p.Number = PhoneDigits(p.Number)
	p.CountryCode = strings.ToUpper(strings.TrimSpace(p.CountryCode))
	p.CallingCode = strings.TrimSpace(p.CallingCode)
	if p.CallingCode != "" && !strings.HasPrefix(p.CallingCode, "+") {
		p.CallingCode = "+" + p.CallingCode
	}
	if p.Number != "" && p.CountryCode == "" {
		p.CountryCode = f.DefaultCountry
	}
	return p
}

// PhoneDigits keeps a number's digits and a leading "+", which is how a phones
// field stores a number and how a search for one compares it.
func PhoneDigits(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for i, r := range s {
		if r >= '0' && r <= '9' || (r == '+' && i == 0) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsPhone(all []types.Phone, p types.Phone) bool {
	for _, q := range all {
		if q.Number == p.Number && q.CallingCode == p.CallingCode {
			return true
		}
	}
	return false
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *PhonesField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.Phones)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v.IsZero() {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	if 1+len(v.AdditionalPhones) > maxValuesOf(f.MaxValues) {
		return validation.NewError("validation_too_many_values", "Holds more than {{.max}} values.").
			SetParams(map[string]any{"max": maxValuesOf(f.MaxValues)})
	}

	if err := validatePhone(types.Phone{
		Number:      v.PrimaryPhoneNumber,
		CountryCode: v.PrimaryPhoneCountryCode,
		CallingCode: v.PrimaryPhoneCallingCode,
	}); err != nil {
		// the primary number's parts are named as the value names them
		primary := validation.Errors{}
		for k, e := range err.(validation.Errors) {
			primary["primaryPhone"+strings.ToUpper(k[:1])+k[1:]] = e
		}
		return primary
	}
	for i, p := range v.AdditionalPhones {
		if err := validatePhone(p); err != nil {
			return validation.Errors{"additionalPhones": validation.Errors{strconv.Itoa(i): err}}
		}
	}

	if f.Unique {
		if err := checkUniquePart(ctx, app, record, f.Name, "primaryPhoneNumber", v.PrimaryPhoneNumber); err != nil {
			return validation.Errors{"primaryPhoneNumber": err}
		}
	}

	return nil
}

func validatePhone(p types.Phone) error {
	// the errors are validation.Errors keyed by part, or nil
	errs := validation.Errors{}
	if !phoneNumberRegex.MatchString(p.Number) {
		errs["number"] = validation.NewError("validation_invalid_phone", "Must be a phone number of 3 to 20 digits.")
	}
	if p.CountryCode != "" && !countryCodeRegex.MatchString(p.CountryCode) {
		errs["countryCode"] = validation.NewError("validation_invalid_country_code", "Must be an ISO 3166-1 alpha-2 country code.")
	}
	if p.CallingCode != "" && !callingCodeRegex.MatchString(p.CallingCode) {
		errs["callingCode"] = validation.NewError("validation_invalid_calling_code", `Must be "+" and one to four digits.`)
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *PhonesField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(&f.DefaultCountry, validation.Match(countryCodeRegex)),
		validateMaxValues(&f.MaxValues),
	)
}

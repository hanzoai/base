package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// The composite values: one field holding a JSON object of named parts.
//
// Each one is stored as a JSON document in a single column, so a filter or a sort
// reaches a part the way it reaches into any JSON field (`emails.primaryEmail`,
// `amount.amountMicros`). An empty value is NULL in the column and null on the
// wire, so "no value" is one fact a filter can ask about (`emails = null`) rather
// than an object of blanks that only looks empty.
//
// The part names are the ones the record shapes a client already speaks use, so
// a value reads the same in a filter, in a sort, over REST and in the browser.

// Currency is an amount of one currency. The amount is in millionths of the
// currency's major unit (1.50 USD is 1_500_000), an integer so that money is
// never a float.
type Currency struct {
	AmountMicros int64  `form:"amountMicros" json:"amountMicros"`
	CurrencyCode string `form:"currencyCode" json:"currencyCode"`
}

// Emails is a primary address and any others.
type Emails struct {
	PrimaryEmail     string   `form:"primaryEmail" json:"primaryEmail"`
	AdditionalEmails []string `form:"additionalEmails" json:"additionalEmails"`
}

// Phone is one number: the digits, the ISO 3166-1 alpha-2 country it is dialled
// in and that country's calling code ("+1").
type Phone struct {
	Number      string `form:"number" json:"number"`
	CountryCode string `form:"countryCode" json:"countryCode"`
	CallingCode string `form:"callingCode" json:"callingCode"`
}

// Phones is a primary number and any others.
type Phones struct {
	PrimaryPhoneNumber      string  `form:"primaryPhoneNumber" json:"primaryPhoneNumber"`
	PrimaryPhoneCountryCode string  `form:"primaryPhoneCountryCode" json:"primaryPhoneCountryCode"`
	PrimaryPhoneCallingCode string  `form:"primaryPhoneCallingCode" json:"primaryPhoneCallingCode"`
	AdditionalPhones        []Phone `form:"additionalPhones" json:"additionalPhones"`
}

// Link is one URL and the label a person reads for it.
type Link struct {
	URL   string `form:"url" json:"url"`
	Label string `form:"label" json:"label"`
}

// Links is a primary link and any others.
type Links struct {
	PrimaryLinkUrl   string `form:"primaryLinkUrl" json:"primaryLinkUrl"`
	PrimaryLinkLabel string `form:"primaryLinkLabel" json:"primaryLinkLabel"`
	SecondaryLinks   []Link `form:"secondaryLinks" json:"secondaryLinks"`
}

// Address is a postal address, and where it is when that is known.
type Address struct {
	AddressStreet1  string   `form:"addressStreet1" json:"addressStreet1"`
	AddressStreet2  string   `form:"addressStreet2" json:"addressStreet2"`
	AddressCity     string   `form:"addressCity" json:"addressCity"`
	AddressState    string   `form:"addressState" json:"addressState"`
	AddressPostcode string   `form:"addressPostcode" json:"addressPostcode"`
	AddressCountry  string   `form:"addressCountry" json:"addressCountry"`
	AddressLat      *float64 `form:"addressLat" json:"addressLat"`
	AddressLng      *float64 `form:"addressLng" json:"addressLng"`
}

// FullName is a person's name in two parts.
type FullName struct {
	FirstName string `form:"firstName" json:"firstName"`
	LastName  string `form:"lastName" json:"lastName"`
}

// Actor is who wrote a record: where the write came from, and the person or
// program that made it.
//
// Id is the writer's auth record id, the value `@request.auth.id` answers, so a
// rule can ask `createdBy.id = @request.auth.id`. Name is what a person reads.
type Actor struct {
	Source string `form:"source" json:"source"`
	Id     string `form:"id" json:"id"`
	Name   string `form:"name" json:"name"`
}

// IsZero reports whether the value holds nothing.
func (v Currency) IsZero() bool { return v.AmountMicros == 0 && v.CurrencyCode == "" }

// IsZero reports whether the value holds nothing.
func (v Emails) IsZero() bool { return v.PrimaryEmail == "" && len(v.AdditionalEmails) == 0 }

// IsZero reports whether the value holds nothing.
func (v Phones) IsZero() bool {
	return v.PrimaryPhoneNumber == "" && v.PrimaryPhoneCountryCode == "" &&
		v.PrimaryPhoneCallingCode == "" && len(v.AdditionalPhones) == 0
}

// IsZero reports whether the value holds nothing.
func (v Links) IsZero() bool {
	return v.PrimaryLinkUrl == "" && v.PrimaryLinkLabel == "" && len(v.SecondaryLinks) == 0
}

// IsZero reports whether the value holds nothing.
func (v Address) IsZero() bool {
	return v.AddressStreet1 == "" && v.AddressStreet2 == "" && v.AddressCity == "" &&
		v.AddressState == "" && v.AddressPostcode == "" && v.AddressCountry == "" &&
		v.AddressLat == nil && v.AddressLng == nil
}

// IsZero reports whether the value holds nothing.
func (v FullName) IsZero() bool { return v.FirstName == "" && v.LastName == "" }

// IsZero reports whether the value holds nothing.
func (v Actor) IsZero() bool { return v.Source == "" && v.Id == "" && v.Name == "" }

// MarshalJSON writes null for an empty value.
func (v Currency) MarshalJSON() ([]byte, error) {
	type plain Currency
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v Emails) MarshalJSON() ([]byte, error) {
	type plain Emails
	if v.AdditionalEmails == nil {
		v.AdditionalEmails = []string{}
	}
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v Phones) MarshalJSON() ([]byte, error) {
	type plain Phones
	if v.AdditionalPhones == nil {
		v.AdditionalPhones = []Phone{}
	}
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v Links) MarshalJSON() ([]byte, error) {
	type plain Links
	if v.SecondaryLinks == nil {
		v.SecondaryLinks = []Link{}
	}
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v Address) MarshalJSON() ([]byte, error) {
	type plain Address
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v FullName) MarshalJSON() ([]byte, error) {
	type plain FullName
	return marshalComposite(v.IsZero(), plain(v))
}

// MarshalJSON writes null for an empty value.
func (v Actor) MarshalJSON() ([]byte, error) {
	type plain Actor
	return marshalComposite(v.IsZero(), plain(v))
}

func marshalComposite(zero bool, v any) ([]byte, error) {
	if zero {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Currency) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Emails) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Phones) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Links) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Address) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v FullName) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

// Value writes NULL for an empty value and the JSON document otherwise.
func (v Actor) Value() (driver.Value, error) { return compositeValue(v.IsZero(), v) }

func compositeValue(zero bool, v json.Marshaler) (driver.Value, error) {
	if zero {
		return nil, nil
	}
	data, err := v.MarshalJSON()
	return string(data), err
}

// Scan reads a stored document, a map, JSON text or bytes, or a value of the
// same type. nil and an empty document leave the value empty.
func (v *Currency) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *Emails) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *Phones) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *Links) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *Address) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *FullName) Scan(value any) error { return scanComposite(value, v) }

// Scan reads a stored document; see [Currency.Scan].
func (v *Actor) Scan(value any) error { return scanComposite(value, v) }

// scanComposite decodes value into dst, which is a pointer to one of the types
// above. The value is reset first, so a scan never keeps parts of what it held.
func scanComposite[T any](value any, dst *T) error {
	var zero T
	*dst = zero

	var raw []byte
	switch v := value.(type) {
	case nil:
		return nil
	case T:
		*dst = v
		return nil
	case *T:
		if v != nil {
			*dst = *v
		}
		return nil
	case JSONRaw:
		raw = v
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return fmt.Errorf("[%T] unable to marshal value for scanning: %w", dst, err)
		}
	}

	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil
	}

	// None of these types decodes itself, so this cannot recurse.
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("[%T] unable to scan value %q: %w", dst, raw, err)
	}

	return nil
}

// AsMap returns the parts, for a rule expression to reach into.
func (v Currency) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v Emails) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v Phones) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v Links) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v Address) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v FullName) AsMap() map[string]any { return asMap(v) }

// AsMap returns the parts, for a rule expression to reach into.
func (v Actor) AsMap() map[string]any { return asMap(v) }

// asMap round-trips v through JSON. An empty value is an empty map rather than
// nil, so a rule reaching into one finds an absent part instead of an error.
func asMap(v json.Marshaler) map[string]any {
	m := map[string]any{}
	raw, err := v.MarshalJSON()
	if err != nil || string(raw) == "null" {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

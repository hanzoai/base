package core

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeLinks] = func() Field {
		return &LinksField{}
	}
}

const FieldTypeLinks = "links"

var (
	_ Field          = (*LinksField)(nil)
	_ CompositeField = (*LinksField)(nil)
	_ TextParts      = (*LinksField)(nil)
)

// LinksField defines "links" type field for storing a primary link and any
// others, each a URL and a label, as [types.Links].
//
// A URL written without a scheme is read as https ("acme.com" is
// "https://acme.com"); only http and https URLs with a host are accepted. Blanks
// and repeated URLs are dropped, and a value with no primary link promotes its
// first other one. With Unique set, no two records share a primary URL.
type LinksField struct {
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

	// MaxValues is how many links the value may hold, the primary one
	// included. 0 means 10.
	MaxValues int `form:"maxValues" json:"maxValues"`

	// Unique refuses a primary URL another record already has.
	Unique bool `form:"unique" json:"unique"`

	// Required will require the field value to hold at least one link.
	Required bool `form:"required" json:"required"`
}

// Type implements [Field.Type] interface method.
func (f *LinksField) Type() string {
	return FieldTypeLinks
}

// Parts implements [CompositeField.Parts] interface method.
func (f *LinksField) Parts() []string {
	return []string{"primaryLinkUrl", "primaryLinkLabel", "secondaryLinks"}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *LinksField) TextParts() []string {
	return f.Parts()
}

// GetId implements [Field.GetId] interface method.
func (f *LinksField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *LinksField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *LinksField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *LinksField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *LinksField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *LinksField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *LinksField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *LinksField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *LinksField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *LinksField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Links{}
	if err := v.Scan(raw); err != nil {
		return v, err
	}

	all := make([]types.Link, 0, 1+len(v.SecondaryLinks))
	for _, l := range append([]types.Link{{URL: v.PrimaryLinkUrl, Label: v.PrimaryLinkLabel}}, v.SecondaryLinks...) {
		l.URL = normalizeURL(l.URL)
		l.Label = strings.TrimSpace(l.Label)
		if l.URL == "" || containsLink(all, l.URL) {
			continue
		}
		all = append(all, l)
	}
	if len(all) == 0 {
		return types.Links{}, nil
	}

	return types.Links{
		PrimaryLinkUrl:   all[0].URL,
		PrimaryLinkLabel: all[0].Label,
		SecondaryLinks:   all[1:],
	}, nil
}

// normalizeURL trims s and reads a URL written without a scheme as https.
func normalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return s
}

func containsLink(all []types.Link, u string) bool {
	for _, l := range all {
		if strings.EqualFold(l.URL, u) {
			return true
		}
	}
	return false
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *LinksField) ValidateValue(ctx context.Context, app App, record *Record) error {
	v, ok := record.GetRaw(f.Name).(types.Links)
	if !ok {
		return validators.ErrUnsupportedValueType
	}

	if v.IsZero() {
		if f.Required {
			return validation.ErrRequired
		}
		return nil
	}

	if 1+len(v.SecondaryLinks) > maxValuesOf(f.MaxValues) {
		return validation.NewError("validation_too_many_values", "Holds more than {{.max}} values.").
			SetParams(map[string]any{"max": maxValuesOf(f.MaxValues)})
	}

	if err := validateLink(v.PrimaryLinkUrl, v.PrimaryLinkLabel); err != nil {
		return validation.Errors{"primaryLinkUrl": err}
	}
	for i, l := range v.SecondaryLinks {
		if err := validateLink(l.URL, l.Label); err != nil {
			return validation.Errors{"secondaryLinks": validation.Errors{strconv.Itoa(i): err}}
		}
	}

	if f.Unique {
		if err := checkUniquePart(ctx, app, record, f.Name, "primaryLinkUrl", v.PrimaryLinkUrl); err != nil {
			return validation.Errors{"primaryLinkUrl": err}
		}
	}

	return nil
}

func validateLink(raw, label string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2048 {
		return validation.NewError("validation_invalid_url", "Must be an http or https URL.")
	}
	if len(label) > 255 {
		return validation.NewError("validation_label_too_long", "The label is longer than 255 characters.")
	}
	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *LinksField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validateMaxValues(&f.MaxValues),
	)
}

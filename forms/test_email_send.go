package forms

import (
	"errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/mails"
)

const (
	TestTemplateVerification = "verification"
	TestTemplateEmailChange  = "email-change"
)

// TestEmailSend is a email template test request form.
type TestEmailSend struct {
	app core.App

	Email      string `form:"email" json:"email"`
	Template   string `form:"template" json:"template"`
	Collection string `form:"collection" json:"collection"` // optional, fallbacks to _superusers
}

// NewTestEmailSend creates and initializes new TestEmailSend form.
func NewTestEmailSend(app core.App) *TestEmailSend {
	return &TestEmailSend{app: app}
}

// Validate makes the form validatable by implementing [validation.Validatable] interface.
func (form *TestEmailSend) Validate() error {
	return validation.ValidateStruct(form,
		validation.Field(
			&form.Collection,
			validation.Length(1, 255),
			validation.By(form.checkAuthCollection),
		),
		validation.Field(
			&form.Email,
			validation.Required,
			validation.Length(1, 255),
			is.EmailFormat,
		),
		validation.Field(
			&form.Template,
			validation.Required,
			validation.In(
				TestTemplateVerification,
				TestTemplateEmailChange,
			),
		),
	)
}

func (form *TestEmailSend) checkAuthCollection(value any) error {
	v, _ := value.(string)
	if v == "" {
		return nil // nothing to check
	}

	c, _ := form.app.FindCollectionByNameOrId(v)
	if c == nil || !c.IsAuth() {
		return validation.NewError("validation_invalid_auth_collection", "Must be a valid auth collection id or name.")
	}

	return nil
}

// Submit validates and sends a test email to the form.Email address.
func (form *TestEmailSend) Submit() error {
	if err := form.Validate(); err != nil {
		return err
	}

	collectionIdOrName := form.Collection
	if collectionIdOrName == "" {
		collectionIdOrName = core.CollectionNameSuperusers
	}

	collection, err := form.app.FindCollectionByNameOrId(collectionIdOrName)
	if err != nil {
		return err
	}

	record := core.NewRecord(collection)
	for _, field := range collection.Fields {
		if field.GetHidden() {
			continue
		}
		record.Set(field.GetName(), "__test_"+field.GetName()+"__")
	}
	record.RefreshTokenKey()
	record.SetEmail(form.Email)

	switch form.Template {
	case TestTemplateVerification:
		return mails.SendRecordVerification(form.app, record)
	case TestTemplateEmailChange:
		return mails.SendRecordChangeEmail(form.app, record, form.Email)
	default:
		return errors.New("unknown template " + form.Template)
	}
}

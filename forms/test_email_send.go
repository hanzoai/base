package forms

import (
	"net/mail"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/mailer"
)

// TestEmailSend proves the configured SMTP settings can deliver a message.
//
// It used to render one of the auth-record email templates against a throwaway
// record, which made "can this deployment send mail" answerable only through a
// flow this fork no longer has: verification and email-change were removed with
// the rest of local auth, so both branches described pages that do not exist and
// endpoints that answer 404. The remaining question is the one an operator
// actually has after editing SMTP settings, and it needs no template, no
// collection and no record — just a message that either arrives or does not.
type TestEmailSend struct {
	app core.App

	Email string `form:"email" json:"email"`
}

// NewTestEmailSend creates and initializes new TestEmailSend form.
func NewTestEmailSend(app core.App) *TestEmailSend {
	return &TestEmailSend{app: app}
}

// Validate makes the form validatable by implementing [validation.Validatable] interface.
func (form *TestEmailSend) Validate() error {
	return validation.ValidateStruct(form,
		validation.Field(
			&form.Email,
			validation.Required,
			validation.Length(1, 255),
			is.EmailFormat,
		),
	)
}

// Submit validates and sends a test email to the form.Email address.
func (form *TestEmailSend) Submit() error {
	if err := form.Validate(); err != nil {
		return err
	}

	settings := form.app.Settings()
	name := settings.Meta.AppName

	message := &mailer.Message{
		From:    mail.Address{Name: settings.Meta.SenderName, Address: settings.Meta.SenderAddress},
		To:      []mail.Address{{Address: form.Email}},
		Subject: name + " test email",
		HTML:    "<p>This is a test message from " + name + ". Your SMTP settings work.</p>",
	}

	return form.app.NewMailClient().Send(message)
}

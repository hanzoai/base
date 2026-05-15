package mails

import (
	"html"
	"html/template"
	"net/mail"
	"slices"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/mails/templates"
	"github.com/hanzoai/base/tools/mailer"
)

// SendRecordVerification sends a verification request email to the specified auth record.
func SendRecordVerification(app core.App, authRecord *core.Record) error {
	token, tokenErr := authRecord.NewVerificationToken()
	if tokenErr != nil {
		return tokenErr
	}

	mailClient := app.NewMailClient()

	subject, body, err := resolveEmailTemplate(app, authRecord, authRecord.Collection().VerificationTemplate, map[string]any{
		core.EmailPlaceholderToken: token,
	})
	if err != nil {
		return err
	}

	message := &mailer.Message{
		From: mail.Address{
			Name:    app.Settings().Meta.SenderName,
			Address: app.Settings().Meta.SenderAddress,
		},
		To:      []mail.Address{{Address: authRecord.Email()}},
		Subject: subject,
		HTML:    body,
	}

	event := new(core.MailerRecordEvent)
	event.App = app
	event.Mailer = mailClient
	event.Message = message
	event.Record = authRecord
	event.Meta = map[string]any{"token": token}

	return app.OnMailerRecordVerificationSend().Trigger(event, func(e *core.MailerRecordEvent) error {
		return e.Mailer.Send(e.Message)
	})
}

// SendRecordChangeEmail sends a change email confirmation email to the specified auth record.
func SendRecordChangeEmail(app core.App, authRecord *core.Record, newEmail string) error {
	token, tokenErr := authRecord.NewEmailChangeToken(newEmail)
	if tokenErr != nil {
		return tokenErr
	}

	mailClient := app.NewMailClient()

	subject, body, err := resolveEmailTemplate(app, authRecord, authRecord.Collection().ConfirmEmailChangeTemplate, map[string]any{
		core.EmailPlaceholderToken: token,
	})
	if err != nil {
		return err
	}

	message := &mailer.Message{
		From: mail.Address{
			Name:    app.Settings().Meta.SenderName,
			Address: app.Settings().Meta.SenderAddress,
		},
		To:      []mail.Address{{Address: newEmail}},
		Subject: subject,
		HTML:    body,
	}

	event := new(core.MailerRecordEvent)
	event.App = app
	event.Mailer = mailClient
	event.Message = message
	event.Record = authRecord
	event.Meta = map[string]any{
		"token":    token,
		"newEmail": newEmail,
	}

	return app.OnMailerRecordEmailChangeSend().Trigger(event, func(e *core.MailerRecordEvent) error {
		return e.Mailer.Send(e.Message)
	})
}

var nonescapeTypes = []string{
	core.FieldTypeAutodate,
	core.FieldTypeDate,
	core.FieldTypeBool,
	core.FieldTypeNumber,
}

func resolveEmailTemplate(
	app core.App,
	authRecord *core.Record,
	emailTemplate core.EmailTemplate,
	placeholders map[string]any,
) (subject string, body string, err error) {
	if placeholders == nil {
		placeholders = map[string]any{}
	}

	// register default system placeholders
	if _, ok := placeholders[core.EmailPlaceholderAppName]; !ok {
		placeholders[core.EmailPlaceholderAppName] = app.Settings().Meta.AppName
	}
	if _, ok := placeholders[core.EmailPlaceholderAppURL]; !ok {
		placeholders[core.EmailPlaceholderAppURL] = app.Settings().Meta.AppURL
	}

	// register default auth record placeholders
	for _, field := range authRecord.Collection().Fields {
		if field.GetHidden() {
			continue
		}

		fieldPlacehodler := "{RECORD:" + field.GetName() + "}"
		if _, ok := placeholders[fieldPlacehodler]; !ok {
			val := authRecord.GetString(field.GetName())

			// note: the escaping is not strictly necessary but for just in case
			// the user decide to store and render the email as plain html
			if !slices.Contains(nonescapeTypes, field.Type()) {
				val = html.EscapeString(val)
			}

			placeholders[fieldPlacehodler] = val
		}
	}

	subject, rawBody := emailTemplate.Resolve(placeholders)

	params := struct {
		HTMLContent template.HTML
	}{
		HTMLContent: template.HTML(rawBody),
	}

	body, err = resolveTemplateContent(params, templates.Layout, templates.HTMLBody)
	if err != nil {
		return "", "", err
	}

	return subject, body, nil
}

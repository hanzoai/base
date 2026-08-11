package forms_test

import (
	"fmt"
	"strings"
	"testing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/forms"
	"github.com/hanzoai/base/tests"
)

func TestEmailSendValidateAndSubmit(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		email          string
		expectedErrors []string
	}{
		{"", []string{"email"}},
		{"invalid", []string{"email"}},
		{strings.Repeat("a", 250) + "@example.com", []string{"email"}},
		{"test@example.com", nil},
	}

	for i, s := range scenarios {
		t.Run(fmt.Sprintf("%d_%s", i, s.email), func(t *testing.T) {
			app, _ := tests.NewTestApp()
			defer app.Cleanup()

			form := forms.NewTestEmailSend(app)
			form.Email = s.email

			result := form.Submit()

			// parse errors
			errs, ok := result.(validation.Errors)
			if !ok && result != nil {
				t.Fatalf("Failed to parse errors %v", result)
			}

			// check errors
			if len(errs) > len(s.expectedErrors) {
				t.Fatalf("Expected error keys %v, got %v", s.expectedErrors, errs)
			}
			for _, k := range s.expectedErrors {
				if _, ok := errs[k]; !ok {
					t.Fatalf("Missing expected error key %q in %v", k, errs)
				}
			}

			expectedEmails := 1
			if len(s.expectedErrors) > 0 {
				expectedEmails = 0
			}

			if app.TestMailer.TotalSend() != expectedEmails {
				t.Fatalf("Expected %d email(s) to be sent, got %d", expectedEmails, app.TestMailer.TotalSend())
			}

			if len(s.expectedErrors) > 0 {
				return
			}

			// The point of the send is that SMTP works, so the message says so
			// and carries no token, no collection and no link — there is no flow
			// behind it to complete.
			msg := app.TestMailer.LastMessage()
			if !strings.Contains(msg.HTML, "SMTP settings work") {
				t.Errorf("Expected the email to state what it proves, got\n%v", msg.HTML)
			}
			if len(msg.To) != 1 || msg.To[0].Address != s.email {
				t.Errorf("Expected the email addressed to %q, got %v", s.email, msg.To)
			}
		})
	}
}

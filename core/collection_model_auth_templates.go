package core

// Common email-template placeholder tokens used by the auth-record
// mailer entry points that survived the IAM-native rip. Placeholders
// for retired flows (OTP, password reset, login alerts) have been
// removed.
const (
	EmailPlaceholderAppName string = "{APP_NAME}"
	EmailPlaceholderAppURL  string = "{APP_URL}"
	EmailPlaceholderToken   string = "{TOKEN}"
)

var defaultVerificationTemplate = EmailTemplate{
	Subject: "Verify your " + EmailPlaceholderAppName + " email",
	Body: `<p>Hello,</p>
<p>Thank you for joining us at ` + EmailPlaceholderAppName + `.</p>
<p>Click on the button below to verify your email address.</p>
<p>
  <a class="btn" href="` + EmailPlaceholderAppURL + "/_/#/auth/confirm-verification/" + EmailPlaceholderToken + `" target="_blank" rel="noopener">Verify</a>
</p>
<p>
  Thanks,<br/>
  ` + EmailPlaceholderAppName + ` team
</p>`,
}

var defaultConfirmEmailChangeTemplate = EmailTemplate{
	Subject: "Confirm your " + EmailPlaceholderAppName + " new email address",
	Body: `<p>Hello,</p>
<p>Click on the button below to confirm your new email address.</p>
<p>
  <a class="btn" href="` + EmailPlaceholderAppURL + "/_/#/auth/confirm-email-change/" + EmailPlaceholderToken + `" target="_blank" rel="noopener">Confirm new email</a>
</p>
<p><i>If you didn't ask to change your email address, you can ignore this email.</i></p>
<p>
  Thanks,<br/>
  ` + EmailPlaceholderAppName + ` team
</p>`,
}

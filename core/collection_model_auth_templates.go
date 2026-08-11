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

// These are DEFAULTS, applied to a collection when it is created
// (setDefaultAuthOptions). A collection stores its own copy, so an app that
// wants a different mail writes one and this file never touches it again.
//
// Neither default links to a confirm page, and that is the point. They used to
// carry `{APP_URL}/_/#/auth/confirm-verification/{TOKEN}`, an address that was
// wrong three ways over: /_/ was the old admin mount, #/auth/... was a route in
// an admin SPA that no longer exists, and the endpoint behind it was deleted in
// the IAM-native rip — bindRecordAuthApi keeps only auth-methods and
// auth-refresh, and every request/confirm flow answers 404. So the button in
// the one mail a person is most likely to click went nowhere, and had for as
// long as the rip.
//
// {TOKEN} is still substituted, because the mechanism is real even where this
// fork serves no page for it: an app can bind its own confirm route from a hook
// and put the token back in its own template. What a default may not do is name
// a route on the reader's behalf and be wrong about it.
var defaultVerificationTemplate = EmailTemplate{
	Subject: "Verify your " + EmailPlaceholderAppName + " email",
	Body: `<p>Hello,</p>
<p>Thank you for joining us at ` + EmailPlaceholderAppName + `.</p>
<p>Your email address is confirmed through your ` + EmailPlaceholderAppName + ` account.</p>
<p>
  <a class="btn" href="` + EmailPlaceholderAppURL + `" target="_blank" rel="noopener">Open ` + EmailPlaceholderAppName + `</a>
</p>
<p>
  Thanks,<br/>
  ` + EmailPlaceholderAppName + ` team
</p>`,
}

var defaultConfirmEmailChangeTemplate = EmailTemplate{
	Subject: "Confirm your " + EmailPlaceholderAppName + " new email address",
	Body: `<p>Hello,</p>
<p>A new email address was requested for your ` + EmailPlaceholderAppName + ` account.</p>
<p>Open your account to confirm it.</p>
<p>
  <a class="btn" href="` + EmailPlaceholderAppURL + `" target="_blank" rel="noopener">Open ` + EmailPlaceholderAppName + `</a>
</p>
<p><i>If you didn't ask to change your email address, you can ignore this email.</i></p>
<p>
  Thanks,<br/>
  ` + EmailPlaceholderAppName + ` team
</p>`,
}

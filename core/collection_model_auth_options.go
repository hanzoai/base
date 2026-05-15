package core

import (
	"strconv"
	"strings"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/hanzoai/base/tools/auth"
	"github.com/hanzoai/base/tools/security"
	"github.com/hanzoai/base/tools/types"
	"github.com/spf13/cast"
)

func (m *Collection) unsetMissingOAuth2MappedFields() {
	if !m.IsAuth() {
		return
	}

	if m.OAuth2.MappedFields.Id != "" {
		if m.Fields.GetByName(m.OAuth2.MappedFields.Id) == nil {
			m.OAuth2.MappedFields.Id = ""
		}
	}

	if m.OAuth2.MappedFields.Name != "" {
		if m.Fields.GetByName(m.OAuth2.MappedFields.Name) == nil {
			m.OAuth2.MappedFields.Name = ""
		}
	}

	if m.OAuth2.MappedFields.Username != "" {
		if m.Fields.GetByName(m.OAuth2.MappedFields.Username) == nil {
			m.OAuth2.MappedFields.Username = ""
		}
	}

	if m.OAuth2.MappedFields.AvatarURL != "" {
		if m.Fields.GetByName(m.OAuth2.MappedFields.AvatarURL) == nil {
			m.OAuth2.MappedFields.AvatarURL = ""
		}
	}
}

func (m *Collection) setDefaultAuthOptions() {
	m.collectionAuthOptions = collectionAuthOptions{
		VerificationTemplate:       defaultVerificationTemplate,
		ConfirmEmailChangeTemplate: defaultConfirmEmailChangeTemplate,
		AuthRule:                   types.Pointer(""),
		AuthToken: TokenConfig{
			Secret:   security.RandomString(50),
			Duration: 604800, // 7 days
		},
		EmailChangeToken: TokenConfig{
			Secret:   security.RandomString(50),
			Duration: 1800, // 30min
		},
		VerificationToken: TokenConfig{
			Secret:   security.RandomString(50),
			Duration: 259200, // 3days
		},
		FileToken: TokenConfig{
			Secret:   security.RandomString(50),
			Duration: 180, // 3min
		},
	}
}

var _ optionsValidator = (*collectionAuthOptions)(nil)

// collectionAuthOptions defines the options for the "auth" type collection.
//
// Auth-type collections under IAM-native Base are passive mirrors of
// the Hanzo IAM user directory. They have no local password, no MFA
// challenges, no OTP issuance, and no external-auth provider link
// tracking — those flows live in IAM. The only collection-scoped
// state that survives here is the OAuth2 provider discovery list
// (used by the auth-methods endpoint), the verification/email-change
// templates, and the token secrets that sign JWTs minted by Base.
type collectionAuthOptions struct {
	// AuthRule could be used to specify additional record constraints
	// applied after record authentication and right before returning the
	// auth token response to the client.
	//
	// For example, to allow only verified users you could set it to
	// "verified = true".
	//
	// Set it to empty string to allow any Auth collection record to authenticate.
	//
	// Set it to nil to disallow authentication altogether for the collection
	// (that includes password, OAuth2, etc.).
	AuthRule *string `form:"authRule" json:"authRule"`

	// ManageRule gives admin-like permissions to allow fully managing
	// the auth record(s), eg. directly updating the verified state and
	// email without requiring an end-user action.
	//
	// This rule is executed in addition to the Create and Update API rules.
	ManageRule *string `form:"manageRule" json:"manageRule"`

	// OAuth2 specifies whether OAuth2 auth is enabled for the collection
	// and which OAuth2 providers are allowed.
	OAuth2 OAuth2Config `form:"oauth2" json:"oauth2"`

	// Various token configurations
	// ---
	AuthToken         TokenConfig `form:"authToken" json:"authToken"`
	EmailChangeToken  TokenConfig `form:"emailChangeToken" json:"emailChangeToken"`
	VerificationToken TokenConfig `form:"verificationToken" json:"verificationToken"`
	FileToken         TokenConfig `form:"fileToken" json:"fileToken"`

	// Default email templates
	// ---
	VerificationTemplate       EmailTemplate `form:"verificationTemplate" json:"verificationTemplate"`
	ConfirmEmailChangeTemplate EmailTemplate `form:"confirmEmailChangeTemplate" json:"confirmEmailChangeTemplate"`
}

func (o *collectionAuthOptions) validate(cv *collectionValidator) error {
	err := validation.ValidateStruct(o,
		validation.Field(
			&o.AuthRule,
			validation.By(cv.checkRule),
			validation.By(cv.ensureNoSystemRuleChange(cv.original.AuthRule)),
		),
		validation.Field(
			&o.ManageRule,
			validation.NilOrNotEmpty,
			validation.By(cv.checkRule),
			validation.By(cv.ensureNoSystemRuleChange(cv.original.ManageRule)),
		),
		validation.Field(&o.OAuth2),
		validation.Field(&o.AuthToken),
		validation.Field(&o.EmailChangeToken),
		validation.Field(&o.VerificationToken),
		validation.Field(&o.FileToken),
		validation.Field(&o.VerificationTemplate, validation.Required),
		validation.Field(&o.ConfirmEmailChangeTemplate, validation.Required),
	)
	if err != nil {
		return err
	}

	return nil
}

// -------------------------------------------------------------------

type EmailTemplate struct {
	Subject string `form:"subject" json:"subject"`
	Body    string `form:"body" json:"body"`
}

// Validate makes EmailTemplate validatable by implementing [validation.Validatable] interface.
func (t EmailTemplate) Validate() error {
	return validation.ValidateStruct(&t,
		validation.Field(&t.Subject, validation.Required),
		validation.Field(&t.Body, validation.Required),
	)
}

// Resolve replaces the placeholder parameters in the current email
// template and returns its components as ready-to-use strings.
func (t EmailTemplate) Resolve(placeholders map[string]any) (subject, body string) {
	body = t.Body
	subject = t.Subject

	for k, v := range placeholders {
		vStr := cast.ToString(v)

		// replace subject placeholder params (if any)
		subject = strings.ReplaceAll(subject, k, vStr)

		// replace body placeholder params (if any)
		body = strings.ReplaceAll(body, k, vStr)
	}

	return subject, body
}

// -------------------------------------------------------------------

type TokenConfig struct {
	Secret string `form:"secret" json:"secret,omitempty"`

	// Duration specifies how long an issued token to be valid (in seconds)
	Duration int64 `form:"duration" json:"duration"`
}

// Validate makes TokenConfig validatable by implementing [validation.Validatable] interface.
func (c TokenConfig) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(&c.Secret, validation.Required, validation.Length(30, 255)),
		validation.Field(&c.Duration, validation.Required, validation.Min(10), validation.Max(94670856)), // ~3y max
	)
}

// DurationTime returns the current Duration as [time.Duration].
func (c TokenConfig) DurationTime() time.Duration {
	return time.Duration(c.Duration) * time.Second
}

// -------------------------------------------------------------------

type OAuth2KnownFields struct {
	Id        string `form:"id" json:"id"`
	Name      string `form:"name" json:"name"`
	Username  string `form:"username" json:"username"`
	AvatarURL string `form:"avatarURL" json:"avatarURL"`
}

type OAuth2Config struct {
	Providers []OAuth2ProviderConfig `form:"providers" json:"providers"`

	MappedFields OAuth2KnownFields `form:"mappedFields" json:"mappedFields"`

	Enabled bool `form:"enabled" json:"enabled"`
}

// GetProviderConfig returns the first OAuth2ProviderConfig that matches the specified name.
//
// Returns false and zero config if no such provider is available in c.Providers.
func (c OAuth2Config) GetProviderConfig(name string) (config OAuth2ProviderConfig, exists bool) {
	for _, p := range c.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return
}

// Validate makes OAuth2Config validatable by implementing [validation.Validatable] interface.
func (c OAuth2Config) Validate() error {
	if !c.Enabled {
		return nil // no need to validate
	}

	return validation.ValidateStruct(&c,
		// note: don't require providers for now as they could be externally registered/removed
		validation.Field(&c.Providers, validation.By(checkForDuplicatedProviders)),
	)
}

func checkForDuplicatedProviders(value any) error {
	configs, _ := value.([]OAuth2ProviderConfig)

	existing := map[string]struct{}{}

	for i, c := range configs {
		if c.Name == "" {
			continue // the name nonempty state is validated separately
		}
		if _, ok := existing[c.Name]; ok {
			return validation.Errors{
				strconv.Itoa(i): validation.Errors{
					"name": validation.NewError("validation_duplicated_provider", "The provider {{.name}} is already registered.").
						SetParams(map[string]any{"name": c.Name}),
				},
			}
		}
		existing[c.Name] = struct{}{}
	}

	return nil
}

type OAuth2ProviderConfig struct {
	// PKCE overwrites the default provider PKCE config option.
	//
	// This usually shouldn't be needed but some OAuth2 vendors, like the LinkedIn OIDC,
	// may require manual adjustment due to returning error if extra parameters are added to the request
	// (https://github.com/hanzoai/base/discussions/3799#discussioncomment-7640312)
	PKCE *bool `form:"pkce" json:"pkce"`

	Name         string         `form:"name" json:"name"`
	ClientId     string         `form:"clientId" json:"clientId"`
	ClientSecret string         `form:"clientSecret" json:"clientSecret,omitempty"`
	AuthURL      string         `form:"authURL" json:"authURL"`
	TokenURL     string         `form:"tokenURL" json:"tokenURL"`
	UserInfoURL  string         `form:"userInfoURL" json:"userInfoURL"`
	DisplayName  string         `form:"displayName" json:"displayName"`
	Extra        map[string]any `form:"extra" json:"extra"`
}

// Validate makes OAuth2ProviderConfig validatable by implementing [validation.Validatable] interface.
func (c OAuth2ProviderConfig) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(&c.Name, validation.Required, validation.By(checkProviderName)),
		validation.Field(&c.ClientId, validation.Required),
		validation.Field(&c.ClientSecret, validation.Required),
		validation.Field(&c.AuthURL, is.URL),
		validation.Field(&c.TokenURL, is.URL),
		validation.Field(&c.UserInfoURL, is.URL),
	)
}

func checkProviderName(value any) error {
	name, _ := value.(string)
	if name == "" {
		return nil // nothing to check
	}

	if _, err := auth.NewProviderByName(name); err != nil {
		return validation.NewError("validation_missing_provider", "Invalid or missing provider with name {{.name}}.").
			SetParams(map[string]any{"name": name})
	}

	return nil
}

// InitProvider returns a new auth.Provider instance loaded with the current OAuth2ProviderConfig options.
func (c OAuth2ProviderConfig) InitProvider() (auth.Provider, error) {
	provider, err := auth.NewProviderByName(c.Name)
	if err != nil {
		return nil, err
	}

	if c.ClientId != "" {
		provider.SetClientId(c.ClientId)
	}

	if c.ClientSecret != "" {
		provider.SetClientSecret(c.ClientSecret)
	}

	if c.AuthURL != "" {
		provider.SetAuthURL(c.AuthURL)
	}

	if c.UserInfoURL != "" {
		provider.SetUserInfoURL(c.UserInfoURL)
	}

	if c.TokenURL != "" {
		provider.SetTokenURL(c.TokenURL)
	}

	if c.DisplayName != "" {
		provider.SetDisplayName(c.DisplayName)
	}

	if c.PKCE != nil {
		provider.SetPKCE(*c.PKCE)
	}

	if c.Extra != nil {
		provider.SetExtra(c.Extra)
	}

	return provider, nil
}

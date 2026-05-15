package core

// Email returns the "email" record field value (usually available with Auth collections).
func (m *Record) Email() string {
	return m.GetString(FieldNameEmail)
}

// SetEmail sets the "email" record field value (usually available with Auth collections).
func (m *Record) SetEmail(email string) {
	m.Set(FieldNameEmail, email)
}

// Verified returns the "emailVisibility" record field value (usually available with Auth collections).
func (m *Record) EmailVisibility() bool {
	return m.GetBool(FieldNameEmailVisibility)
}

// SetEmailVisibility sets the "emailVisibility" record field value (usually available with Auth collections).
func (m *Record) SetEmailVisibility(visible bool) {
	m.Set(FieldNameEmailVisibility, visible)
}

// Verified returns the "verified" record field value (usually available with Auth collections).
func (m *Record) Verified() bool {
	return m.GetBool(FieldNameVerified)
}

// SetVerified sets the "verified" record field value (usually available with Auth collections).
func (m *Record) SetVerified(verified bool) {
	m.Set(FieldNameVerified, verified)
}

// TokenKey returns the "tokenKey" record field value (usually available with Auth collections).
func (m *Record) TokenKey() string {
	return m.GetString(FieldNameTokenKey)
}

// SetTokenKey sets the "tokenKey" record field value (usually available with Auth collections).
func (m *Record) SetTokenKey(key string) {
	m.Set(FieldNameTokenKey, key)
}

// RefreshTokenKey generates and sets a new random auth record "tokenKey".
func (m *Record) RefreshTokenKey() {
	m.Set(FieldNameTokenKey+autogenerateModifier, "")
}

// Package tests provides test token helper functions.
package tests

import (
	"time"

	"github.com/hanzoai/base/core"
)

// GetUserAuthToken returns a valid auth token for a test user in the given collection.
// This should be used in tests instead of hardcoded tokens.
func GetUserAuthToken(app core.App, collectionNameOrId, recordId string) (string, error) {
	record, err := app.FindRecordById(collectionNameOrId, recordId)
	if err != nil {
		return "", err
	}
	return record.NewAuthToken()
}

// GetUserStaticAuthToken returns a long-lived static auth token for testing.
func GetUserStaticAuthToken(app core.App, collectionNameOrId, recordId string) (string, error) {
	record, err := app.FindRecordById(collectionNameOrId, recordId)
	if err != nil {
		return "", err
	}
	return record.NewStaticAuthToken(86400 * 365 * 100 * time.Second) // 100 years
}

// GetUserVerificationToken returns a verification token for testing.
func GetUserVerificationToken(app core.App, collectionNameOrId, recordId string) (string, error) {
	record, err := app.FindRecordById(collectionNameOrId, recordId)
	if err != nil {
		return "", err
	}
	return record.NewVerificationToken()
}

// GetUserPasswordResetToken returns a password reset token for testing.
func GetUserPasswordResetToken(app core.App, collectionNameOrId, recordId string) (string, error) {
	record, err := app.FindRecordById(collectionNameOrId, recordId)
	if err != nil {
		return "", err
	}
	return record.NewPasswordResetToken()
}

// GetSuperuserAuthToken returns a valid auth token for a test superuser.
func GetSuperuserAuthToken(app core.App, recordId string) (string, error) {
	return GetUserAuthToken(app, core.CollectionNameSuperusers, recordId)
}

// Common test record IDs (matching tests/data/data.db)
const (
	// Users collection (_hz_users_auth_)
	TestUserID1 = "4q1xlclmfloku33" // test@example.com
	TestUserID2 = "oap640cot4yru2s" // test2@example.com
	TestUserID3 = "bgs820n361vj1qd" // test3@example.com

	// Clients collection (v851q4r790rhknl)
	TestClientID1 = "gk390qegs4y47wn" // test@example.com
	TestClientID2 = "o1y0dd0spd786md" // test2@example.com

	// Nologin collection (kpv709sk2lqbqk8)
	TestNologinID1 = "dc49k6jgejn40h3" // test@example.com
	TestNologinID2 = "phhq3wr65cap535" // test2@example.com
	TestNologinID3 = "oos036e9xvqeexy" // test3@example.com

	// Superusers collection (hbc_3142635823)
	TestSuperuserID1 = "sywbhecnh46rhm0" // test@example.com
	TestSuperuserID2 = "sbmbsdb40jyxf7h" // test2@example.com
	TestSuperuserID3 = "9q2trqumvlyr3bd" // test3@example.com
	TestSuperuserID4 = "q911776rrfy658l" // test4@example.com
)

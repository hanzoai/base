// Command fixtest2 generates email change tokens with correct newEmail values and other specialized tokens.
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
)

func main() {
	// Find the test data directory
	_, currentFile, _, _ := runtime.Caller(0)
	testDataDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "tests", "data")

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       testDataDir,
		EncryptionEnv: "hz_test_env",
	})

	if err := app.Bootstrap(); err != nil {
		log.Fatalf("Failed to bootstrap: %v", err)
	}

	// Generate email change tokens with newEmail=change@example.com
	fmt.Println("=== Email Change Tokens (newEmail=change@example.com) ===")

	// User 4q1xlclmfloku33 (test@example.com)
	record, _ := app.FindRecordById("users", "4q1xlclmfloku33")
	col, _ := app.FindCollectionByNameOrId("users")

	tokenKey := record.TokenKey()
	signingKey := tokenKey + col.EmailChangeToken.Secret

	// Valid token (far future expiry)
	validEmailChangeToken := createToken(
		"4q1xlclmfloku33",
		"_hz_users_auth_",
		"emailChange",
		"test@example.com",
		"change@example.com",
		signingKey,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("Valid emailChange (4q1xlclmfloku33): %s\n\n", validEmailChangeToken)

	// Expired token
	expiredEmailChangeToken := createToken(
		"4q1xlclmfloku33",
		"_hz_users_auth_",
		"emailChange",
		"test@example.com",
		"change@example.com",
		signingKey,
		-1*time.Hour, // expired
	)
	fmt.Printf("Expired emailChange (4q1xlclmfloku33): %s\n\n", expiredEmailChangeToken)

	// Generate password reset tokens for specific test cases
	fmt.Println("=== Password Reset Tokens ===")

	// User test@example.com - password reset token
	resetKey := tokenKey + col.PasswordResetToken.Secret
	validPasswordResetToken := createToken(
		"4q1xlclmfloku33",
		"_hz_users_auth_",
		"passwordReset",
		"test@example.com",
		"",
		resetKey,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("Valid passwordReset (4q1xlclmfloku33): %s\n\n", validPasswordResetToken)

	// User test2@example.com (oap640cot4yru2s)
	record2, _ := app.FindRecordById("users", "oap640cot4yru2s")
	tokenKey2 := record2.TokenKey()
	resetKey2 := tokenKey2 + col.PasswordResetToken.Secret

	validPasswordResetToken2 := createToken(
		"oap640cot4yru2s",
		"_hz_users_auth_",
		"passwordReset",
		"test2@example.com",
		"",
		resetKey2,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("Valid passwordReset (oap640cot4yru2s): %s\n\n", validPasswordResetToken2)

	// User test3@example.com (bgs820n361vj1qd)
	record3, _ := app.FindRecordById("users", "bgs820n361vj1qd")
	tokenKey3 := record3.TokenKey()
	resetKey3 := tokenKey3 + col.PasswordResetToken.Secret

	validPasswordResetToken3 := createToken(
		"bgs820n361vj1qd",
		"_hz_users_auth_",
		"passwordReset",
		"test3@example.com",
		"",
		resetKey3,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("Valid passwordReset (bgs820n361vj1qd): %s\n\n", validPasswordResetToken3)

	// Generate file tokens
	fmt.Println("=== File Tokens ===")

	// Superuser file token
	suRecord, _ := app.FindRecordById("_superusers", "sywbhecnh46rhm0")
	suCol, _ := app.FindCollectionByNameOrId("_superusers")
	suTokenKey := suRecord.TokenKey()
	suFileKey := suTokenKey + suCol.FileToken.Secret

	suFileToken := createFileToken(
		"sywbhecnh46rhm0",
		"hbc_3142635823",
		suFileKey,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("Superuser file token (sywbhecnh46rhm0): %s\n\n", suFileToken)

	// User file token
	userFileKey := tokenKey + col.FileToken.Secret
	userFileToken := createFileToken(
		"4q1xlclmfloku33",
		"_hz_users_auth_",
		userFileKey,
		86400*365*100*time.Second, // 100 years
	)
	fmt.Printf("User file token (4q1xlclmfloku33): %s\n\n", userFileToken)

	// Expired auth token
	fmt.Println("=== Expired Auth Tokens ===")
	authKey := tokenKey + col.AuthToken.Secret
	expiredAuthToken := createAuthToken(
		"4q1xlclmfloku33",
		"_hz_users_auth_",
		authKey,
		-1*time.Hour, // expired
	)
	fmt.Printf("Expired auth token (4q1xlclmfloku33): %s\n\n", expiredAuthToken)

	// Superuser expired auth token
	suAuthKey := suTokenKey + suCol.AuthToken.Secret
	suExpiredAuthToken := createAuthToken(
		"sywbhecnh46rhm0",
		"hbc_3142635823",
		suAuthKey,
		-1*time.Hour, // expired
	)
	fmt.Printf("Superuser expired auth token (sywbhecnh46rhm0): %s\n\n", suExpiredAuthToken)
}

func createToken(recordId, collectionId, tokenType, email, newEmail, signingKey string, duration time.Duration) string {
	claims := jwt.MapClaims{
		"id":           recordId,
		"collectionId": collectionId,
		"type":         tokenType,
		"email":        email,
		"exp":          time.Now().Add(duration).Unix(),
	}
	if newEmail != "" {
		claims["newEmail"] = newEmail
	}

	token, _ := security.NewJWT(claims, signingKey, duration)
	return token
}

func createFileToken(recordId, collectionId, signingKey string, duration time.Duration) string {
	claims := jwt.MapClaims{
		"id":           recordId,
		"collectionId": collectionId,
		"type":         "file",
		"exp":          time.Now().Add(duration).Unix(),
	}

	token, _ := security.NewJWT(claims, signingKey, duration)
	return token
}

func createAuthToken(recordId, collectionId, signingKey string, duration time.Duration) string {
	claims := jwt.MapClaims{
		"id":           recordId,
		"collectionId": collectionId,
		"type":         "auth",
		"refreshable":  true,
		"exp":          time.Now().Add(duration).Unix(),
	}

	token, _ := security.NewJWT(claims, signingKey, duration)
	return token
}

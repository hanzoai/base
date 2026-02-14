// Command tokengen2 generates all types of JWT tokens for testing purposes.
// Usage: go run cmd/tokengen2/main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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

	// Generate all token types for all auth collection records
	collections, err := app.FindAllCollections()
	if err != nil {
		log.Fatalf("Failed to get collections: %v", err)
	}

	allTokens := make(map[string]map[string]string) // recordId -> tokenType -> token

	for _, col := range collections {
		if !col.IsAuth() {
			continue
		}

		fmt.Printf("\n=== Collection: %s (ID: %s) ===\n", col.Name, col.Id)

		records, err := app.FindAllRecords(col.Name)
		if err != nil {
			fmt.Printf("  Error getting records: %v\n", err)
			continue
		}

		for _, record := range records {
			email := record.Email()
			tokenKey := record.TokenKey()

			fmt.Printf("\nRecord: %s (email: %s)\n", record.Id, email)
			fmt.Printf("  TokenKey: %s\n", tokenKey)

			tokens := make(map[string]string)
			allTokens[record.Id] = tokens

			// 1. Auth token (refreshable, short duration)
			if t, err := record.NewAuthToken(); err == nil {
				tokens["auth"] = t
				fmt.Printf("  Auth Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  Auth Token Error: %v\n", err)
			}

			// 2. Static auth token (non-refreshable, long duration)
			if t, err := record.NewStaticAuthToken(86400 * 365 * 100 * time.Second); err == nil {
				tokens["static"] = t
				fmt.Printf("  Static Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  Static Token Error: %v\n", err)
			}

			// 3. Verification token
			if t, err := record.NewVerificationToken(); err == nil {
				tokens["verification"] = t
				fmt.Printf("  Verification Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  Verification Token Error: %v\n", err)
			}

			// 4. Password reset token
			if t, err := record.NewPasswordResetToken(); err == nil {
				tokens["passwordReset"] = t
				fmt.Printf("  Password Reset Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  Password Reset Token Error: %v\n", err)
			}

			// 5. Email change token (with newEmail claim)
			newEmail := "newemail@example.com"
			if t, err := record.NewEmailChangeToken(newEmail); err == nil {
				tokens["emailChange"] = t
				fmt.Printf("  Email Change Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  Email Change Token Error: %v\n", err)
			}

			// 6. File token
			if t, err := record.NewFileToken(); err == nil {
				tokens["file"] = t
				fmt.Printf("  File Token: %s\n", t[:50]+"...")
			} else {
				fmt.Printf("  File Token Error: %v\n", err)
			}

			// Also create expired versions (for testing expired token scenarios)
			expiredDuration := -1 * time.Hour // Already expired

			// Expired verification token
			expiredVerifyToken := createExpiredToken(
				record.Id,
				col.Id,
				"verification",
				email,
				"",
				tokenKey + col.VerificationToken.Secret,
				expiredDuration,
			)
			tokens["expiredVerification"] = expiredVerifyToken
			fmt.Printf("  Expired Verification: %s\n", expiredVerifyToken[:50]+"...")

			// Expired password reset token
			expiredResetToken := createExpiredToken(
				record.Id,
				col.Id,
				"passwordReset",
				email,
				"",
				tokenKey + col.PasswordResetToken.Secret,
				expiredDuration,
			)
			tokens["expiredPasswordReset"] = expiredResetToken
			fmt.Printf("  Expired Password Reset: %s\n", expiredResetToken[:50]+"...")

			// Expired email change token
			expiredEmailChangeToken := createExpiredToken(
				record.Id,
				col.Id,
				"emailChange",
				email,
				newEmail,
				tokenKey + col.EmailChangeToken.Secret,
				expiredDuration,
			)
			tokens["expiredEmailChange"] = expiredEmailChangeToken
			fmt.Printf("  Expired Email Change: %s\n", expiredEmailChangeToken[:50]+"...")
		}
	}

	// Write JSON output
	jsonData, _ := json.MarshalIndent(allTokens, "", "  ")
	os.WriteFile("all_tokens.json", jsonData, 0644)
	fmt.Println("\nWritten all_tokens.json")

	// Now print sed commands for common replacements
	fmt.Println("\n=== REPLACEMENT GUIDE ===")
	fmt.Println("Use the tokens from all_tokens.json to replace hardcoded tokens in test files.")
	fmt.Println("Key record IDs:")
	fmt.Println("  Users: 4q1xlclmfloku33 (test@example.com), oap640cot4yru2s (test2@example.com)")
	fmt.Println("  Clients: gk390qegs4y47wn (test@example.com)")
	fmt.Println("  Nologin: dc49k6jgejn40h3 (test@example.com)")
	fmt.Println("  Superusers: sywbhecnh46rhm0 (test@example.com)")
}

func createExpiredToken(recordId, collectionId, tokenType, email, newEmail, signingKey string, duration time.Duration) string {
	claims := jwt.MapClaims{
		"id":           recordId,
		"type":         tokenType,
		"collectionId": collectionId,
		"exp":          time.Now().Add(duration).Unix(),
		"email":        email,
	}
	if newEmail != "" {
		claims["newEmail"] = newEmail
	}

	token, _ := security.NewJWT(claims, signingKey, duration)
	return token
}

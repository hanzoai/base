// Command tokengen generates JWT tokens for testing purposes.
// Usage: go run cmd/tokengen/main.go
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

	// Get all auth collections
	collections, err := app.FindAllCollections()
	if err != nil {
		log.Fatalf("Failed to get collections: %v", err)
	}

	fmt.Println("=== Token Generation Report ===")
	fmt.Println()

	for _, col := range collections {
		if !col.IsAuth() {
			continue
		}

		fmt.Printf("Collection: %s (ID: %s)\n", col.Name, col.Id)
		fmt.Printf("  AuthToken Secret: %s\n", col.AuthToken.Secret[:20]+"...")

		// Get all records in this collection
		records, err := app.FindAllRecords(col.Name)
		if err != nil {
			fmt.Printf("  Error getting records: %v\n", err)
			continue
		}

		for _, record := range records {
			fmt.Printf("\n  Record: %s (email: %s)\n", record.Id, record.Email())
			fmt.Printf("    TokenKey: %s\n", record.TokenKey())

			// Generate auth token (refreshable)
			authToken, err := record.NewAuthToken()
			if err != nil {
				fmt.Printf("    Auth Token Error: %v\n", err)
			} else {
				fmt.Printf("    Auth Token (refreshable): %s\n", authToken)
			}

			// Generate static auth token (non-refreshable, long duration for tests)
			staticToken, err := record.NewStaticAuthToken(86400 * 365 * 100 * time.Second) // ~100 years
			if err != nil {
				fmt.Printf("    Static Token Error: %v\n", err)
			} else {
				fmt.Printf("    Static Token (100yr): %s\n", staticToken)
			}

			// Generate verification token
			verifyToken, err := record.NewVerificationToken()
			if err != nil {
				fmt.Printf("    Verify Token Error: %v\n", err)
			} else {
				fmt.Printf("    Verification Token: %s\n", verifyToken)
			}

			// Generate password reset token
			resetToken, err := record.NewPasswordResetToken()
			if err != nil {
				fmt.Printf("    Reset Token Error: %v\n", err)
			} else {
				fmt.Printf("    Password Reset Token: %s\n", resetToken)
			}
		}
		fmt.Println()
	}

	// Also generate some tokens for superusers
	fmt.Println("=== Superuser Tokens ===")
	superusers, err := app.FindAllRecords(core.CollectionNameSuperusers)
	if err != nil {
		log.Printf("Error getting superusers: %v", err)
	} else {
		for _, su := range superusers {
			fmt.Printf("\nSuperuser: %s (email: %s)\n", su.Id, su.Email())

			authToken, err := su.NewAuthToken()
			if err != nil {
				fmt.Printf("  Auth Token Error: %v\n", err)
			} else {
				fmt.Printf("  Auth Token: %s\n", authToken)
			}

			staticToken, err := su.NewStaticAuthToken(86400 * 365 * 100 * time.Second)
			if err != nil {
				fmt.Printf("  Static Token Error: %v\n", err)
			} else {
				fmt.Printf("  Static Token: %s\n", staticToken)
			}
		}
	}

	// Generate a mapping JSON for easy replacement
	generateTokenMapping(app)
}

type TokenMapping struct {
	CollectionID   string `json:"collectionId"`
	CollectionName string `json:"collectionName"`
	RecordID       string `json:"recordId"`
	Email          string `json:"email"`
	AuthToken      string `json:"authToken"`
	StaticToken    string `json:"staticToken"`
	VerifyToken    string `json:"verifyToken,omitempty"`
	ResetToken     string `json:"resetToken,omitempty"`
}

func generateTokenMapping(app core.App) {
	fmt.Println("\n=== JSON Token Mapping ===")

	var mappings []TokenMapping

	collections, _ := app.FindAllCollections()
	for _, col := range collections {
		if !col.IsAuth() {
			continue
		}

		records, _ := app.FindAllRecords(col.Name)
		for _, record := range records {
			mapping := TokenMapping{
				CollectionID:   col.Id,
				CollectionName: col.Name,
				RecordID:       record.Id,
				Email:          record.Email(),
			}

			if t, err := record.NewAuthToken(); err == nil {
				mapping.AuthToken = t
			}
			if t, err := record.NewStaticAuthToken(86400 * 365 * 100 * time.Second); err == nil {
				mapping.StaticToken = t
			}
			if t, err := record.NewVerificationToken(); err == nil {
				mapping.VerifyToken = t
			}
			if t, err := record.NewPasswordResetToken(); err == nil {
				mapping.ResetToken = t
			}

			mappings = append(mappings, mapping)
		}
	}

	jsonData, _ := json.MarshalIndent(mappings, "", "  ")
	fmt.Println(string(jsonData))

	// Write to file
	os.WriteFile("token_mapping.json", jsonData, 0644)
	fmt.Println("\nWritten to token_mapping.json")
}

// Helper to manually create a token with specific claims (for debugging)
func createManualToken(key string, claims jwt.MapClaims, duration time.Duration) string {
	token, _ := security.NewJWT(claims, key, duration)
	return token
}

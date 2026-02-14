// Command tokenreplace replaces old PocketBase test tokens with new Hanzo Base tokens.
// Usage: go run cmd/tokenreplace/main.go
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
)

// OldToNewCollectionID maps old PocketBase IDs to new Hanzo Base IDs
var OldToNewCollectionID = map[string]string{
	"_pb_users_auth_": "_hz_users_auth_",
	"pbc_3142635823":  "hbc_3142635823",
}

// CollectionsNeedingResign - collections with same IDs but tokens need new signatures
var CollectionsNeedingResign = map[string]bool{
	"v851q4r790rhknl": true, // clients
	"kpv709sk2lqbqk8": true, // nologin
}

type TokenPayload struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	CollectionID string `json:"collectionId"`
	Exp          int64  `json:"exp"`
	Refreshable  *bool  `json:"refreshable,omitempty"`
	Email        string `json:"email,omitempty"`
	NewEmail     string `json:"newEmail,omitempty"`
}

func main() {
	// Find the test data directory
	_, currentFile, _, _ := runtime.Caller(0)
	testDataDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "tests", "data")
	apisDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "apis")

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       testDataDir,
		EncryptionEnv: "hz_test_env",
	})

	if err := app.Bootstrap(); err != nil {
		fmt.Printf("Failed to bootstrap: %v\n", err)
		os.Exit(1)
	}

	// Build a mapping of record ID -> collection secrets
	secretMapping := make(map[string]map[string]string) // recordID -> {collectionId, authSecret, verifySecret, ...}

	collections, _ := app.FindAllCollections()
	for _, col := range collections {
		if !col.IsAuth() {
			continue
		}
		records, _ := app.FindAllRecords(col.Name)
		for _, record := range records {
			secretMapping[record.Id] = map[string]string{
				"collectionId":  col.Id,
				"tokenKey":      record.TokenKey(),
				"authSecret":    col.AuthToken.Secret,
				"verifySecret":  col.VerificationToken.Secret,
				"resetSecret":   col.PasswordResetToken.Secret,
				"changeSecret":  col.EmailChangeToken.Secret,
				"fileSecret":    col.FileToken.Secret,
			}
		}
	}

	// Find all Go test files with JWT tokens
	fmt.Println("Scanning for JWT tokens in test files...")

	tokenReplacements := make(map[string]string)

	err := filepath.Walk(apisDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Find all JWT tokens in the file (they start with eyJ)
		tokens := extractJWTTokens(string(content))
		for _, oldToken := range tokens {
			if _, exists := tokenReplacements[oldToken]; exists {
				continue
			}

			newToken, err := regenerateToken(oldToken, secretMapping)
			if err != nil {
				fmt.Printf("  Warning: Could not regenerate token: %v\n", err)
				continue
			}
			if newToken != "" && newToken != oldToken {
				tokenReplacements[oldToken] = newToken
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nFound %d tokens to replace\n", len(tokenReplacements))

	// Generate sed commands
	fmt.Println("\n=== SED REPLACEMENT COMMANDS ===")
	for old, new := range tokenReplacements {
		// Escape special characters for sed
		fmt.Printf("sed -i '' 's/%s/%s/g' apis/*_test.go core/*_test.go\n", old, new)
	}

	// Also output a JSON mapping
	jsonData, _ := json.MarshalIndent(tokenReplacements, "", "  ")
	os.WriteFile("token_replacements.json", jsonData, 0644)
	fmt.Println("\nWritten token_replacements.json")

	// Actually do the replacements
	fmt.Println("\n=== PERFORMING REPLACEMENTS ===")
	replaceCount := 0
	err = filepath.Walk(apisDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		newContent := string(content)
		modified := false
		for old, newToken := range tokenReplacements {
			if strings.Contains(newContent, old) {
				newContent = strings.ReplaceAll(newContent, old, newToken)
				modified = true
				replaceCount++
			}
		}

		if modified {
			if err := os.WriteFile(path, []byte(newContent), info.Mode()); err != nil {
				return err
			}
			fmt.Printf("  Updated: %s\n", path)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error during replacement: %v\n", err)
	}

	// Also check core test files
	coreDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "core")
	err = filepath.Walk(coreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		newContent := string(content)
		modified := false
		for old, newToken := range tokenReplacements {
			if strings.Contains(newContent, old) {
				newContent = strings.ReplaceAll(newContent, old, newToken)
				modified = true
				replaceCount++
			}
		}

		if modified {
			if err := os.WriteFile(path, []byte(newContent), info.Mode()); err != nil {
				return err
			}
			fmt.Printf("  Updated: %s\n", path)
		}
		return nil
	})

	fmt.Printf("\nCompleted %d replacements\n", replaceCount)
}

func extractJWTTokens(content string) []string {
	var tokens []string
	seen := make(map[string]bool)

	// JWT tokens start with eyJ (base64 of {"alg":)
	idx := 0
	for {
		start := strings.Index(content[idx:], "eyJ")
		if start == -1 {
			break
		}
		start += idx

		// Find the end of the token (usually ends at a quote or whitespace)
		end := start
		for end < len(content) {
			c := content[end]
			if c == '"' || c == '\'' || c == ' ' || c == '\n' || c == '\t' || c == ',' || c == '}' {
				break
			}
			end++
		}

		token := content[start:end]

		// Validate it's a JWT (has 2 dots)
		if strings.Count(token, ".") == 2 && !seen[token] {
			seen[token] = true
			tokens = append(tokens, token)
		}

		idx = end
	}
	return tokens
}

func regenerateToken(oldToken string, secretMapping map[string]map[string]string) (string, error) {
	// Parse the old token without verification
	parts := strings.Split(oldToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid token format")
	}

	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode payload: %v", err)
	}

	var payload TokenPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", fmt.Errorf("failed to unmarshal payload: %v", err)
	}

	// Check if this is a token that needs replacement (either ID change or resign)
	newCollectionID, needsIDReplacement := OldToNewCollectionID[payload.CollectionID]
	needsResign := CollectionsNeedingResign[payload.CollectionID]

	if !needsIDReplacement && !needsResign {
		// Token is fine as-is
		return "", nil
	}

	if !needsIDReplacement {
		// Collection ID stays the same, just resign
		newCollectionID = payload.CollectionID
	}

	// Get the record's secret info
	secrets, ok := secretMapping[payload.ID]
	if !ok {
		return "", fmt.Errorf("record %s not found in test data", payload.ID)
	}

	// Build new claims
	claims := jwt.MapClaims{
		"id":           payload.ID,
		"type":         payload.Type,
		"collectionId": newCollectionID,
		"exp":          payload.Exp,
	}
	if payload.Refreshable != nil {
		claims["refreshable"] = *payload.Refreshable
	}
	if payload.Email != "" {
		claims["email"] = payload.Email
	}
	if payload.NewEmail != "" {
		claims["newEmail"] = payload.NewEmail
	}

	// Determine signing key based on token type
	var signingKey string
	tokenKey := secrets["tokenKey"]
	switch payload.Type {
	case "auth":
		signingKey = tokenKey + secrets["authSecret"]
	case "verification":
		signingKey = tokenKey + secrets["verifySecret"]
	case "passwordReset":
		signingKey = tokenKey + secrets["resetSecret"]
	case "emailChange":
		signingKey = tokenKey + secrets["changeSecret"]
	case "file":
		signingKey = tokenKey + secrets["fileSecret"]
	default:
		return "", fmt.Errorf("unknown token type: %s", payload.Type)
	}

	// Calculate duration from expiration
	expTime := time.Unix(payload.Exp, 0)
	duration := time.Until(expTime)
	if duration < 0 {
		// Token already expired, use a long duration for testing
		duration = 86400 * 365 * 100 * time.Second // 100 years
		claims["exp"] = time.Now().Add(duration).Unix()
	}

	// Generate new token
	newToken, err := security.NewJWT(claims, signingKey, duration)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %v", err)
	}

	return newToken, nil
}

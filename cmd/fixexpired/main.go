// Command fixexpired replaces valid tokens with expired ones for tests that expect token rejection
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/security"
)

func main() {
	// Find the test data directory
	_, currentFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Join(filepath.Dir(currentFile), "..", "..")
	testDataDir := filepath.Join(baseDir, "tests", "data")
	apisDir := filepath.Join(baseDir, "apis")

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       testDataDir,
		EncryptionEnv: "hz_test_env",
	})

	if err := app.Bootstrap(); err != nil {
		log.Fatalf("Failed to bootstrap: %v", err)
	}

	// Generate EXPIRED auth tokens

	// Superuser
	suRecord, _ := app.FindRecordById("_superusers", "sywbhecnh46rhm0")
	suCol, _ := app.FindCollectionByNameOrId("_superusers")
	suTokenKey := suRecord.TokenKey()
	suAuthKey := suTokenKey + suCol.AuthToken.Secret

	expiredSuAuthToken := createAuthToken("sywbhecnh46rhm0", "hbc_3142635823", suAuthKey, -24*time.Hour)
	fmt.Printf("Expired superuser auth token: %s\n", expiredSuAuthToken)

	// Regular user
	userRecord, _ := app.FindRecordById("users", "4q1xlclmfloku33")
	userCol, _ := app.FindCollectionByNameOrId("users")
	userTokenKey := userRecord.TokenKey()
	userAuthKey := userTokenKey + userCol.AuthToken.Secret

	expiredUserAuthToken := createAuthToken("4q1xlclmfloku33", "_hz_users_auth_", userAuthKey, -24*time.Hour)
	fmt.Printf("Expired user auth token: %s\n", expiredUserAuthToken)

	// Define replacements - replace VALID tokens (exp far future) with EXPIRED tokens
	// These are the tokens that need to be expired for the "expired/invalid token" tests that expect 401
	replacements := map[string]string{
		// Superuser auth token (valid) -> expired
		// This appears in RequireSuperuserAuth and RequireSuperuserOrOwnerAuth tests
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJoYmNfMzE0MjYzNTgyMyIsImV4cCI6NDkyMjQxNjA4OCwiaWQiOiJzeXdiaGVjbmg0NnJobTAiLCJyZWZyZXNoYWJsZSI6dHJ1ZSwidHlwZSI6ImF1dGgifQ.sJXChe3RN8ZYKTjpFae7z2l9YzEUiqinBb7cHOLVtw8": expiredSuAuthToken,
	}

	// Read middlewares_test.go
	middlewaresPath := filepath.Join(apisDir, "middlewares_test.go")
	content, err := os.ReadFile(middlewaresPath)
	if err != nil {
		log.Fatalf("Error reading middlewares_test.go: %v", err)
	}

	newContent := string(content)
	totalReplacements := 0

	for old, newToken := range replacements {
		if strings.Contains(newContent, old) {
			// Count occurrences
			count := strings.Count(newContent, old)
			newContent = strings.ReplaceAll(newContent, old, newToken)
			totalReplacements += count
			fmt.Printf("Replaced %d occurrence(s) of superuser token\n", count)
		}
	}

	// Save the file
	if err := os.WriteFile(middlewaresPath, []byte(newContent), 0644); err != nil {
		log.Fatalf("Error writing middlewares_test.go: %v", err)
	}

	fmt.Printf("\nTotal replacements: %d\n", totalReplacements)

	// Now also fix RequireAuth and RequireSameCollectionContextAuth tests
	// These need expired USER tokens (for test@example.com / 4q1xlclmfloku33)

	// Actually, looking at the tests again:
	// - RequireAuth expects 200 for the expired/invalid token test (it's testing graceful handling)
	// - RequireSameCollectionContextAuth expects 401 for expired token (it's testing rejection)
	// So we need to figure out which test context needs which token

	fmt.Println("\nNote: Some tests may need manual review to determine if tokens should be expired or valid")
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

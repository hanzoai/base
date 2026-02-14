// Command fixtokens generates all needed tokens and replaces corrupted ones in test files.
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
	coreDir := filepath.Join(baseDir, "core")

	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       testDataDir,
		EncryptionEnv: "hz_test_env",
	})

	if err := app.Bootstrap(); err != nil {
		log.Fatalf("Failed to bootstrap: %v", err)
	}

	// Generate superuser file tokens
	suRecord, _ := app.FindRecordById("_superusers", "sywbhecnh46rhm0")
	suCol, _ := app.FindCollectionByNameOrId("_superusers")
	suTokenKey := suRecord.TokenKey()
	suFileKey := suTokenKey + suCol.FileToken.Secret

	validSuFileToken := createFileToken("sywbhecnh46rhm0", "hbc_3142635823", suFileKey, 86400*365*100*time.Second)
	expiredSuFileToken := createFileToken("sywbhecnh46rhm0", "hbc_3142635823", suFileKey, -1*time.Hour)

	fmt.Printf("Valid superuser file token: %s\n", validSuFileToken)
	fmt.Printf("Expired superuser file token: %s\n", expiredSuFileToken)

	// Generate user file tokens
	userRecord, _ := app.FindRecordById("users", "4q1xlclmfloku33")
	userCol, _ := app.FindCollectionByNameOrId("users")
	userTokenKey := userRecord.TokenKey()
	userFileKey := userTokenKey + userCol.FileToken.Secret

	validUserFileToken := createFileToken("4q1xlclmfloku33", "_hz_users_auth_", userFileKey, 86400*365*100*time.Second)
	expiredUserFileToken := createFileToken("4q1xlclmfloku33", "_hz_users_auth_", userFileKey, -1*time.Hour)

	fmt.Printf("Valid user file token: %s\n", validUserFileToken)
	fmt.Printf("Expired user file token: %s\n", expiredUserFileToken)

	// Generate expired auth tokens
	suAuthKey := suTokenKey + suCol.AuthToken.Secret
	expiredSuAuthToken := createAuthToken("sywbhecnh46rhm0", "hbc_3142635823", suAuthKey, -1*time.Hour)
	fmt.Printf("Expired superuser auth token: %s\n", expiredSuAuthToken)

	userAuthKey := userTokenKey + userCol.AuthToken.Secret
	expiredUserAuthToken := createAuthToken("4q1xlclmfloku33", "_hz_users_auth_", userAuthKey, -1*time.Hour)
	fmt.Printf("Expired user auth token: %s\n", expiredUserAuthToken)

	// Define replacements
	replacements := map[string]string{
		// Expired superuser file tokens (corrupted with "base" and old pbc_ prefix)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MTY0MDk5MTY2MSwidHlwZSI6ImZbaseGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.nqqtqpPhxU0045F4XP_ruAkzAidYBc5oPy9ErN3XBq0": expiredSuFileToken,
		// Valid superuser file tokens (corrupted with "base" and old pbc_ prefix)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6InN5d2JoZWNuaDQ2cmhtMCIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZbaseGUiLCJjb2xsZWN0aW9uSWQiOiJwYmNfMzE0MjYzNTgyMyJ9.Lupz541xRvrktwkrl55p5pPCF77T69ZRsohsIcb2dxc": validSuFileToken,
		// Valid user file tokens (corrupted with "fbaseGU" and old _pb_ prefix)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6ImZbaseGUiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8ifQ.nSTLuCPcGpWn2K2l-BFkC3Vlzc-ZTDPByYq8dN1oPSo": validUserFileToken,
	}

	totalReplacements := 0

	// Process test files
	for _, dir := range []string{apisDir, coreDir} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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

			for old, newToken := range replacements {
				if strings.Contains(newContent, old) {
					newContent = strings.ReplaceAll(newContent, old, newToken)
					modified = true
					totalReplacements++
					fmt.Printf("Replaced token in %s\n", filepath.Base(path))
				}
			}

			if modified {
				if err := os.WriteFile(path, []byte(newContent), info.Mode()); err != nil {
					return err
				}
				fmt.Printf("Updated: %s\n", path)
			}

			return nil
		})

		if err != nil {
			fmt.Printf("Error walking %s: %v\n", dir, err)
		}
	}

	fmt.Printf("\nTotal replacements: %d\n", totalReplacements)
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

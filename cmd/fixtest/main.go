// Command fixtest fixes corrupted JWT tokens in test files by analyzing context and replacing with fresh tokens.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// TokenSet contains all token types for a record
type TokenSet struct {
	Auth                 string `json:"auth"`
	Static               string `json:"static"`
	Verification         string `json:"verification"`
	PasswordReset        string `json:"passwordReset"`
	EmailChange          string `json:"emailChange"`
	File                 string `json:"file"`
	ExpiredVerification  string `json:"expiredVerification"`
	ExpiredPasswordReset string `json:"expiredPasswordReset"`
	ExpiredEmailChange   string `json:"expiredEmailChange"`
}

func main() {
	// Find the test data directory
	_, currentFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Join(filepath.Dir(currentFile), "..", "..")
	apisDir := filepath.Join(baseDir, "apis")
	coreDir := filepath.Join(baseDir, "core")

	// Load the fresh tokens
	tokenData, err := os.ReadFile(filepath.Join(baseDir, "all_tokens.json"))
	if err != nil {
		fmt.Printf("Error reading all_tokens.json: %v\n", err)
		os.Exit(1)
	}

	var freshTokens map[string]TokenSet
	if err := json.Unmarshal(tokenData, &freshTokens); err != nil {
		fmt.Printf("Error parsing all_tokens.json: %v\n", err)
		os.Exit(1)
	}

	// Define known token patterns to replace - these are corrupted tokens with "base" in them
	// Map of: corrupted_pattern -> (recordId, tokenType)
	replacements := map[string]struct {
		RecordID  string
		TokenType string
	}{
		// Users collection - test@example.com (4q1xlclmfloku33)
		// Verification tokens (type: verification, collectionId: _hz_users_auth_ or corrupted _pb_users_auth_)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MTY0MDk5MTY2MSwidHlwZSI6InZlcmlmaWNhdGlvbiIsImNvbGxlY3Rbase25JZCI6Il9wYl91c2Vyc19hdXRoXyIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSJ9.qqelNNL2Udl6K_TJ282sNHYCpASgA6SIuSVKGfBHMZU": {"4q1xlclmfloku33", "expiredVerification"},
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6InZlcmlmaWNhdGlvbiIsImNvbGxlY3Rbase25JZCI6Il9wYl91c2Vyc19hdXRoXyIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSJ9.SetHpu2H-x-q4TIUz-xiQjwi7MNwLCLvSs4O0hUSp0E": {"4q1xlclmfloku33", "verification"},
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6IjRxMXhsY2xtZmxva3UzMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6InBhc3N3b3JkUmVzZXQiLCJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8iLCJlbWFbaseCI6InRlc3RAZXhhbXBsZS5jb20ifQ.xR-xq1oHDy0D8Q4NDOAEyYKGHWd_swzoiSoL8FLFBHY": {"4q1xlclmfloku33", "passwordReset"},

		// test2@example.com (oap640cot4yru2s)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6Im9hcDY0MGNvdDR5cnUycyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6InZlcmlmaWNhdGlvbiIsImNvbGxlY3Rbase25JZCI6Il9wYl91c2Vyc19hdXRoXyIsImVtYWlsIjoidGVzdDJAZXhhbXBsZS5jb20ifQ.QQmM3odNFVk6u4J4-5H8IBM3dfk9YCD7mPW-8PhBAI8": {"oap640cot4yru2s", "verification"},

		// Nologin collection - test@example.com (dc49k6jgejn40h3)
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6ImRjNDlrNmpnZWpuNDBoMyIsImV4cCI6MjUyNDYwNDQ2MSwidHlwZSI6InZlcmlmaWNhdGlvbiIsImNvbGxlY3Rbase25JZCI6ImtwdjcwOXNrMmxxYnFrOCIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSJ9.5GmuZr4vmwk3Cb_3ZZWNxwbE75KZC-j71xxIPR9AsVw": {"dc49k6jgejn40h3", "verification"},
	}

	// Pattern to find JWT tokens in test files
	jwtPattern := regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

	// Additional pattern for corrupted tokens containing "base"
	corruptedPattern := regexp.MustCompile(`eyJ[A-Za-z0-9_-]*base[A-Za-z0-9_-]*`)

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

			// First, replace known corrupted tokens
			for old, info := range replacements {
				if strings.Contains(newContent, old) {
					tokens := freshTokens[info.RecordID]
					var newToken string
					switch info.TokenType {
					case "auth":
						newToken = tokens.Auth
					case "static":
						newToken = tokens.Static
					case "verification":
						newToken = tokens.Verification
					case "passwordReset":
						newToken = tokens.PasswordReset
					case "emailChange":
						newToken = tokens.EmailChange
					case "file":
						newToken = tokens.File
					case "expiredVerification":
						newToken = tokens.ExpiredVerification
					case "expiredPasswordReset":
						newToken = tokens.ExpiredPasswordReset
					case "expiredEmailChange":
						newToken = tokens.ExpiredEmailChange
					}
					if newToken != "" {
						newContent = strings.ReplaceAll(newContent, old, newToken)
						modified = true
						totalReplacements++
						fmt.Printf("  Replaced %s token for %s in %s\n", info.TokenType, info.RecordID, filepath.Base(path))
					}
				}
			}

			// Find any remaining corrupted tokens (containing "base" in the middle)
			corrupted := corruptedPattern.FindAllString(newContent, -1)
			for _, c := range corrupted {
				if jwtPattern.MatchString(c) {
					fmt.Printf("  WARNING: Found potentially corrupted token in %s: %s...\n", filepath.Base(path), c[:50])
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
			fmt.Printf("Error walking %s: %v\n", dir, err)
		}
	}

	fmt.Printf("\nTotal replacements: %d\n", totalReplacements)
}

// Command typegen generates TypeScript type definitions from a running Hanzo Base instance.
//
// Usage:
//
//	go run cmd/typegen/main.go --url http://localhost:8090 --output types.ts \
//	  --admin-email admin@example.com --admin-password secret
//
// It connects to the Base API, reads all collection schemas, and emits
// TypeScript interfaces with a Collections type map.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"unicode"
)

// --- API response types ---

type authResponse struct {
	Token string `json:"token"`
}

type field struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	System       bool     `json:"system"`
	Hidden       bool     `json:"hidden"`
	MaxSelect    int      `json:"maxSelect"`
	Values       []string `json:"values"`       // select
	CollectionID string   `json:"collectionId"` // relation
}

type collection struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	System bool    `json:"system"`
	Fields []field `json:"fields"`
}

// --- CLI flags ---

var (
	flagURL      = flag.String("url", "http://localhost:8090", "Base instance URL")
	flagOutput   = flag.String("output", "", "Output file path (default: stdout)")
	flagEmail    = flag.String("admin-email", "", "Superuser email for auth")
	flagPassword = flag.String("admin-password", "", "Superuser password for auth")
	flagExclude  = flag.String("exclude", "", "Comma-separated collection names to exclude")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "typegen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	baseURL := strings.TrimRight(*flagURL, "/")

	// 1. Authenticate
	token, err := authenticate(baseURL, *flagEmail, *flagPassword)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// 2. Fetch collections
	collections, err := fetchCollections(baseURL, token)
	if err != nil {
		return fmt.Errorf("fetch collections: %w", err)
	}

	// 3. Build exclusion set
	excludeSet := make(map[string]bool)
	if *flagExclude != "" {
		for _, name := range strings.Split(*flagExclude, ",") {
			excludeSet[strings.TrimSpace(name)] = true
		}
	}

	// 4. Filter and sort
	var filtered []collection
	for _, c := range collections {
		if excludeSet[c.Name] {
			continue
		}
		filtered = append(filtered, c)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	// 5. Build collection ID -> name map for relation resolution
	idToName := make(map[string]string, len(collections))
	for _, c := range collections {
		idToName[c.ID] = c.Name
	}

	// 6. Generate TypeScript
	ts := generate(filtered, idToName)

	// 7. Write output
	if *flagOutput != "" {
		if err := os.WriteFile(*flagOutput, []byte(ts), 0644); err != nil {
			return fmt.Errorf("write %s: %w", *flagOutput, err)
		}
		fmt.Fprintf(os.Stderr, "typegen: wrote %s (%d collections)\n", *flagOutput, len(filtered))
		return nil
	}

	_, err = fmt.Print(ts)
	return err
}

// authenticate obtains a superuser auth token.
// If email/password are empty it returns an empty token (anonymous access).
func authenticate(baseURL, email, password string) (string, error) {
	if email == "" || password == "" {
		return "", nil
	}

	body, err := json.Marshal(map[string]string{
		"identity": email,
		"password": password,
	})
	if err != nil {
		return "", err
	}

	url := baseURL + "/api/collections/_superusers/auth-with-password"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST %s: status %d: %s", url, resp.StatusCode, string(b))
	}

	var ar authResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return "", fmt.Errorf("decode auth response: %w", err)
	}
	return ar.Token, nil
}

// fetchCollections retrieves all collections from the Base API.
func fetchCollections(baseURL, token string) ([]collection, error) {
	url := baseURL + "/api/collections?perPage=500"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(b))
	}

	// The API returns a paginated response with items array.
	var result struct {
		Items []collection `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode collections: %w", err)
	}
	return result.Items, nil
}

// --- TypeScript generation ---

func generate(collections []collection, idToName map[string]string) string {
	var buf strings.Builder

	buf.WriteString("// Auto-generated by base typegen — do not edit.\n")
	buf.WriteString("// Source: " + *flagURL + "\n\n")

	for i, c := range collections {
		writeInterface(&buf, c, idToName)
		if i < len(collections)-1 {
			buf.WriteByte('\n')
		}
	}

	// Collections type map
	buf.WriteByte('\n')
	buf.WriteString("export type Collections = {\n")
	for _, c := range collections {
		buf.WriteString("  " + c.Name + ": " + pascalCase(c.Name) + "\n")
	}
	buf.WriteString("}\n")

	return buf.String()
}

func writeInterface(buf *strings.Builder, c collection, idToName map[string]string) {
	name := pascalCase(c.Name)
	buf.WriteString("export interface " + name + " {\n")

	for _, f := range c.Fields {
		if f.Hidden {
			continue
		}
		ts, comment := fieldToTS(f, idToName)
		line := "  " + f.Name + ": " + ts
		if comment != "" {
			line += "  // " + comment
		}
		buf.WriteString(line + "\n")
	}

	buf.WriteString("}\n")
}

// fieldToTS maps a Base field to its TypeScript type and an optional comment.
func fieldToTS(f field, idToName map[string]string) (tsType string, comment string) {
	switch f.Type {
	case "text", "email", "url", "editor", "password":
		return "string", ""

	case "number":
		return "number", ""

	case "bool":
		return "boolean", ""

	case "date", "autodate":
		return "string", ""

	case "json":
		return "unknown", ""

	case "geoPoint":
		return "{ lon: number; lat: number }", ""

	case "select":
		return selectType(f), ""

	case "file":
		if f.MaxSelect > 1 {
			return "string[]", ""
		}
		return "string", ""

	case "relation":
		target := idToName[f.CollectionID]
		if target == "" {
			target = f.CollectionID
		}
		cmt := "relation -> " + target
		if f.MaxSelect > 1 {
			return "string[]", cmt
		}
		return "string", cmt

	default:
		return "unknown", "unhandled type: " + f.Type
	}
}

// selectType generates a union type for single-select or union-array for multi-select.
func selectType(f field) string {
	if len(f.Values) == 0 {
		if f.MaxSelect > 1 {
			return "string[]"
		}
		return "string"
	}

	quoted := make([]string, len(f.Values))
	for i, v := range f.Values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", "\\'") + "'"
	}
	union := strings.Join(quoted, " | ")

	if f.MaxSelect > 1 {
		return "(" + union + ")[]"
	}
	return union
}

// pascalCase converts a snake_case or kebab-case name to PascalCase.
// Examples: "tasks" -> "Tasks", "user_roles" -> "UserRoles", "my-items" -> "MyItems"
func pascalCase(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))

	upper := true
	for _, r := range s {
		if r == '_' || r == '-' {
			upper = true
			continue
		}
		if upper {
			buf.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

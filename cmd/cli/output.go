package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is the output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat returns the output format from a string.
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	default:
		return FormatTable
	}
}

// DetectFormat returns table if stdout is a TTY, json otherwise.
func DetectFormat(explicit string) Format {
	if explicit != "" {
		return ParseFormat(explicit)
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return FormatJSON
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return FormatTable
	}
	return FormatJSON
}

// Print outputs data in the requested format to w.
func Print(w io.Writer, format Format, data json.RawMessage) error {
	switch format {
	case FormatJSON:
		var buf json.RawMessage
		if err := json.Unmarshal(data, &buf); err != nil {
			// already raw, just write
			_, err := w.Write(data)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
			return nil
		}
		pretty, err := json.MarshalIndent(buf, "", "  ")
		if err != nil {
			_, err := w.Write(data)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
			return nil
		}
		_, err = w.Write(pretty)
		if err != nil {
			return err
		}
		fmt.Fprintln(w)
		return nil

	case FormatYAML:
		var obj any
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		return yaml.NewEncoder(w).Encode(obj)

	default: // table
		return printTable(w, data)
	}
}

// printTable attempts to render JSON data as a tab-separated table.
// Works for arrays of objects or single objects.
func printTable(w io.Writer, data json.RawMessage) error {
	// try array of objects
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return printObjectsTable(w, arr)
	}

	// try paginated response: { items: [...], page, perPage, ... }
	var paginated struct {
		Items      []map[string]any `json:"items"`
		Page       int              `json:"page"`
		PerPage    int              `json:"perPage"`
		TotalItems int              `json:"totalItems"`
	}
	if err := json.Unmarshal(data, &paginated); err == nil && paginated.Items != nil {
		if len(paginated.Items) == 0 {
			fmt.Fprintln(w, "(no records)")
			return nil
		}
		if err := printObjectsTable(w, paginated.Items); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n(%d of %d total)\n", len(paginated.Items), paginated.TotalItems)
		return nil
	}

	// try single object
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		return printKV(w, obj)
	}

	// fallback: raw output
	_, err := w.Write(data)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

// printObjectsTable prints a slice of flat objects as a table.
func printObjectsTable(w io.Writer, objs []map[string]any) error {
	if len(objs) == 0 {
		fmt.Fprintln(w, "(empty)")
		return nil
	}

	// collect and sort keys
	keySet := map[string]struct{}{}
	for _, obj := range objs {
		for k := range obj {
			keySet[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// prioritize id, name, type, created, updated
	priority := []string{"id", "name", "type", "created", "updated"}
	ordered := make([]string, 0, len(keys))
	for _, p := range priority {
		for _, k := range keys {
			if k == p {
				ordered = append(ordered, k)
				break
			}
		}
	}
	for _, k := range keys {
		found := false
		for _, p := range priority {
			if k == p {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, k)
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(ordered, "\t"))

	for _, obj := range objs {
		vals := make([]string, len(ordered))
		for i, k := range ordered {
			vals[i] = formatValue(obj[k])
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}

	return tw.Flush()
}

// printKV prints a single object as key-value pairs.
func printKV(w io.Writer, obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", k, formatValue(obj[k]))
	}
	return tw.Flush()
}

func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

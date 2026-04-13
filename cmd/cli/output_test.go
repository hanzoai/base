package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintJSON(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"id":"abc","name":"test"}`)
	var buf bytes.Buffer
	if err := Print(&buf, FormatJSON, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"id"`) {
		t.Fatalf("expected JSON output, got: %s", out)
	}
}

func TestPrintYAML(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"id":"abc","name":"test"}`)
	var buf bytes.Buffer
	if err := Print(&buf, FormatYAML, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "id:") {
		t.Fatalf("expected YAML output, got: %s", out)
	}
	if !strings.Contains(out, "name:") {
		t.Fatalf("expected name in YAML output, got: %s", out)
	}
}

func TestPrintTableArray(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`[{"id":"1","name":"a"},{"id":"2","name":"b"}]`)
	var buf bytes.Buffer
	if err := Print(&buf, FormatTable, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 rows), got %d: %s", len(lines), out)
	}
}

func TestPrintTablePaginated(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"items":[{"id":"1","name":"a"}],"page":1,"perPage":10,"totalItems":1,"totalPages":1}`)
	var buf bytes.Buffer
	if err := Print(&buf, FormatTable, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "1 of 1 total") {
		t.Fatalf("expected pagination footer, got: %s", out)
	}
}

func TestPrintTableSingleObject(t *testing.T) {
	t.Parallel()

	data := json.RawMessage(`{"id":"abc","name":"test","type":"base"}`)
	var buf bytes.Buffer
	if err := Print(&buf, FormatTable, data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "id") || !strings.Contains(out, "abc") {
		t.Fatalf("expected key-value output, got: %s", out)
	}
}

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	if f := DetectFormat("json"); f != FormatJSON {
		t.Fatalf("expected json, got %s", f)
	}
	if f := DetectFormat("yaml"); f != FormatYAML {
		t.Fatalf("expected yaml, got %s", f)
	}
	if f := DetectFormat("table"); f != FormatTable {
		t.Fatalf("expected table, got %s", f)
	}
}

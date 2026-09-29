package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

func TestLookupPath(t *testing.T) {
	doc := map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": float64(42)}}}}
	got, ok := lookupPath(doc, "a.b.0.c")
	if !ok || got != float64(42) {
		t.Fatalf("got %v ok=%v", got, ok)
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		actual   any
		op       string
		expected any
		want     bool
	}{
		{float64(90), "gte", float64(85), true},
		{"healthy", "eq", "healthy", true},
		{"database timeout", "contains", "timeout", true},
		{false, "falsy", nil, true},
	}
	for _, tt := range tests {
		got, err := compare(tt.actual, tt.op, tt.expected)
		if err != nil || got != tt.want {
			t.Fatalf("%v %s %v: got %v err=%v", tt.actual, tt.op, tt.expected, got, err)
		}
	}
}

func TestRenderMessageFromItem(t *testing.T) {
	item := map[string]any{
		"name":    "Example",
		"message": "expires in 5 days",
		"status":  "critical",
	}
	got := renderMessage("{{name}}: {{message}}", item, "critical")
	if got != "Example: expires in 5 days" {
		t.Fatalf("got %q", got)
	}
}

func TestGenericJSONForEach(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attention":[{"name":"API","status":"critical","message":"expires in 3 days"},{"name":"Site","status":"warning","message":"expires in 20 days"}]}`))
	}))
	defer ts.Close()

	settings := json.RawMessage(`{"rules":[{"forEach":"attention","path":"status","op":"truthy","severityPath":"status","message":"{{name}}: {{message}}"}]}`)
	result := (GenericJSONAdapter{}).Check(context.Background(), ts.Client(), cfgpkg.SourceConfig{
		Name: "Certificates", Type: "generic-json", URL: ts.URL, Settings: settings,
	})
	if len(result.Errors) != 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	if len(result.Alerts) != 2 {
		t.Fatalf("got %d alerts", len(result.Alerts))
	}
	if result.Alerts[0].Severity != "critical" || result.Alerts[0].Message != "API: expires in 3 days" {
		t.Fatalf("first alert: %+v", result.Alerts[0])
	}
	if result.Alerts[1].Severity != "warning" || result.Alerts[1].Message != "Site: expires in 20 days" {
		t.Fatalf("second alert: %+v", result.Alerts[1])
	}
}

func TestGenericJSONForEachMissingCollectionIsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthyCount":4,"warningCount":0,"criticalCount":0}`))
	}))
	defer ts.Close()

	settings := json.RawMessage(`{"rules":[{"forEach":"attention","path":"status","op":"truthy","severityPath":"status","message":"{{name}}: {{message}}"}]}`)
	result := (GenericJSONAdapter{}).Check(context.Background(), ts.Client(), cfgpkg.SourceConfig{
		Name: "Certificates", Type: "generic-json", URL: ts.URL, Settings: settings,
	})
	if len(result.Errors) != 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	if len(result.Alerts) != 0 {
		t.Fatalf("got %d alerts, want 0", len(result.Alerts))
	}
}

func TestGenericJSONForEachNullCollectionIsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attention":null,"healthyCount":4,"warningCount":0,"criticalCount":0}`))
	}))
	defer ts.Close()

	settings := json.RawMessage(`{"rules":[{"forEach":"attention","path":"status","op":"truthy","severityPath":"status","message":"{{name}}: {{message}}"}]}`)
	result := (GenericJSONAdapter{}).Check(context.Background(), ts.Client(), cfgpkg.SourceConfig{
		Name: "Certificates", Type: "generic-json", URL: ts.URL, Settings: settings,
	})
	if len(result.Errors) != 0 {
		t.Fatalf("errors: %v", result.Errors)
	}
	if len(result.Alerts) != 0 {
		t.Fatalf("got %d alerts, want 0", len(result.Alerts))
	}
}

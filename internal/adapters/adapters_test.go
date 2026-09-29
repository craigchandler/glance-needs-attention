package adapters

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetJSONResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
		status                int
	}{
		{"valid", `{"ok":true}`, "", 200},
		{"status", "private response", "HTTP 503", 503},
		{"malformed", `{"secret":"private response"`, "not valid JSON", 200},
		{"multiple documents", `{} {"private response":true}`, "not valid JSON", 200},
		{"oversized", `{"value":"` + strings.Repeat("x", maxResponseBytes) + `"}`, "4 MiB", 200},
		{"at limit", `{}` + strings.Repeat(" ", maxResponseBytes-2), "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer ts.Close()
			var doc any
			err := getJSON(context.Background(), ts.Client(), ts.URL, &doc)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(err.Error(), "private response") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetJSONDoesNotExposeURLOrTransportErrors(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("private-transport-value")
	})}
	for _, endpoint := range []string{
		"https://example.invalid/status?token=private-query-value",
		"https://user:private-password@example.invalid/status",
		"http://[private-malformed-value",
		"file:///private-path",
		"/private-relative-path",
	} {
		var doc any
		err := getJSON(context.Background(), client, endpoint, &doc)
		if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "example.invalid") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

func TestGetJSONTimeout(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Millisecond, Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	var doc any
	if err := getJSON(context.Background(), client, "https://example.invalid/?token=private", &doc); err == nil || err.Error() != "source request timed out" {
		t.Fatalf("unexpected timeout: %v", err)
	}
}

func TestNumericErrorsOmitValues(t *testing.T) {
	for _, pair := range [][2]any{{"private-value", 1}, {1, "private-value"}} {
		_, err := compare(pair[0], "gte", pair[1])
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("unsafe comparison error: %v", err)
		}
	}
}

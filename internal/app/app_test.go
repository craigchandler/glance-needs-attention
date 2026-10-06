package app

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/craigchandler/glance-needs-attention/internal/adapters"
	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

func TestRefreshRetainsHealthySourcesWhenAnotherFails(t *testing.T) {
	responseTimes := make(chan time.Time, 4)
	var recovered atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/failed" {
			if recovered.Load() {
				_, _ = w.Write([]byte(`{"reports":[]}`))
			} else {
				http.Error(w, "private upstream details", http.StatusServiceUnavailable)
			}
			return
		}
		responseTimes <- time.Now()
		_, _ = w.Write([]byte(`{"attention":[{"name":"Certificate","status":"warning"}]}`))
	}))
	defer ts.Close()
	a := New(cfgpkg.Config{RefreshInterval: "1m", HTTPTimeout: "1s", Sources: []cfgpkg.SourceConfig{
		{Name: "Unavailable", Type: "backup-status", URL: ts.URL + "/failed"},
		{Name: "Certificates", Type: "generic-json", URL: ts.URL + "/healthy",
			Settings: json.RawMessage(`{"rules":[{"forEach":"attention","path":"status","op":"truthy","severityPath":"status","message":"{{name}}: {{value}}"}]}`)},
	}})
	a.refresh()
	s := a.snapshot()
	if len(s.Alerts) != 1 || s.Alerts[0].Message != "Certificate: warning" || len(s.Errors) != 1 || s.Errors[0] != "Unavailable: HTTP 503" {
		t.Fatalf("unexpected result: %+v", s)
	}
	if s.RefreshedAt.Before(<-responseTimes) {
		t.Fatal("refresh time must reflect completion, not the start of polling")
	}
	w := httptest.NewRecorder()
	a.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("source failure made polling unhealthy: %d", w.Code)
	}
	// A later successful poll clears the failed source error.
	recovered.Store(true)
	a.refresh()
	if len(a.snapshot().Errors) != 0 {
		t.Fatal("source error survived a successful refresh")
	}
}

func TestWidgetEscapesSourceData(t *testing.T) {
	unsafe := `<script>alert("example")</script>`
	a := New(cfgpkg.Config{})
	a.state = State{Alerts: []adapters.Alert{{Source: unsafe, Message: unsafe, Severity: "warning"}}, Errors: []string{unsafe}}
	w := httptest.NewRecorder()
	a.Widget(w, httptest.NewRequest(http.MethodGet, "/widget", nil))
	if strings.Contains(w.Body.String(), unsafe) || strings.Count(w.Body.String(), html.EscapeString(unsafe)) != 3 {
		t.Fatalf("source content was not escaped: %s", w.Body.String())
	}
}

func TestHealthFreshness(t *testing.T) {
	for _, tc := range []struct {
		name, interval string
		refreshed      time.Time
		want           int
	}{
		{"not started", "1m", time.Time{}, http.StatusServiceUnavailable},
		{"stale", "1m", time.Now().Add(-6 * time.Minute), http.StatusServiceUnavailable},
		{"fresh", "1m", time.Now(), http.StatusOK},
		{"large interval", "2000000h", time.Now(), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(cfgpkg.Config{RefreshInterval: tc.interval})
			a.state.RefreshedAt = tc.refreshed
			w := httptest.NewRecorder()
			a.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestStatusSummaryAndWidget(t *testing.T) {
	var upstreamAlerts atomic.Value
	upstreamAlerts.Store([]adapters.Alert{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"attention": upstreamAlerts.Load()})
	}))
	defer ts.Close()
	a := New(cfgpkg.Config{Title: "Needs Attention", RefreshInterval: "1m", HTTPTimeout: "1s", Sources: []cfgpkg.SourceConfig{
		{Name: "Example", Type: "generic-json", URL: ts.URL,
			Settings: json.RawMessage(`{"rules":[{"forEach":"attention","path":"severity","op":"truthy","severityPath":"severity","message":"{{message}}"}]}`)},
	}})

	t.Run("before first refresh", func(t *testing.T) {
		assertStatusResponse(t, a, []adapters.Alert{}, "healthy")
	})

	for _, tc := range []struct {
		name           string
		severities     []string
		wantSeverities []string
		wantStatus     string
	}{
		{name: "no alerts", wantStatus: "healthy"},
		{name: "warning only", severities: []string{"warning", "warning"}, wantSeverities: []string{"warning", "warning"}, wantStatus: "warning"},
		{name: "critical only", severities: []string{"critical"}, wantSeverities: []string{"critical"}, wantStatus: "critical"},
		{name: "critical with warning and info", severities: []string{"warning", "info", "critical"}, wantSeverities: []string{"critical", "warning", "info"}, wantStatus: "critical"},
		{name: "warning with info", severities: []string{"info", "warning"}, wantSeverities: []string{"warning", "info"}, wantStatus: "warning"},
		{name: "info only", severities: []string{"info"}, wantSeverities: []string{"info"}, wantStatus: "healthy"},
		{name: "cleared after alerts", wantStatus: "healthy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alerts := make([]adapters.Alert, 0, len(tc.severities))
			for _, severity := range tc.severities {
				alerts = append(alerts, adapters.Alert{Source: "Example", Severity: severity, Message: "Example " + severity})
			}
			upstreamAlerts.Store(alerts)
			a.refresh()
			wantAlerts := make([]adapters.Alert, 0, len(tc.wantSeverities))
			for _, severity := range tc.wantSeverities {
				wantAlerts = append(wantAlerts, adapters.Alert{Source: "Example", Severity: severity, Message: "Example " + severity})
			}
			assertStatusResponse(t, a, wantAlerts, tc.wantStatus)

			w := httptest.NewRecorder()
			a.Widget(w, httptest.NewRequest(http.MethodGet, "/widget", nil))
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" ||
				w.Header().Get("Widget-Title") != "Needs Attention" || w.Header().Get("Widget-Content-Type") != "html" {
				t.Fatalf("unexpected widget response: %d %v", w.Code, w.Header())
			}
			body := w.Body.String()
			if strings.Contains(body, "No issues requiring attention") != (len(wantAlerts) == 0) {
				t.Fatalf("unexpected widget clear state: %s", body)
			}
			for _, alert := range wantAlerts {
				if !strings.Contains(body, alert.Source) || !strings.Contains(body, alert.Message) {
					t.Fatalf("widget missing alert %+v: %s", alert, body)
				}
				mark := `<span class="color-primary">•</span>`
				switch alert.Severity {
				case "warning":
					mark = `<span class="color-highlight">!</span>`
				case "critical":
					mark = `<span class="color-negative">✗</span>`
				}
				if !strings.Contains(body, mark) {
					t.Fatalf("widget missing severity marker %s: %s", mark, body)
				}
			}

			w = httptest.NewRecorder()
			a.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("alert severity changed health semantics: %d", w.Code)
			}
		})
	}
}

func TestStatusSourceErrorsRemainSeparate(t *testing.T) {
	a := New(cfgpkg.Config{RefreshInterval: "1m", Sources: []cfgpkg.SourceConfig{{Name: "Example", Type: "unknown"}}})
	a.refresh()
	got := assertStatusResponse(t, a, []adapters.Alert{}, "healthy")
	if !reflect.DeepEqual(got.Errors, []string{`Example: unknown source type "unknown"`}) {
		t.Fatalf("source errors changed: %v", got.Errors)
	}
	w := httptest.NewRecorder()
	a.Widget(w, httptest.NewRequest(http.MethodGet, "/widget", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Data source issues (1)") {
		t.Fatalf("widget missing source error: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	a.Health(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("source error changed health semantics: %d", w.Code)
	}
}

func assertStatusResponse(t *testing.T, a *App, wantAlerts []adapters.Alert, wantStatus string) State {
	t.Helper()
	w := httptest.NewRecorder()
	a.Status(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected status response: %d %v", w.Code, w.Header())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"alerts", "alertCount", "status", "refreshedAt"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %q in status response: %s", key, w.Body.String())
		}
	}
	if len(wantAlerts) == 0 && string(fields["alerts"]) != "[]" {
		t.Fatalf("empty alerts must be [], got %s", fields["alerts"])
	}
	var got State
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Alerts, wantAlerts) || got.AlertCount != len(got.Alerts) || got.Status != wantStatus {
		t.Fatalf("unexpected status response: %s; want alerts %+v and status %q", w.Body.String(), wantAlerts, wantStatus)
	}
	wantRefreshedAt, err := a.snapshot().RefreshedAt.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(fields["refreshedAt"]) != string(wantRefreshedAt) {
		t.Fatalf("refresh timestamp format/value changed: got %s, want %s", fields["refreshedAt"], wantRefreshedAt)
	}
	if len(got.Errors) == 0 {
		if _, ok := fields["errors"]; ok {
			t.Fatal("empty source errors should remain omitted")
		}
	}
	var alertFields []map[string]json.RawMessage
	if err := json.Unmarshal(fields["alerts"], &alertFields); err != nil {
		t.Fatal(err)
	}
	for _, alert := range alertFields {
		for _, key := range []string{"source", "severity", "message"} {
			if _, ok := alert[key]; !ok {
				t.Fatalf("alert missing existing field %q: %v", key, alert)
			}
		}
	}
	return got
}

package app

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
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

package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

func TestGitHubAdapterIncludesSecurityAlertSummary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"counts":{"securityAlerts":18}}`))
	}))
	defer ts.Close()

	result := (GitHubAdapter{}).Check(context.Background(), ts.Client(), cfgpkg.SourceConfig{Name: "GitHub", URL: ts.URL})
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(result.Alerts))
	}
	if got, want := result.Alerts[0].Message, "18 security alert(s)"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if got, want := result.Alerts[0].Severity, "critical"; got != want {
		t.Fatalf("severity = %q, want %q", got, want)
	}
}

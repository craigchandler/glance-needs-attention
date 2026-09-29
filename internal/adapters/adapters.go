package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type Alert struct {
	Source   string `json:"source"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type Result struct {
	Alerts []Alert
	Errors []string
}

type Adapter interface {
	Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result
}

func ForType(t string) (Adapter, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "backup-status":
		return BackupAdapter{}, nil
	case "github-status":
		return GitHubAdapter{}, nil
	case "qnap-status":
		return QNAPAdapter{}, nil
	case "azure-cost":
		return AzureCostAdapter{}, nil
	case "generic-json":
		return GenericJSONAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown source type %q", t)
	}
}

const maxResponseBytes = 4 << 20

func getJSON(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("source URL must be HTTP(S) with a host and no embedded credentials")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid source request")
	}
	resp, err := client.Do(req)
	if err != nil {
		// URL and transport errors can contain credentials, query strings or
		// response text. Only expose a safe category to the status/widget.
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("source request timed out")
		}
		return errors.New("source request failed (check endpoint and connectivity)")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return errors.New("could not read source response")
	}
	if len(body) > maxResponseBytes {
		return errors.New("source response exceeds 4 MiB limit")
	}
	if dst != nil {
		if err := json.Unmarshal(body, dst); err != nil {
			return errors.New("source response is not valid JSON for this adapter")
		}
	}
	return nil
}

func decodeSettings(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func severity(v, fallback string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "critical", "warning", "info":
		return v
	default:
		return fallback
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

package adapters

import (
	"context"
	"fmt"
	"net/http"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type AzureCostAdapter struct{}

type azureSettings struct {
	MonthToDateWarn float64 `json:"monthToDateWarn"`
	Severity        string  `json:"severity"`
	DefaultCurrency string  `json:"defaultCurrency"`
}

type azureStatus struct {
	Summary struct {
		MonthToDate float64 `json:"monthToDate"`
		Currency    string  `json:"currency"`
	} `json:"summary"`
	Error string `json:"error"`
}

func (AzureCostAdapter) Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result {
	settings := azureSettings{Severity: "warning", DefaultCurrency: "AUD"}
	if err := decodeSettings(source.Settings, &settings); err != nil {
		return Result{Errors: []string{source.Name + ": invalid settings: " + err.Error()}}
	}
	var s azureStatus
	if err := getJSON(ctx, client, source.URL, &s); err != nil {
		return Result{Errors: []string{source.Name + ": " + err.Error()}}
	}
	if s.Error != "" {
		return Result{Alerts: []Alert{{source.Name, "critical", s.Error}}}
	}
	if settings.MonthToDateWarn > 0 && s.Summary.MonthToDate >= settings.MonthToDateWarn {
		cur := s.Summary.Currency
		if cur == "" {
			cur = settings.DefaultCurrency
		}
		return Result{Alerts: []Alert{{source.Name, severity(settings.Severity, "warning"), fmt.Sprintf("Month-to-date cost %.2f %s exceeds %.2f", s.Summary.MonthToDate, cur, settings.MonthToDateWarn)}}}
	}
	return Result{}
}

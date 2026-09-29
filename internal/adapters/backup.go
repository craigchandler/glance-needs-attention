package adapters

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type BackupAdapter struct{}

type backupSettings struct {
	FriendlySources map[string]string `json:"friendlySources"`
	FailedSeverity  string            `json:"failedSeverity"`
	WarningSeverity string            `json:"warningSeverity"`
	StaleSeverity   string            `json:"staleSeverity"`
}

type backupStatus struct {
	Reports []struct {
		Source  string `json:"source"`
		Status  string `json:"status"`
		Stale   bool   `json:"stale"`
		AgeText string `json:"ageText"`
	} `json:"reports"`
	Error string `json:"error"`
}

func (BackupAdapter) Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result {
	settings := backupSettings{FailedSeverity: "critical", WarningSeverity: "warning", StaleSeverity: "warning"}
	if err := decodeSettings(source.Settings, &settings); err != nil {
		return Result{Errors: []string{source.Name + ": invalid settings: " + err.Error()}}
	}
	var s backupStatus
	if err := getJSON(ctx, client, source.URL, &s); err != nil {
		return Result{Errors: []string{source.Name + ": " + err.Error()}}
	}
	if s.Error != "" {
		return Result{Alerts: []Alert{{source.Name, "critical", s.Error}}}
	}
	var out Result
	for _, r := range s.Reports {
		name := r.Source
		if v, ok := settings.FriendlySources[r.Source]; ok {
			name = v
		}
		switch {
		case r.Stale:
			msg := name + " backup is stale"
			if r.AgeText != "" {
				msg += " (" + r.AgeText + ")"
			}
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.StaleSeverity, "warning"), msg})
		case strings.EqualFold(r.Status, "failed"):
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.FailedSeverity, "critical"), fmt.Sprintf("%s backup failed", name)})
		case strings.EqualFold(r.Status, "warning"):
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.WarningSeverity, "warning"), fmt.Sprintf("%s backup completed with warnings", name)})
		}
	}
	return out
}

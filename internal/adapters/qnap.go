package adapters

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type QNAPAdapter struct{}

type qnapSettings struct {
	VolumeWarnPercent   float64  `json:"volumeWarnPercent"`
	HealthyVolumeStatus []string `json:"healthyVolumeStatuses"`
	HealthyDiskStatus   []string `json:"healthyDiskStatuses"`
	UsageSeverity       string   `json:"usageSeverity"`
	HealthSeverity      string   `json:"healthSeverity"`
}

type qnapStatus struct {
	Volume struct {
		UsedPercent float64 `json:"usedPercent"`
		Status      string  `json:"status"`
	} `json:"volume"`
	Disks []struct {
		Name   string `json:"name"`
		Health string `json:"health"`
	} `json:"disks"`
}

func inSet(v string, set []string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, x := range set {
		if v == strings.ToLower(strings.TrimSpace(x)) {
			return true
		}
	}
	return false
}

func (QNAPAdapter) Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result {
	settings := qnapSettings{
		VolumeWarnPercent:   85,
		HealthyVolumeStatus: []string{"ready", "good"},
		HealthyDiskStatus:   []string{"good", "ok", "normal"},
		UsageSeverity:       "warning",
		HealthSeverity:      "critical",
	}
	if err := decodeSettings(source.Settings, &settings); err != nil {
		return Result{Errors: []string{source.Name + ": invalid settings: " + err.Error()}}
	}
	var s qnapStatus
	if err := getJSON(ctx, client, source.URL, &s); err != nil {
		return Result{Errors: []string{source.Name + ": " + err.Error()}}
	}
	var out Result
	if s.Volume.Status != "" && !inSet(s.Volume.Status, settings.HealthyVolumeStatus) {
		out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.HealthSeverity, "critical"), "Volume status: " + s.Volume.Status})
	}
	if settings.VolumeWarnPercent > 0 && s.Volume.UsedPercent >= settings.VolumeWarnPercent {
		out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.UsageSeverity, "warning"), fmt.Sprintf("Volume is %.0f%% full", s.Volume.UsedPercent)})
	}
	for _, d := range s.Disks {
		if strings.TrimSpace(d.Health) != "" && !inSet(d.Health, settings.HealthyDiskStatus) {
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.HealthSeverity, "critical"), fmt.Sprintf("%s health: %s", d.Name, d.Health)})
		}
	}
	return out
}

package adapters

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type GitHubAdapter struct{}

type githubSettings struct {
	IncludeSecurityAlerts *bool  `json:"includeSecurityAlerts"`
	IncludeFailedRuns     *bool  `json:"includeFailedRuns"`
	IncludeReviewRequests *bool  `json:"includeReviewRequests"`
	IncludeAssignedIssues *bool  `json:"includeAssignedIssues"`
	SecuritySeverity      string `json:"securitySeverity"`
	FailedRunSeverity     string `json:"failedRunSeverity"`
	ReviewSeverity        string `json:"reviewSeverity"`
	AssignedIssueSeverity string `json:"assignedIssueSeverity"`
}

type githubStatus struct {
	Counts     map[string]int                            `json:"counts"`
	FailedRuns []struct{ Repo, Name, Conclusion string } `json:"failedRuns"`
	Error      string                                    `json:"error"`
}

func enabled(v *bool, d bool) bool {
	if v == nil {
		return d
	}
	return *v
}

func (GitHubAdapter) Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result {
	settings := githubSettings{SecuritySeverity: "critical", FailedRunSeverity: "critical", ReviewSeverity: "info", AssignedIssueSeverity: "info"}
	if err := decodeSettings(source.Settings, &settings); err != nil {
		return Result{Errors: []string{source.Name + ": invalid settings: " + err.Error()}}
	}
	var s githubStatus
	if err := getJSON(ctx, client, source.URL, &s); err != nil {
		return Result{Errors: []string{source.Name + ": " + err.Error()}}
	}
	var out Result
	if enabled(settings.IncludeSecurityAlerts, true) {
		if n := s.Counts["securityAlerts"]; n > 0 {
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.SecuritySeverity, "critical"), fmt.Sprintf("%d security alert(s)", n)})
		}
	}
	if enabled(settings.IncludeFailedRuns, true) {
		for _, r := range s.FailedRuns {
			repo := r.Repo
			if i := strings.IndexByte(repo, '/'); i >= 0 {
				repo = repo[i+1:]
			}
			msg := repo + " · " + r.Name + " failed"
			if r.Conclusion != "" && r.Conclusion != "failure" {
				msg += " (" + r.Conclusion + ")"
			}
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.FailedRunSeverity, "critical"), msg})
		}
	}
	if enabled(settings.IncludeReviewRequests, true) {
		if n := s.Counts["reviewRequests"]; n > 0 {
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.ReviewSeverity, "info"), fmt.Sprintf("%d pull request review request(s)", n)})
		}
	}
	if enabled(settings.IncludeAssignedIssues, true) {
		if n := s.Counts["assignedIssues"]; n > 0 {
			out.Alerts = append(out.Alerts, Alert{source.Name, severity(settings.AssignedIssueSeverity, "info"), fmt.Sprintf("%d assigned issue(s)", n)})
		}
	}
	if s.Error != "" {
		out.Errors = append(out.Errors, source.Name+" partial data: "+s.Error)
	}
	return out
}

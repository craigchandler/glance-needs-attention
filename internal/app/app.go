package app

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/craigchandler/glance-needs-attention/internal/adapters"
	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type State struct {
	Alerts      []adapters.Alert `json:"alerts"`
	RefreshedAt time.Time        `json:"refreshedAt"`
	Errors      []string         `json:"errors,omitempty"`
}

type App struct {
	cfg    cfgpkg.Config
	client *http.Client
	mu     sync.RWMutex
	state  State
}

func New(cfg cfgpkg.Config) *App {
	return &App{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.HTTPTimeoutDuration()},
	}
}

func (a *App) Start() {
	a.refresh()
	go func() {
		ticker := time.NewTicker(a.cfg.RefreshDuration())
		defer ticker.Stop()
		for range ticker.C {
			a.refresh()
		}
	}()
}

func (a *App) refresh() {
	st := State{}
	for _, source := range a.cfg.Sources {
		if !source.IsEnabled() {
			continue
		}
		adapter, err := adapters.ForType(source.Type)
		if err != nil {
			st.Errors = append(st.Errors, source.Name+": "+err.Error())
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTPTimeoutDuration())
		result := adapter.Check(ctx, a.client, source)
		cancel()
		st.Alerts = append(st.Alerts, result.Alerts...)
		st.Errors = append(st.Errors, result.Errors...)
	}
	sort.SliceStable(st.Alerts, func(i, j int) bool {
		rank := func(s string) int {
			switch s {
			case "critical":
				return 0
			case "warning":
				return 1
			default:
				return 2
			}
		}
		if rank(st.Alerts[i].Severity) != rank(st.Alerts[j].Severity) {
			return rank(st.Alerts[i].Severity) < rank(st.Alerts[j].Severity)
		}
		if st.Alerts[i].Source != st.Alerts[j].Source {
			return st.Alerts[i].Source < st.Alerts[j].Source
		}
		return st.Alerts[i].Message < st.Alerts[j].Message
	})
	st.RefreshedAt = time.Now()
	a.mu.Lock()
	a.state = st
	a.mu.Unlock()
}

func (a *App) snapshot() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

func (a *App) Health(w http.ResponseWriter, r *http.Request) {
	s := a.snapshot()
	w.Header().Set("Content-Type", "application/json")
	ok := !s.RefreshedAt.IsZero() && time.Since(s.RefreshedAt)/5 < a.cfg.RefreshDuration()
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          ok,
		"refreshedAt": s.RefreshedAt,
		"errors":      s.Errors,
	})
}

func (a *App) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.snapshot())
}

var wt = template.Must(template.New("w").Funcs(template.FuncMap{
	"class": func(s string) string {
		switch s {
		case "critical":
			return "color-negative"
		case "warning":
			return "color-highlight"
		default:
			return "color-primary"
		}
	},
	"mark": func(s string) string {
		switch s {
		case "critical":
			return "✗"
		case "warning":
			return "!"
		default:
			return "•"
		}
	},
}).Parse(`<div class="widget-content">{{if not .Alerts}}<div class="flex justify-between"><span>No issues requiring attention</span><span class="color-positive">✓</span></div>{{else}}{{range .Alerts}}<div class="flex gap-10 margin-top-5"><span class="{{class .Severity}}">{{mark .Severity}}</span><div><div class="size-h6 color-paragraph">{{.Source}}</div><div>{{.Message}}</div></div></div>{{end}}{{end}}{{if .Errors}}<details class="details margin-top-10"><summary class="summary color-highlight">Data source issues ({{len .Errors}})</summary><div class="margin-top-5">{{range .Errors}}<div class="color-paragraph size-h6">{{.}}</div>{{end}}</div></details>{{end}}</div>`))

func (a *App) Widget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Widget-Title", a.cfg.Title)
	w.Header().Set("Widget-Content-Type", "html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = wt.Execute(w, a.snapshot())
}

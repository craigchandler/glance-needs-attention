package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Title           string         `json:"title"`
	ListenAddr      string         `json:"listenAddr"`
	RefreshInterval string         `json:"refreshInterval"`
	HTTPTimeout     string         `json:"httpTimeout"`
	Sources         []SourceConfig `json:"sources"`
}

type SourceConfig struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	URL      string          `json:"url"`
	Enabled  *bool           `json:"enabled,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Title == "" {
		cfg.Title = "Needs Attention"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8795"
	}
	if cfg.RefreshInterval == "" {
		cfg.RefreshInterval = "1m"
	}
	if cfg.HTTPTimeout == "" {
		cfg.HTTPTimeout = "5s"
	}
	if d, err := time.ParseDuration(cfg.RefreshInterval); err != nil || d <= 0 {
		return Config{}, fmt.Errorf("refreshInterval must be a positive duration")
	}
	if d, err := time.ParseDuration(cfg.HTTPTimeout); err != nil || d <= 0 {
		return Config{}, fmt.Errorf("httpTimeout must be a positive duration")
	}
	for i, s := range cfg.Sources {
		if strings.TrimSpace(s.Name) == "" {
			return Config{}, fmt.Errorf("sources[%d].name is required", i)
		}
		if strings.TrimSpace(s.Type) == "" {
			return Config{}, fmt.Errorf("sources[%d].type is required", i)
		}
		if strings.TrimSpace(s.URL) == "" {
			return Config{}, fmt.Errorf("sources[%d].url is required", i)
		}
	}
	return cfg, nil
}

func (s SourceConfig) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

func (c Config) RefreshDuration() time.Duration {
	d, _ := time.ParseDuration(c.RefreshInterval)
	return d
}

func (c Config) HTTPTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.HTTPTimeout)
	return d
}

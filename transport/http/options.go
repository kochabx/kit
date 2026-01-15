package http

import "github.com/kochabx/kit/core/stag"

type Options struct {
	Swag    SwagOption
	Metrics MetricsOption
	Health  HealthOption
}

type SwagOption struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path" default:"/swagger/*any"`
}

func (s *SwagOption) init() error {
	return stag.ApplyDefaults(s)
}

type MetricsOption struct {
	Enabled                   bool   `json:"enabled"`
	Path                      string `json:"path" default:"/metrics"`
	EnabledGoCollector        bool   `json:"enabled_go_collector"`
	EnabledBuildInfoCollector bool   `json:"enabled_build_info_collector"`
}

func (m *MetricsOption) init() error {
	return stag.ApplyDefaults(m)
}

type HealthOption struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path" default:"/health"`
}

func (h *HealthOption) init() error {
	return stag.ApplyDefaults(h)
}

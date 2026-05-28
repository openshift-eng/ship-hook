package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

type Config struct {
	ShipHook ShipHookConfig       `json:"ship_hook,omitempty"`
	Plugins  []PluginConfig       `json:"plugins,omitempty"`
	Orgs     map[string]OrgConfig `json:"orgs,omitempty"`
}

type ShipHookConfig struct{}

type PluginConfig struct {
	Name     string          `json:"name"`
	Disabled *bool           `json:"disabled,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

type OrgConfig struct {
	Plugins []PluginConfig        `json:"plugins,omitempty"`
	Repos   map[string]RepoConfig `json:"repos,omitempty"`
}

type RepoConfig struct {
	Plugins []PluginConfig `json:"plugins,omitempty"`
}

func Load(configPath, supplementalDir string) (*Config, error) {
	cfg := &Config{}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
		}
	}

	if supplementalDir != "" {
		entries, err := os.ReadDir(supplementalDir)
		if err != nil {
			if os.IsNotExist(err) {
				return cfg, nil
			}
			return nil, fmt.Errorf("failed to read supplemental config dir %s: %w", supplementalDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, "..") {
				continue
			}
			ext := filepath.Ext(name)
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			path := filepath.Join(supplementalDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to read supplemental config %s: %w", path, err)
			}
			var supplemental Config
			if err := yaml.Unmarshal(data, &supplemental); err != nil {
				return nil, fmt.Errorf("failed to parse supplemental config %s: %w", path, err)
			}
			if err := cfg.MergeFrom(&supplemental); err != nil {
				return nil, fmt.Errorf("failed to merge supplemental config %s: %w", path, err)
			}
		}
	}

	return cfg, nil
}

func findPlugin(plugins []PluginConfig, name string) *PluginConfig {
	for i := range plugins {
		if plugins[i].Name == name {
			return &plugins[i]
		}
	}
	return nil
}

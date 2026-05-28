package config

import (
	"encoding/json"
	"sync/atomic"
)

type Resolver struct {
	cfg atomic.Pointer[Config]
}

func NewResolver(cfg *Config) *Resolver {
	r := &Resolver{}
	r.cfg.Store(cfg)
	return r
}

func (r *Resolver) UpdateConfig(cfg *Config) {
	r.cfg.Store(cfg)
}

func (r *Resolver) Config() *Config {
	return r.cfg.Load()
}

func (r *Resolver) IsEnabled(pluginName, org, repo string) bool {
	if r == nil {
		return false
	}
	cfg := r.cfg.Load()
	if cfg == nil {
		return false
	}

	found := false
	enabled := false

	if topLevel := findPlugin(cfg.Plugins, pluginName); topLevel != nil {
		found = true
		enabled = !isDisabled(topLevel)
	}

	if orgCfg, ok := cfg.Orgs[org]; ok {
		if orgPlugin := findPlugin(orgCfg.Plugins, pluginName); orgPlugin != nil {
			if !found {
				found = true
				enabled = !isDisabled(orgPlugin)
			} else if orgPlugin.Disabled != nil {
				enabled = !*orgPlugin.Disabled
			}
		}

		if repoCfg, ok := orgCfg.Repos[repo]; ok {
			if repoPlugin := findPlugin(repoCfg.Plugins, pluginName); repoPlugin != nil {
				if !found {
					found = true
					enabled = !isDisabled(repoPlugin)
				} else if repoPlugin.Disabled != nil {
					enabled = !*repoPlugin.Disabled
				}
			}
		}
	}

	return found && enabled
}

func (r *Resolver) rawConfigLayers(pluginName, org, repo string) []json.RawMessage {
	if r == nil {
		return nil
	}
	cfg := r.cfg.Load()
	if cfg == nil {
		return nil
	}

	var layers []json.RawMessage

	if topLevel := findPlugin(cfg.Plugins, pluginName); topLevel != nil && len(topLevel.Config) > 0 {
		layers = append(layers, topLevel.Config)
	}

	if orgCfg, ok := cfg.Orgs[org]; ok {
		if orgPlugin := findPlugin(orgCfg.Plugins, pluginName); orgPlugin != nil && len(orgPlugin.Config) > 0 {
			layers = append(layers, orgPlugin.Config)
		}

		if repoCfg, ok := orgCfg.Repos[repo]; ok {
			if repoPlugin := findPlugin(repoCfg.Plugins, pluginName); repoPlugin != nil && len(repoPlugin.Config) > 0 {
				layers = append(layers, repoPlugin.Config)
			}
		}
	}

	return layers
}

func ResolvePluginConfig[T any](r *Resolver, pluginName string, defaultCfg T, org, repo string) T {
	cfg := defaultCfg
	if r == nil {
		return cfg
	}
	for _, layer := range r.rawConfigLayers(pluginName, org, repo) {
		_ = json.Unmarshal(layer, &cfg)
	}
	return cfg
}

func isDisabled(p *PluginConfig) bool {
	return p.Disabled != nil && *p.Disabled
}

package config

import "encoding/json"

type Resolver struct {
	cfg *Config
}

func NewResolver(cfg *Config) *Resolver {
	return &Resolver{cfg: cfg}
}

func (r *Resolver) rawConfigLayers(pluginName, org, repo string) []json.RawMessage {
	if r == nil || r.cfg == nil {
		return nil
	}

	var layers []json.RawMessage

	if topLevel := findPlugin(r.cfg.Plugins, pluginName); topLevel != nil && len(topLevel.Config) > 0 {
		layers = append(layers, topLevel.Config)
	}

	if orgCfg, ok := r.cfg.Orgs[org]; ok {
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

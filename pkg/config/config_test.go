package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

func boolPtr(b bool) *bool { return &b }

func TestLoad(t *testing.T) {
	yamlContent := `
plugins:
  - name: test-plugin
    config:
      key: value
  - name: disabled-plugin
    disabled: true
orgs:
  test-org:
    plugins:
      - name: test-plugin
        config:
          key: org-value
    repos:
      test-repo:
        plugins:
          - name: test-plugin
            disabled: true
`
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(cfg.Plugins))
	}
	if cfg.Plugins[0].Name != "test-plugin" {
		t.Errorf("expected plugin name 'test-plugin', got %q", cfg.Plugins[0].Name)
	}
	if cfg.Plugins[0].Disabled != nil {
		t.Errorf("expected nil disabled for test-plugin")
	}
	if cfg.Plugins[1].Disabled == nil || !*cfg.Plugins[1].Disabled {
		t.Errorf("expected disabled-plugin to be disabled")
	}
	if len(cfg.Plugins[0].Config) == 0 {
		t.Errorf("expected config for test-plugin")
	}

	orgCfg, ok := cfg.Orgs["test-org"]
	if !ok {
		t.Fatal("expected test-org in orgs")
	}
	if len(orgCfg.Plugins) != 1 {
		t.Fatalf("expected 1 org plugin, got %d", len(orgCfg.Plugins))
	}

	repoCfg, ok := orgCfg.Repos["test-repo"]
	if !ok {
		t.Fatal("expected test-repo in repos")
	}
	if len(repoCfg.Plugins) != 1 {
		t.Fatalf("expected 1 repo plugin, got %d", len(repoCfg.Plugins))
	}
	if repoCfg.Plugins[0].Disabled == nil || !*repoCfg.Plugins[0].Disabled {
		t.Errorf("expected repo plugin to be disabled")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Plugins) != 0 {
		t.Errorf("expected no plugins, got %d", len(cfg.Plugins))
	}
}

func TestLoad_SupplementalDir(t *testing.T) {
	dir := t.TempDir()

	mainConfig := `
plugins:
  - name: plugin-a
`
	supplemental := `
plugins:
  - name: plugin-b
orgs:
  extra-org:
    plugins:
      - name: plugin-a
`
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(mainConfig), 0644); err != nil {
		t.Fatal(err)
	}

	suppDir := filepath.Join(dir, "supplemental")
	if err := os.Mkdir(suppDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(suppDir, "extra.yaml"), []byte(supplemental), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, suppDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(cfg.Plugins))
	}
	if _, ok := cfg.Orgs["extra-org"]; !ok {
		t.Error("expected extra-org from supplemental config")
	}
}

func TestLoad_SupplementalDirNonExistent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("plugins: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, filepath.Join(dir, "nonexistent"))
	if err != nil {
		t.Fatalf("Load should not fail for non-existent supplemental dir: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoad_SupplementalSkipsDotDot(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("plugins: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	suppDir := filepath.Join(dir, "supp")
	if err := os.Mkdir(suppDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(suppDir, "..data.yaml"), []byte("plugins:\n  - name: should-not-load\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, suppDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Plugins) != 0 {
		t.Errorf("expected no plugins (dotdot file should be skipped), got %d", len(cfg.Plugins))
	}
}

func TestMergeFrom(t *testing.T) {
	t.Run("non-overlapping plugins", func(t *testing.T) {
		base := &Config{Plugins: []PluginConfig{{Name: "a"}}}
		other := &Config{Plugins: []PluginConfig{{Name: "b"}}}
		if err := base.MergeFrom(other); err != nil {
			t.Fatalf("MergeFrom failed: %v", err)
		}
		if len(base.Plugins) != 2 {
			t.Errorf("expected 2 plugins, got %d", len(base.Plugins))
		}
	})

	t.Run("duplicate plugin at top level", func(t *testing.T) {
		base := &Config{Plugins: []PluginConfig{{Name: "a"}}}
		other := &Config{Plugins: []PluginConfig{{Name: "a"}}}
		if err := base.MergeFrom(other); err == nil {
			t.Error("expected error for duplicate plugin")
		}
	})

	t.Run("non-overlapping orgs", func(t *testing.T) {
		base := &Config{Orgs: map[string]OrgConfig{"org-a": {Plugins: []PluginConfig{{Name: "p"}}}}}
		other := &Config{Orgs: map[string]OrgConfig{"org-b": {Plugins: []PluginConfig{{Name: "p"}}}}}
		if err := base.MergeFrom(other); err != nil {
			t.Fatalf("MergeFrom failed: %v", err)
		}
		if len(base.Orgs) != 2 {
			t.Errorf("expected 2 orgs, got %d", len(base.Orgs))
		}
	})

	t.Run("same org, non-overlapping plugins", func(t *testing.T) {
		base := &Config{Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "a"}}}}}
		other := &Config{Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "b"}}}}}
		if err := base.MergeFrom(other); err != nil {
			t.Fatalf("MergeFrom failed: %v", err)
		}
		if len(base.Orgs["org"].Plugins) != 2 {
			t.Errorf("expected 2 org plugins, got %d", len(base.Orgs["org"].Plugins))
		}
	})

	t.Run("same org, duplicate plugin", func(t *testing.T) {
		base := &Config{Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "a"}}}}}
		other := &Config{Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "a"}}}}}
		if err := base.MergeFrom(other); err == nil {
			t.Error("expected error for duplicate org plugin")
		}
	})

	t.Run("same org, new repo", func(t *testing.T) {
		base := &Config{Orgs: map[string]OrgConfig{"org": {
			Repos: map[string]RepoConfig{"repo-a": {Plugins: []PluginConfig{{Name: "p"}}}},
		}}}
		other := &Config{Orgs: map[string]OrgConfig{"org": {
			Repos: map[string]RepoConfig{"repo-b": {Plugins: []PluginConfig{{Name: "p"}}}},
		}}}
		if err := base.MergeFrom(other); err != nil {
			t.Fatalf("MergeFrom failed: %v", err)
		}
		if len(base.Orgs["org"].Repos) != 2 {
			t.Errorf("expected 2 repos, got %d", len(base.Orgs["org"].Repos))
		}
	})

	t.Run("same org same repo, duplicate plugin", func(t *testing.T) {
		base := &Config{Orgs: map[string]OrgConfig{"org": {
			Repos: map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p"}}}},
		}}}
		other := &Config{Orgs: map[string]OrgConfig{"org": {
			Repos: map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p"}}}},
		}}}
		if err := base.MergeFrom(other); err == nil {
			t.Error("expected error for duplicate repo plugin")
		}
	})
}

func TestIsEnabled(t *testing.T) {
	testCases := []struct {
		name       string
		cfg        *Config
		pluginName string
		org, repo  string
		expected   bool
	}{
		{
			name:       "nil resolver",
			cfg:        nil,
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   false,
		},
		{
			name:       "not listed",
			cfg:        &Config{},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   false,
		},
		{
			name:       "listed at top level",
			cfg:        &Config{Plugins: []PluginConfig{{Name: "p"}}},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "listed at top, disabled at org",
			cfg: &Config{
				Plugins: []PluginConfig{{Name: "p"}},
				Orgs:    map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}}}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   false,
		},
		{
			name: "disabled at org, re-enabled at repo",
			cfg: &Config{
				Plugins: []PluginConfig{{Name: "p"}},
				Orgs: map[string]OrgConfig{"org": {
					Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}},
					Repos:   map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(false)}}}},
				}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "disabled at top level",
			cfg: &Config{
				Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   false,
		},
		{
			name: "disabled at top, enabled at org",
			cfg: &Config{
				Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}},
				Orgs:    map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(false)}}}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "different org not affected",
			cfg: &Config{
				Plugins: []PluginConfig{{Name: "p"}},
				Orgs:    map[string]OrgConfig{"other-org": {Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}}}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "listed only at org level",
			cfg: &Config{
				Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "p"}}}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "listed only at repo level",
			cfg: &Config{
				Orgs: map[string]OrgConfig{"org": {
					Repos: map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p"}}}},
				}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   true,
		},
		{
			name: "listed only at repo level, different repo not affected",
			cfg: &Config{
				Orgs: map[string]OrgConfig{"org": {
					Repos: map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p"}}}},
				}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "other-repo",
			expected:   false,
		},
		{
			name: "listed only at org level, different org not affected",
			cfg: &Config{
				Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{Name: "p"}}}},
			},
			pluginName: "p",
			org:        "other-org",
			repo:       "repo",
			expected:   false,
		},
		{
			name: "listed at org level, disabled at repo level",
			cfg: &Config{
				Orgs: map[string]OrgConfig{"org": {
					Plugins: []PluginConfig{{Name: "p"}},
					Repos:   map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}}}},
				}},
			},
			pluginName: "p",
			org:        "org",
			repo:       "repo",
			expected:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r *Resolver
			if tc.cfg != nil {
				r = NewResolver(tc.cfg)
			}
			got := r.IsEnabled(tc.pluginName, tc.org, tc.repo)
			if got != tc.expected {
				t.Errorf("IsEnabled(%q, %q, %q) = %v, want %v", tc.pluginName, tc.org, tc.repo, got, tc.expected)
			}
		})
	}
}

type testPluginConfig struct {
	Label    string `json:"label"`
	BotLogin string `json:"bot_login"`
	Count    int    `json:"count"`
}

func TestResolvePluginConfig(t *testing.T) {
	defaults := testPluginConfig{Label: "default-label", BotLogin: "default-bot", Count: 5}

	testCases := []struct {
		name      string
		cfg       *Config
		org, repo string
		expected  testPluginConfig
	}{
		{
			name:     "nil resolver returns defaults",
			cfg:      nil,
			org:      "org",
			repo:     "repo",
			expected: defaults,
		},
		{
			name:     "no config returns defaults",
			cfg:      &Config{Plugins: []PluginConfig{{Name: "p"}}},
			org:      "org",
			repo:     "repo",
			expected: defaults,
		},
		{
			name: "top-level overrides one field",
			cfg: &Config{Plugins: []PluginConfig{{
				Name:   "p",
				Config: json.RawMessage(`{"label":"custom-label"}`),
			}}},
			org:      "org",
			repo:     "repo",
			expected: testPluginConfig{Label: "custom-label", BotLogin: "default-bot", Count: 5},
		},
		{
			name: "org overrides on top of top-level",
			cfg: &Config{
				Plugins: []PluginConfig{{
					Name:   "p",
					Config: json.RawMessage(`{"label":"top-label","count":10}`),
				}},
				Orgs: map[string]OrgConfig{"org": {Plugins: []PluginConfig{{
					Name:   "p",
					Config: json.RawMessage(`{"label":"org-label"}`),
				}}}},
			},
			org:      "org",
			repo:     "repo",
			expected: testPluginConfig{Label: "org-label", BotLogin: "default-bot", Count: 10},
		},
		{
			name: "repo overrides on top of org",
			cfg: &Config{
				Plugins: []PluginConfig{{
					Name:   "p",
					Config: json.RawMessage(`{"label":"top-label"}`),
				}},
				Orgs: map[string]OrgConfig{"org": {
					Plugins: []PluginConfig{{
						Name:   "p",
						Config: json.RawMessage(`{"bot_login":"org-bot"}`),
					}},
					Repos: map[string]RepoConfig{"repo": {Plugins: []PluginConfig{{
						Name:   "p",
						Config: json.RawMessage(`{"label":"repo-label"}`),
					}}}},
				}},
			},
			org:      "org",
			repo:     "repo",
			expected: testPluginConfig{Label: "repo-label", BotLogin: "org-bot", Count: 5},
		},
		{
			name: "different org not affected",
			cfg: &Config{
				Plugins: []PluginConfig{{
					Name:   "p",
					Config: json.RawMessage(`{"label":"top-label"}`),
				}},
				Orgs: map[string]OrgConfig{"other-org": {Plugins: []PluginConfig{{
					Name:   "p",
					Config: json.RawMessage(`{"label":"other-org-label"}`),
				}}}},
			},
			org:      "org",
			repo:     "repo",
			expected: testPluginConfig{Label: "top-label", BotLogin: "default-bot", Count: 5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r *Resolver
			if tc.cfg != nil {
				r = NewResolver(tc.cfg)
			}
			got := ResolvePluginConfig(r, "p", defaults, tc.org, tc.repo)
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

func TestResolver_UpdateConfig(t *testing.T) {
	cfg1 := &Config{Plugins: []PluginConfig{{Name: "p"}}}
	r := NewResolver(cfg1)

	if !r.IsEnabled("p", "org", "repo") {
		t.Fatal("expected p to be enabled with initial config")
	}

	cfg2 := &Config{Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}}}
	r.UpdateConfig(cfg2)

	if r.IsEnabled("p", "org", "repo") {
		t.Fatal("expected p to be disabled after UpdateConfig")
	}

	if r.Config() != cfg2 {
		t.Fatal("Config() should return the updated config")
	}
}

func TestResolver_ConcurrentAccess(t *testing.T) {
	cfg1 := &Config{Plugins: []PluginConfig{{Name: "p"}}}
	cfg2 := &Config{Plugins: []PluginConfig{{Name: "p", Disabled: boolPtr(true)}}}
	r := NewResolver(cfg1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if i%2 == 0 {
				r.UpdateConfig(cfg2)
			} else {
				r.UpdateConfig(cfg1)
			}
		}
	}()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				r.IsEnabled("p", "org", "repo")
			}
		})
	}

	wg.Wait()
}

func TestYAMLRoundTrip(t *testing.T) {
	input := `plugins:
- name: test-plugin
  config:
    label: my-label
    bot_login: my-bot
- name: other-plugin
  disabled: true
orgs:
  my-org:
    plugins:
    - name: test-plugin
      config:
        label: org-label
    repos:
      my-repo:
        plugins:
        - name: test-plugin
          disabled: true
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(cfg.Plugins))
	}
	if cfg.Plugins[1].Disabled == nil || !*cfg.Plugins[1].Disabled {
		t.Error("expected other-plugin to be disabled")
	}

	var rawConfig struct {
		Label    string `json:"label"`
		BotLogin string `json:"bot_login"`
	}
	if err := json.Unmarshal(cfg.Plugins[0].Config, &rawConfig); err != nil {
		t.Fatalf("failed to unmarshal plugin config: %v", err)
	}
	if rawConfig.Label != "my-label" {
		t.Errorf("expected label 'my-label', got %q", rawConfig.Label)
	}
	if rawConfig.BotLogin != "my-bot" {
		t.Errorf("expected bot_login 'my-bot', got %q", rawConfig.BotLogin)
	}
}

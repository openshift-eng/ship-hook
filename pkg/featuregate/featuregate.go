package featuregate

import (
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
)

type DefaultState bool

const (
	DefaultOff DefaultState = false
	DefaultOn  DefaultState = true
)

type Spec struct {
	Name    string
	Default DefaultState
}

type Gates struct {
	specs     map[string]Spec
	envLookup func(string) (string, bool)
}

func New(envLookup func(string) (string, bool), specs ...Spec) *Gates {
	m := make(map[string]Spec, len(specs))
	for _, s := range specs {
		m[s.Name] = s
	}
	return &Gates{specs: m, envLookup: envLookup}
}

func (g *Gates) Enabled(name string) bool {
	spec, ok := g.specs[name]
	if !ok {
		return false
	}

	envVar := envVarName(name)
	if val, set := g.envLookup(envVar); set {
		switch strings.ToLower(val) {
		case "true", "1":
			return true
		case "false", "0":
			return false
		}
	}

	return bool(spec.Default)
}

func (g *Gates) LogStates(logger *logrus.Entry) {
	names := make([]string, 0, len(g.specs))
	for name := range g.specs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		spec := g.specs[name]
		envVar := envVarName(name)
		entry := logger.WithFields(logrus.Fields{
			"gate":    name,
			"default": bool(spec.Default),
			"envvar":  envVar,
			"enabled": g.Enabled(name),
		})
		if val, set := g.envLookup(envVar); set {
			entry = entry.WithField("envvar_value", val)
		}
		entry.Info("Feature gate state")
	}
}

func envVarName(gateName string) string {
	return "SHIP_HOOK_FEATURE_" + strings.ToUpper(strings.ReplaceAll(gateName, "-", "_"))
}

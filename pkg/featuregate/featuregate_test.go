package featuregate

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func fakeEnv(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func noEnv() func(string) (string, bool) {
	return fakeEnv(nil)
}

func TestEnabled(t *testing.T) {
	testCases := []struct {
		name     string
		spec     Spec
		env      map[string]string
		expected bool
	}{
		{
			name:     "default off, no envvar",
			spec:     Spec{Name: "my-gate", Default: DefaultOff},
			expected: false,
		},
		{
			name:     "default on, no envvar",
			spec:     Spec{Name: "my-gate", Default: DefaultOn},
			expected: true,
		},
		{
			name:     "default off, envvar true",
			spec:     Spec{Name: "my-gate", Default: DefaultOff},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "true"},
			expected: true,
		},
		{
			name:     "default off, envvar 1",
			spec:     Spec{Name: "my-gate", Default: DefaultOff},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "1"},
			expected: true,
		},
		{
			name:     "default on, envvar false",
			spec:     Spec{Name: "my-gate", Default: DefaultOn},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "false"},
			expected: false,
		},
		{
			name:     "default on, envvar 0",
			spec:     Spec{Name: "my-gate", Default: DefaultOn},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "0"},
			expected: false,
		},
		{
			name:     "default off, envvar unrecognized value",
			spec:     Spec{Name: "my-gate", Default: DefaultOff},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "maybe"},
			expected: false,
		},
		{
			name:     "default on, envvar unrecognized value",
			spec:     Spec{Name: "my-gate", Default: DefaultOn},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "whatever"},
			expected: true,
		},
		{
			name:     "envvar case insensitive",
			spec:     Spec{Name: "my-gate", Default: DefaultOff},
			env:      map[string]string{"SHIP_HOOK_FEATURE_MY_GATE": "TRUE"},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gates := New(fakeEnv(tc.env), tc.spec)
			got := gates.Enabled(tc.spec.Name)
			if got != tc.expected {
				t.Errorf("Enabled(%q) = %v, want %v", tc.spec.Name, got, tc.expected)
			}
		})
	}
}

func TestEnabledUnknownGate(t *testing.T) {
	gates := New(noEnv(), Spec{Name: "known", Default: DefaultOn})
	if gates.Enabled("unknown") {
		t.Error("Enabled(unknown gate) = true, want false")
	}
}

func TestEnvVarName(t *testing.T) {
	testCases := []struct {
		gate     string
		expected string
	}{
		{"my-feature", "SHIP_HOOK_FEATURE_MY_FEATURE"},
		{"auto-assign-reviewers", "SHIP_HOOK_FEATURE_AUTO_ASSIGN_REVIEWERS"},
		{"simple", "SHIP_HOOK_FEATURE_SIMPLE"},
	}

	for _, tc := range testCases {
		t.Run(tc.gate, func(t *testing.T) {
			got := envVarName(tc.gate)
			if got != tc.expected {
				t.Errorf("envVarName(%q) = %q, want %q", tc.gate, got, tc.expected)
			}
		})
	}
}

func TestLogStates(t *testing.T) {
	gates := New(
		fakeEnv(map[string]string{"SHIP_HOOK_FEATURE_GATE_B": "true"}),
		Spec{Name: "gate-a", Default: DefaultOff},
		Spec{Name: "gate-b", Default: DefaultOff},
	)
	gates.LogStates(logrus.WithField("test", true))
}

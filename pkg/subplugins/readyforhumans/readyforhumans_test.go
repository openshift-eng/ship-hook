package readyforhumans

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/config"
	"github.com/openshift-eng/ship-hook/pkg/featuregate"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

func nilResolver() *config.Resolver { return nil }

func resolverWithConfig(pluginName string, rawConfig string) *config.Resolver {
	return config.NewResolver(&config.Config{
		Plugins: []config.PluginConfig{{
			Name:   pluginName,
			Config: json.RawMessage(rawConfig),
		}},
	})
}

func gateDisabled() *featuregate.Gates {
	return featuregate.New(func(string) (string, bool) { return "", false })
}

func gateEnabled() *featuregate.Gates {
	return featuregate.New(
		func(key string) (string, bool) {
			if key == "SHIP_HOOK_FEATURE_CODERABBIT_AUTO_LABEL" {
				return "true", true
			}
			return "", false
		},
		featuregate.Spec{Name: "coderabbit-auto-label", Default: featuregate.DefaultOff},
	)
}

func allowAll() permissions.Checker {
	return permissions.NewStub(func(string) string { return "employee" })
}

func denyAll() permissions.Checker {
	return permissions.NewStub(nil)
}

func TestHandlePullRequestEvent(t *testing.T) {
	testCases := []struct {
		name        string
		gates       *featuregate.Gates
		perms       permissions.Checker
		resolver    *config.Resolver
		event       github.PullRequestEvent
		expectAdded []string
	}{
		{
			name:  "gate disabled, no label",
			gates: gateDisabled(),
			perms: denyAll(),
			event: testhelpers.NewPR("org", "repo", 1).WithAuthor("external").PullRequestEvent(github.PullRequestActionOpened),
		},
		{
			name:  "gate enabled, action not opened",
			gates: gateEnabled(),
			perms: denyAll(),
			event: testhelpers.NewPR("org", "repo", 1).WithAuthor("external").PullRequestEvent(github.PullRequestActionSynchronize),
		},
		{
			name:  "gate enabled, author has coderabbit access",
			gates: gateEnabled(),
			perms: allowAll(),
			event: testhelpers.NewPR("org", "repo", 42).WithAuthor("rh-employee").PullRequestEvent(github.PullRequestActionOpened),
		},
		{
			name:        "gate enabled, author without access, label added",
			gates:       gateEnabled(),
			perms:       denyAll(),
			event:       testhelpers.NewPR("org", "repo", 42).WithAuthor("external-contributor").PullRequestEvent(github.PullRequestActionOpened),
			expectAdded: []string{"org/repo#42:ready-for-human-review"},
		},
		{
			name:        "gate enabled, author without access, custom label from config",
			gates:       gateEnabled(),
			perms:       denyAll(),
			resolver:    resolverWithConfig(pluginName, `{"label":"custom-label"}`),
			event:       testhelpers.NewPR("org", "repo", 10).WithAuthor("external").PullRequestEvent(github.PullRequestActionOpened),
			expectAdded: []string{"org/repo#10:custom-label"},
		},
		{
			name:  "gate enabled, label already present",
			gates: gateEnabled(),
			perms: denyAll(),
			event: testhelpers.NewPR("org", "repo", 42).WithAuthor("external").WithLabel("ready-for-human-review").PullRequestEvent(github.PullRequestActionOpened),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			resolver := tc.resolver
			if resolver == nil {
				resolver = nilResolver()
			}
			p := New(fc, resolver, tc.gates, tc.perms)
			p.HandlePullRequestEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), tc.event)

			if diff := cmp.Diff(tc.expectAdded, fc.IssueLabelsAdded); diff != "" {
				t.Errorf("labels added mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHandleReviewEvent(t *testing.T) {
	testCases := []struct {
		name        string
		resolver    *config.Resolver
		event       github.ReviewEvent
		expectAdded []string
	}{
		{
			name:     "ignore review from different user",
			resolver: nilResolver(),
			event:    testhelpers.NewPR("org", "repo", 1).ReviewEvent(github.ReviewActionSubmitted, "someone-else", github.ReviewStateApproved),
		},
		{
			name:     "ignore edited action from coderabbit",
			resolver: nilResolver(),
			event:    testhelpers.NewPR("org", "repo", 1).ReviewEvent(github.ReviewActionEdited, "coderabbitai[bot]", github.ReviewStateApproved),
		},
		{
			name:     "ignore dismissed action from coderabbit",
			resolver: nilResolver(),
			event:    testhelpers.NewPR("org", "repo", 1).ReviewEvent(github.ReviewActionDismissed, "coderabbitai[bot]", github.ReviewStateApproved),
		},
		{
			name:     "ignore non-approved state",
			resolver: nilResolver(),
			event:    testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, "coderabbitai[bot]", github.ReviewStateChangesRequested),
		},
		{
			name:        "add label on approval when absent (default config)",
			resolver:    nilResolver(),
			event:       testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, "coderabbitai[bot]", github.ReviewStateApproved),
			expectAdded: []string{"org/repo#42:ready-for-human-review"},
		},
		{
			name:     "no-op on approval when label already present",
			resolver: nilResolver(),
			event:    testhelpers.NewPR("org", "repo", 42).WithLabel("ready-for-human-review").ReviewEvent(github.ReviewActionSubmitted, "coderabbitai[bot]", github.ReviewStateApproved),
		},
		{
			name:        "custom label from config",
			resolver:    resolverWithConfig(pluginName, `{"label":"custom-label"}`),
			event:       testhelpers.NewPR("org", "repo", 10).ReviewEvent(github.ReviewActionSubmitted, "coderabbitai[bot]", github.ReviewStateApproved),
			expectAdded: []string{"org/repo#10:custom-label"},
		},
		{
			name:        "custom bot_login from config",
			resolver:    resolverWithConfig(pluginName, `{"bot_login":"other-bot[bot]"}`),
			event:       testhelpers.NewPR("org", "repo", 10).ReviewEvent(github.ReviewActionSubmitted, "other-bot[bot]", github.ReviewStateApproved),
			expectAdded: []string{"org/repo#10:ready-for-human-review"},
		},
		{
			name:     "custom bot_login does not match default bot",
			resolver: resolverWithConfig(pluginName, `{"bot_login":"other-bot[bot]"}`),
			event:    testhelpers.NewPR("org", "repo", 10).ReviewEvent(github.ReviewActionSubmitted, "coderabbitai[bot]", github.ReviewStateApproved),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			p := New(fc, tc.resolver, gateDisabled(), denyAll())
			p.HandleReviewEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), tc.event)

			if diff := cmp.Diff(tc.expectAdded, fc.IssueLabelsAdded); diff != "" {
				t.Errorf("labels added mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

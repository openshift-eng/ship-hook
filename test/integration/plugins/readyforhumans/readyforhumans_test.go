//go:build integration

package readyforhumans

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/authz"
	"github.com/openshift-eng/ship-hook/pkg/featuregate"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/readyforhumans"
	integration "github.com/openshift-eng/ship-hook/test/integration"
)

func TestReadyForHumans_HandleReviewEvent(t *testing.T) {
	ghc := fakegithub.NewFakeClient()
	ghc.RepoLabelsExisting = []string{"ready-for-human-review"}

	gates := featuregate.New(func(string) (string, bool) { return "", false })
	plugin := readyforhumans.New(ghc, nil, gates, authz.NewAuthorizer(authz.NewPolicy(), nil))
	event := integration.LoadTestEvent[github.ReviewEvent](t, "review_submitted_approved.json")

	plugin.HandleReviewEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)

	if len(ghc.IssueLabelsAdded) != 1 {
		t.Fatalf("expected 1 label added, got %d", len(ghc.IssueLabelsAdded))
	}
	expected := "test-org/test-repo#1:ready-for-human-review"
	if ghc.IssueLabelsAdded[0] != expected {
		t.Errorf("expected label %q, got %q", expected, ghc.IssueLabelsAdded[0])
	}
}

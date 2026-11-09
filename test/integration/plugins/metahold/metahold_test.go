//go:build integration

package metahold

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/metahold"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
	integration "github.com/openshift-eng/ship-hook/test/integration"
)

func TestMetaHoldLifecycle(t *testing.T) {
	client := testhelpers.NewLabelClient(fakegithub.NewFakeClient())
	plugin := metahold.New(client)
	ctx := context.Background()
	log := logrus.NewEntry(logrus.StandardLogger())
	// Repeat the cycle to exercise adding and removing the same label again.
	for range 2 {
		for _, filename := range []string{"meta_hold_labeled.json", "meta_hold_unlabeled.json"} {
			event := integration.LoadTestEvent[github.PullRequestEvent](t, filename)
			client.RecordLabels(event.Repo.Owner.Login, event.Repo.Name, event.Number, event.PullRequest.Labels)
			result := plugin.HandlePullRequestEvent(ctx, log, event)
			if result.Outcome != dispatch.OutcomeActed || result.Err != nil {
				t.Fatalf("%s: expected label mutation, got %+v", filename, result)
			}
			want := []github.Label{{Name: "bug"}}
			if event.Action == github.PullRequestActionLabeled {
				want = append(want, github.Label{Name: "do-not-merge/hold"}, github.Label{Name: "do-not-merge/meta"})
			}
			labels, err := client.GetIssueLabels(event.Repo.Owner.Login, event.Repo.Name, event.Number)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, labels); diff != "" {
				t.Fatalf("%s: labels mismatch (-want +got):\n%s", filename, diff)
			}
			result = plugin.HandlePullRequestEvent(ctx, log, event)
			if result.Outcome != dispatch.OutcomeNoChange || result.Err != nil {
				t.Fatalf("%s: repeated event changed labels: %+v", filename, result)
			}
		}
	}
	if diff := cmp.Diff([]string{"test-org/test-repo#1:do-not-merge/meta"}, client.IssueLabelsAdded); diff != "" {
		t.Errorf("labels added mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"test-org/test-repo#1:do-not-merge/meta"}, client.IssueLabelsRemoved); diff != "" {
		t.Errorf("labels removed mismatch (-want +got):\n%s", diff)
	}
}

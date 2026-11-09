package testhelpers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"
)

// TestLabelSnapshots verifies isolation between PRs and repositories, snapshot
// ownership, case-insensitive identities, and clearing state between dev runs.
func TestLabelSnapshots(t *testing.T) {
	client := NewLabelClient(fakegithub.NewFakeClient())
	snapshot := []github.Label{{Name: "Do-Not-Merge/Meta"}, {Name: "bug"}}
	client.RecordLabels("org", "repo", 42, snapshot)
	snapshot[0].Name = "changed"
	want := []github.Label{{Name: "Do-Not-Merge/Meta"}, {Name: "bug"}}
	got, err := client.GetIssueLabels("ORG", "REPO", 42)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("snapshot mismatch (-want +got):\n%s", diff)
	}
	got[0].Name = "also changed"
	for _, tc := range []struct {
		org, repo string
		number    int
	}{
		{"other", "repo", 42}, {"org", "other", 42}, {"org", "repo", 43},
	} {
		labels, err := client.GetIssueLabels(tc.org, tc.repo, tc.number)
		if err != nil || len(labels) != 0 {
			t.Fatalf("labels leaked into %s/%s#%d: %v, %v", tc.org, tc.repo, tc.number, labels, err)
		}
	}
	if err := client.RemoveLabel("org", "repo", 42, "do-not-merge/meta"); err != nil {
		t.Fatal(err)
	}
	got, err = client.GetIssueLabels("org", "repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]github.Label{{Name: "bug"}}, got); diff != "" {
		t.Fatalf("removal mismatch (-want +got):\n%s", diff)
	}
	client.ResetLabels()
	got, err = client.GetIssueLabels("org", "repo", 42)
	if err != nil || len(got) != 0 {
		t.Fatalf("reset retained labels: %v, %v", got, err)
	}
}

package metahold

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

func TestHandlePullRequestEvent(t *testing.T) {
	testCases := []struct {
		name        string
		labels      []string
		wantAdded   []string
		wantRemoved []string
	}{
		{name: "no labels"},
		{name: "unrelated labels", labels: []string{"bug", "hold", "do-not-merge", "other/do-not-merge/hold", "do-not-merge-other/hold"}},
		{
			name:      "hold without meta",
			labels:    []string{"do-not-merge/hold"},
			wantAdded: []string{"org/repo#42:do-not-merge/meta"},
		},
		{
			name:      "multiple holds without meta",
			labels:    []string{"do-not-merge/hold", "do-not-merge/work-in-progress", "bug"},
			wantAdded: []string{"org/repo#42:do-not-merge/meta"},
		},
		{name: "hold with meta", labels: []string{"do-not-merge/hold", "do-not-merge/meta"}},
		{name: "remaining hold keeps meta", labels: []string{"do-not-merge/meta", "do-not-merge/work-in-progress"}},
		{
			name:        "meta alone",
			labels:      []string{"do-not-merge/meta"},
			wantRemoved: []string{"org/repo#42:do-not-merge/meta"},
		},
		{
			name:        "meta with unrelated labels",
			labels:      []string{"bug", "do-not-merge/meta", "do-not-merge"},
			wantRemoved: []string{"org/repo#42:do-not-merge/meta"},
		},
		{
			name:      "label extending meta is a separate hold",
			labels:    []string{"do-not-merge/meta-other"},
			wantAdded: []string{"org/repo#42:do-not-merge/meta"},
		},
		{
			name:      "mixed case hold",
			labels:    []string{"Do-Not-Merge/Hold"},
			wantAdded: []string{"org/repo#42:do-not-merge/meta"},
		},
		{name: "mixed case meta with hold", labels: []string{"DO-NOT-MERGE/META", "do-not-merge/hold"}},
		{
			name:        "mixed case meta alone",
			labels:      []string{"DO-NOT-MERGE/META"},
			wantRemoved: []string{"org/repo#42:do-not-merge/meta"},
		},
	}

	for _, action := range []github.PullRequestEventAction{
		github.PullRequestActionOpened,
		github.PullRequestActionReopened,
		github.PullRequestActionLabeled,
		github.PullRequestActionUnlabeled,
	} {
		t.Run(string(action), func(t *testing.T) {
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					ghc := fakegithub.NewFakeClient()
					pr := testhelpers.NewPR("org", "repo", 42)
					for _, label := range tc.labels {
						ghc.IssueLabelsExisting = append(ghc.IssueLabelsExisting, "org/repo#42:"+label)
						pr.WithLabel(label)
					}
					plugin := New(ghc)
					result := plugin.HandlePullRequestEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), pr.PullRequestEvent(action))
					wantOutcome := dispatch.OutcomeNoChange
					if len(tc.wantAdded)+len(tc.wantRemoved) > 0 {
						wantOutcome = dispatch.OutcomeActed
					}
					if result.Outcome != wantOutcome || result.Err != nil || result.Reason == "" {
						t.Errorf("got result %+v, want %s with a reason and no error", result, wantOutcome)
					}
					if diff := cmp.Diff(tc.wantAdded, ghc.IssueLabelsAdded); diff != "" {
						t.Errorf("labels added mismatch (-want +got):\n%s", diff)
					}
					if diff := cmp.Diff(tc.wantRemoved, ghc.IssueLabelsRemoved); diff != "" {
						t.Errorf("labels removed mismatch (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

func TestReconcileCurrentLabels(t *testing.T) {
	for _, tc := range []struct {
		name         string
		currentLabel string
		payloadLabel string
	}{
		{name: "new hold absent from payload", currentLabel: "do-not-merge/hold", payloadLabel: "do-not-merge/meta"},
		{name: "removed hold still in payload", currentLabel: "do-not-merge/meta", payloadLabel: "do-not-merge/hold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ghc := fakegithub.NewFakeClient()
			ghc.IssueLabelsExisting = []string{"org/repo#42:" + tc.currentLabel, "other/repo#1:do-not-merge/hold"}
			plugin := New(ghc)
			event := testhelpers.NewPR("org", "repo", 42).WithLabel(tc.payloadLabel).PullRequestEvent(github.PullRequestActionUnlabeled)
			logger := logrus.NewEntry(logrus.StandardLogger())
			result := plugin.HandlePullRequestEvent(context.Background(), logger, event)
			if result.Outcome != dispatch.OutcomeActed || result.Err != nil {
				t.Fatalf("expected a successful mutation, got %+v", result)
			}
			// Replaying the same stale webhook must not repeat or undo the mutation.
			result = plugin.HandlePullRequestEvent(context.Background(), logger, event)
			if result.Outcome != dispatch.OutcomeNoChange || result.Err != nil {
				t.Errorf("expected no change on replay, got %+v", result)
			}
			if len(ghc.IssueLabelsAdded)+len(ghc.IssueLabelsRemoved) != 1 {
				t.Errorf("expected exactly one mutation, got added=%v removed=%v", ghc.IssueLabelsAdded, ghc.IssueLabelsRemoved)
			}
		})
	}
}

type failingClient struct {
	*fakegithub.FakeClient
	getErr    error
	addErr    error
	removeErr error
}

func (c *failingClient) GetIssueLabels(org, repo string, number int) ([]github.Label, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	return c.FakeClient.GetIssueLabels(org, repo, number)
}

func (c *failingClient) AddLabel(org, repo string, number int, label string) error {
	return c.addErr
}

func (c *failingClient) RemoveLabel(org, repo string, number int, label string) error {
	return c.removeErr
}

func TestGitHubErrors(t *testing.T) {
	wantErr := errors.New("GitHub unavailable")
	for _, operation := range []string{"get", "add", "remove"} {
		t.Run(operation, func(t *testing.T) {
			ghc := &failingClient{FakeClient: fakegithub.NewFakeClient()}
			switch operation {
			case "get":
				ghc.getErr = wantErr
			case "add":
				ghc.IssueLabelsExisting = []string{"org/repo#42:do-not-merge/hold"}
				ghc.addErr = wantErr
			case "remove":
				ghc.IssueLabelsExisting = []string{"org/repo#42:do-not-merge/meta"}
				ghc.removeErr = wantErr
			}
			plugin := New(ghc)
			event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionLabeled)
			result := plugin.HandlePullRequestEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)
			if result.Outcome != dispatch.OutcomeErrored || !errors.Is(result.Err, wantErr) || result.Reason == "" {
				t.Errorf("expected errored result with the GitHub error and a reason, got %+v", result)
			}
			if len(ghc.IssueLabelsAdded)+len(ghc.IssueLabelsRemoved) != 0 {
				t.Error("unexpected label mutation after failure")
			}
		})
	}
}

func TestIgnoredEvents(t *testing.T) {
	// A nil client ensures ignored events do not make GitHub API calls.
	plugin := New(nil)
	logger := logrus.NewEntry(logrus.StandardLogger())
	ctx := context.Background()
	if plugin.Name() != "meta-hold" {
		t.Fatalf("unexpected plugin name: %s", plugin.Name())
	}
	for _, action := range []github.PullRequestEventAction{github.PullRequestActionSynchronize, github.PullRequestActionEdited, github.PullRequestActionClosed} {
		event := testhelpers.NewPR("org", "repo", 42).WithLabel("do-not-merge/hold").PullRequestEvent(action)
		if result := plugin.HandlePullRequestEvent(ctx, logger, event); result.Outcome != dispatch.OutcomeFiltered {
			t.Errorf("expected filtered result for %s, got %+v", action, result)
		}
	}
	pr := testhelpers.NewPR("org", "repo", 42)
	if result := plugin.HandleIssueCommentEvent(ctx, logger, pr.CommentEvent("/hold")); result.Outcome != dispatch.OutcomeSkipped {
		t.Errorf("expected skipped issue comment, got %+v", result)
	}
	if result := plugin.HandleReviewEvent(ctx, logger, pr.ReviewEvent(github.ReviewActionSubmitted, "reviewer", github.ReviewStateApproved)); result.Outcome != dispatch.OutcomeSkipped {
		t.Errorf("expected skipped review, got %+v", result)
	}
}

type concurrentLabelClient struct {
	*fakegithub.FakeClient
	reads    atomic.Int32
	mutating chan struct{}
	resume   chan struct{}
}

func (c *concurrentLabelClient) GetIssueLabels(org, repo string, number int) ([]github.Label, error) {
	c.reads.Add(1)
	return c.FakeClient.GetIssueLabels(org, repo, number)
}

func (c *concurrentLabelClient) AddLabel(org, repo string, number int, label string) error {
	close(c.mutating)
	<-c.resume
	return c.FakeClient.AddLabel(org, repo, number, label)
}

func (c *concurrentLabelClient) RemoveLabel(org, repo string, number int, label string) error {
	close(c.mutating)
	<-c.resume
	return c.FakeClient.RemoveLabel(org, repo, number, label)
}

type observedKeyMutex struct {
	keymutex.KeyMutex
	attempts atomic.Int32
	second   chan struct{}
}

func (m *observedKeyMutex) LockKey(key string) {
	if m.attempts.Add(1) == 2 {
		close(m.second)
	}
	m.KeyMutex.LockKey(key)
}

// TestConcurrentReconciliation holds the first mutation open and verifies that
// a second delivery waits before reading labels, then observes the new state.
func TestConcurrentReconciliation(t *testing.T) {
	for _, label := range []string{"do-not-merge/hold", "do-not-merge/meta"} {
		t.Run(label, func(t *testing.T) {
			client := &concurrentLabelClient{
				FakeClient: fakegithub.NewFakeClient(),
				mutating:   make(chan struct{}), resume: make(chan struct{}),
			}
			client.IssueLabelsExisting = []string{"org/repo#42:" + label}
			release := sync.OnceFunc(func() { close(client.resume) })
			defer release()
			plugin := New(client)
			locks := &observedKeyMutex{KeyMutex: plugin.reconciliations, second: make(chan struct{})}
			plugin.reconciliations = locks
			event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionLabeled)
			results := make(chan dispatch.HandlerResult, 2)
			run := func() {
				results <- plugin.HandlePullRequestEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)
			}
			go run()
			select {
			case <-client.mutating:
			case <-time.After(5 * time.Second):
				t.Fatal("first handler did not reach mutation")
			}
			go run()
			select {
			case <-locks.second:
			case <-time.After(5 * time.Second):
				t.Fatal("second handler did not coordinate reconciliation")
			}
			if client.reads.Load() != 1 {
				t.Fatal("second handler read labels before the first mutation completed")
			}
			release()
			outcomes := make(map[dispatch.Outcome]int)
			for range 2 {
				select {
				case result := <-results:
					if result.Err != nil {
						t.Fatalf("reconciliation failed: %+v", result)
					}
					outcomes[result.Outcome]++
				case <-time.After(5 * time.Second):
					t.Fatal("reconciliation did not finish")
				}
			}
			if outcomes[dispatch.OutcomeActed] != 1 || outcomes[dispatch.OutcomeNoChange] != 1 {
				t.Fatalf("expected one mutation and one no-op, got %v", outcomes)
			}
		})
	}
}

package example

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

func TestHandlePullRequestEvent(t *testing.T) {
	ghc := fakegithub.NewFakeClient()
	plugin := New(ghc)

	event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionOpened)
	plugin.HandlePullRequestEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)

	if len(ghc.IssueCommentsAdded) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(ghc.IssueCommentsAdded))
	}
	expected := "org/repo#42:example plugin noticed PR #42"
	if ghc.IssueCommentsAdded[0] != expected {
		t.Errorf("expected comment %q, got %q", expected, ghc.IssueCommentsAdded[0])
	}
}

func TestHandleIssueCommentEvent(t *testing.T) {
	ghc := fakegithub.NewFakeClient()
	plugin := New(ghc)

	event := testhelpers.NewPR("org", "repo", 1).CommentEvent("/example")
	plugin.HandleIssueCommentEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)

	if len(ghc.IssueCommentsAdded) != 0 {
		t.Errorf("expected no comments, got %d", len(ghc.IssueCommentsAdded))
	}
}

func TestHandleReviewEvent(t *testing.T) {
	ghc := fakegithub.NewFakeClient()
	plugin := New(ghc)

	event := testhelpers.NewPR("org", "repo", 1).ReviewEvent(github.ReviewActionSubmitted, "reviewer", github.ReviewStateApproved)
	plugin.HandleReviewEvent(context.Background(), logrus.NewEntry(logrus.StandardLogger()), event)
}

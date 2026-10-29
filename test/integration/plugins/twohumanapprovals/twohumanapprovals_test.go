//go:build integration

package twohumanapprovals

import (
	"context"
	"testing"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/twohumanapprovals"
	integration "github.com/openshift-eng/ship-hook/test/integration"
)

func TestTwoHumanApprovals(t *testing.T) {
	ctx := context.Background()
	service := orgdatacore.NewService()
	if err := service.LoadFromDataSource(ctx, orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())); err != nil {
		t.Fatal(err)
	}
	perms := permissions.NewStub(func(login string) string {
		if employee := service.GetEmployeeByGitHubID(login); employee != nil {
			return employee.UID
		}
		return ""
	})
	log := logrus.NewEntry(logrus.StandardLogger())

	for _, earlier := range []string{"attribution", "comment", "review"} {
		for _, trigger := range []string{"comment", "review"} {
			t.Run(earlier+"_then_"+trigger, func(t *testing.T) {
				fc := fakegithub.NewFakeClient()
				plugin := twohumanapprovals.New(fc, perms)
				opened := integration.LoadTestEvent[github.PullRequestEvent](t, "pull_request_opened.json")
				opened.PullRequest.User.Login = "redhat-chai-bot"
				result := plugin.HandlePullRequestEvent(ctx, log, opened)
				if result.Outcome != dispatch.OutcomeActed || result.Err != nil || len(fc.IssueLabelsAdded) != 1 || fc.IssueLabelsAdded[0] != "test-org/test-repo#1:needs-human-review" {
					t.Fatalf("expected bot PR to be labeled, got %+v, labels %v", result, fc.IssueLabelsAdded)
				}

				// Subsequent webhook payloads reflect the label added at PR creation.
				opened.PullRequest.Labels = []github.Label{{Name: "needs-human-review"}}
				switch earlier {
				case "attribution":
					opened.PullRequest.Body = "## Summary\nBot changes\n\n@ghuser1 requested from Slack\n"
				case "comment":
					comment := integration.LoadTestEvent[github.IssueCommentEvent](t, "issue_comment_created.json").Comment
					comment.User.Login = "ghuser1"
					comment.Body = "/ok-to-test"
					fc.IssueComments[1] = []github.IssueComment{comment}
				case "review":
					review := integration.LoadTestEvent[github.ReviewEvent](t, "review_submitted_approved.json").Review
					review.User = github.User{Login: "ghuser1", Type: github.UserTypeUser}
					fc.Reviews[1] = []github.Review{review}
				}

				comment := integration.LoadTestEvent[github.IssueCommentEvent](t, "issue_comment_created.json")
				comment.Issue.Labels = opened.PullRequest.Labels
				comment.Issue.Body = opened.PullRequest.Body
				comment.Comment.Body = "/approve"
				comment.Comment.User.Login = "ghuser1"
				result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				if result.Outcome != dispatch.OutcomeNoChange || len(fc.IssueLabelsRemoved) != 0 {
					t.Fatalf("one employee must not count twice: %+v", result)
				}

				comment.Comment.User.Login = "outsider"
				result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				if result.Outcome != dispatch.OutcomeFiltered || len(fc.IssueLabelsRemoved) != 0 {
					t.Fatalf("outsider must not count as employee: %+v", result)
				}

				if trigger == "comment" {
					comment.Comment.User.Login = "ghuser2"
					result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				} else {
					review := integration.LoadTestEvent[github.ReviewEvent](t, "review_submitted_approved.json")
					review.PullRequest = opened.PullRequest
					review.Review.User = github.User{Login: "ghuser2", Type: github.UserTypeUser}
					result = plugin.HandleReviewEvent(ctx, log, review)
				}
				if result.Outcome != dispatch.OutcomeActed || result.Err != nil || len(fc.IssueLabelsRemoved) != 1 || fc.IssueLabelsRemoved[0] != "test-org/test-repo#1:needs-human-review" {
					t.Fatalf("expected second employee to clear label, got %+v, labels %v", result, fc.IssueLabelsRemoved)
				}
			})
		}
	}
}

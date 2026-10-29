//go:build integration

package twohumanapprovals

import (
	"context"
	"encoding/json"
	"testing"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/cyborg"
	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/twohumanapprovals"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
	integration "github.com/openshift-eng/ship-hook/test/integration"
)

func TestTwoHumanApprovals(t *testing.T) {
	ctx := context.Background()
	data := orgdatacore.CreateTestData()
	// Eligibility uses Cyborg membership; distinctness uses only GitHub logins.
	data.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{
		"GhUser1": "testuser1", "GHUser2": "testuser1",
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	service, err := cyborg.NewService(ctx, orgdatacore.NewFakeDataSource(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	perms := permissions.NewStub(map[permissions.Operation]func(string) bool{
		{Action: "approve", Resource: "pr"}: func(login string) bool {
			return service.GetEmployeeByGitHubID(login) != nil
		},
	})
	log := logrus.NewEntry(logrus.StandardLogger())

	for _, earlier := range []string{"attribution", "comment", "review"} {
		for _, trigger := range []string{"comment", "review"} {
			t.Run(earlier+"_then_"+trigger, func(t *testing.T) {
				fc := testhelpers.NewCheckRunClient()
				plugin := twohumanapprovals.New(fc, perms)
				opened := integration.LoadTestEvent[github.PullRequestEvent](t, "pull_request_opened.json")
				opened.PullRequest.User.Login = "redhat-chai-bot"
				result := plugin.HandlePullRequestEvent(ctx, log, opened)
				if result.Outcome != dispatch.OutcomeActed || result.Err != nil || len(fc.CheckRuns()) != 1 || fc.CheckRuns()[0].Status != "in_progress" {
					t.Fatalf("expected waiting check, got %+v, checks %v", result, fc.CheckRuns())
				}

				switch earlier {
				case "attribution":
					opened.PullRequest.Body = "## Summary\nBot changes\n\n@GHUSER1 requested from Slack\n"
				case "comment":
					comment := integration.LoadTestEvent[github.IssueCommentEvent](t, "issue_comment_created.json").Comment
					comment.User.Login = "gHUser1"
					comment.Body = "/ok-to-test"
					fc.IssueComments[1] = []github.IssueComment{comment}
				case "review":
					review := integration.LoadTestEvent[github.ReviewEvent](t, "review_submitted_approved.json").Review
					review.User = github.User{Login: "GhUser1", Type: github.UserTypeUser}
					fc.Reviews[1] = []github.Review{review}
				}

				comment := integration.LoadTestEvent[github.IssueCommentEvent](t, "issue_comment_created.json")
				fc.RecordPullRequest(opened.PullRequest)
				comment.Issue.User.Login = "redhat-chai-bot"
				comment.Issue.Body = opened.PullRequest.Body
				comment.Comment.Body = "/approve"
				comment.Comment.User.Login = "GHUSER1"
				result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				if result.Outcome != dispatch.OutcomeNoChange || fc.CheckRuns()[0].Conclusion != "" {
					t.Fatalf("one GitHub account must not count twice: %+v", result)
				}

				comment.Comment.User.Login = "outsider"
				result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				if result.Outcome != dispatch.OutcomeFiltered || fc.CheckRuns()[0].Conclusion != "" {
					t.Fatalf("outsider must not count as employee: %+v", result)
				}

				if trigger == "comment" {
					comment.Comment.User.Login = "GHUSER2"
					result = plugin.HandleIssueCommentEvent(ctx, log, comment)
				} else {
					review := integration.LoadTestEvent[github.ReviewEvent](t, "review_submitted_approved.json")
					review.PullRequest = opened.PullRequest
					review.Review.User = github.User{Login: "gHuSeR2", Type: github.UserTypeUser}
					result = plugin.HandleReviewEvent(ctx, log, review)
				}
				if result.Outcome != dispatch.OutcomeActed || result.Err != nil || len(fc.CheckRuns()) != 1 || fc.CheckRuns()[0].Status != "completed" || fc.CheckRuns()[0].Conclusion != "success" {
					t.Fatalf("expected passing check, got %+v, checks %v", result, fc.CheckRuns())
				}

				// A new commit is checked against the full prior participation.
				fc.IssueComments[1] = append(fc.IssueComments[1], github.IssueComment{
					User: github.User{Login: "GHUSER2"}, Body: "/verify all",
				})
				opened.Action = github.PullRequestActionSynchronize
				opened.PullRequest.Head.SHA = "next-revision"
				result = plugin.HandlePullRequestEvent(ctx, log, opened)
				checks := fc.CheckRuns()
				if result.Err != nil || result.Outcome != dispatch.OutcomeActed || len(checks) != 2 || checks[1].HeadSHA != "next-revision" || checks[1].Conclusion != "success" {
					t.Fatalf("expected new revision to pass using earlier history: %+v, checks %+v", result, checks)
				}
			})
		}
	}
}

package twohumanapprovals

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

// approvalPermissions supplies a policy independently of any employee directory.
func approvalPermissions() permissions.IdentityChecker {
	return testPermissions{"alice": "person-1", "alice-alt": "person-1", "bob": "person-2"}
}

type testPermissions map[string]string

func (p testPermissions) IsAllowed(user, action, resource string) bool {
	return action == "approve" && resource == "pr" && p[strings.ToLower(user)] != ""
}

func (p testPermissions) SameIdentity(user, other string) bool {
	id := p[strings.ToLower(user)]
	return id != "" && id == p[strings.ToLower(other)]
}

func logger() *logrus.Entry { return logrus.NewEntry(logrus.StandardLogger()) }

func commentEvent(body string) github.IssueCommentEvent {
	event := testhelpers.NewPR("org", "repo", 42).WithAuthor("bob").CommentEvent(body)
	event.Issue.PullRequest = &struct{}{}
	event.Issue.Labels = []github.Label{{Name: label}}
	return event
}

func historyComment(login, body string) github.IssueComment {
	return github.IssueComment{User: github.User{Login: login}, Body: body}
}

func historyReview(login string, state github.ReviewState, body string) github.Review {
	return github.Review{ID: 10, User: github.User{Login: login}, State: state, Body: body}
}

func assertResult(t *testing.T, result dispatch.HandlerResult, outcome dispatch.Outcome, removed []string, fc *fakegithub.FakeClient) {
	t.Helper()
	if result.Outcome != outcome || result.Err != nil || result.Reason == "" {
		t.Fatalf("unexpected handler result: %+v, want outcome %s", result, outcome)
	}
	if diff := cmp.Diff(removed, fc.IssueLabelsRemoved); diff != "" {
		t.Errorf("labels removed mismatch (-want +got):\n%s", diff)
	}
}

func TestHandlePullRequestEvent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		author  string
		action  github.PullRequestEventAction
		labeled bool
		outcome dispatch.Outcome
	}{
		{name: "designated account", author: botLogin, action: github.PullRequestActionOpened, outcome: dispatch.OutcomeActed},
		{name: "case insensitive login", author: "RedHat-Chai-Bot", action: github.PullRequestActionOpened, outcome: dispatch.OutcomeActed},
		{name: "ordinary human", author: "alice", action: github.PullRequestActionOpened, outcome: dispatch.OutcomeFiltered},
		{name: "other bot", author: "other[bot]", action: github.PullRequestActionOpened, outcome: dispatch.OutcomeFiltered},
		{name: "already labeled", author: botLogin, action: github.PullRequestActionOpened, labeled: true, outcome: dispatch.OutcomeNoChange},
		{name: "new commits do not restore label", author: botLogin, action: github.PullRequestActionSynchronize, outcome: dispatch.OutcomeFiltered},
		{name: "reopened does not restore label", author: botLogin, action: github.PullRequestActionReopened, outcome: dispatch.OutcomeFiltered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			builder := testhelpers.NewPR("org", "repo", 42).WithAuthor(tc.author)
			if tc.labeled {
				builder.WithLabel(label)
			}
			plugin := New(fc, nil)
			result := plugin.HandlePullRequestEvent(context.Background(), logger(), builder.PullRequestEvent(tc.action))
			assertResult(t, result, tc.outcome, nil, fc)
			var want []string
			if tc.outcome == dispatch.OutcomeActed {
				want = []string{"org/repo#42:" + label}
			}
			if diff := cmp.Diff(want, fc.IssueLabelsAdded); diff != "" {
				t.Errorf("labels added mismatch (-want +got):\n%s", diff)
			}
			if plugin.Name() != pluginName {
				t.Errorf("unexpected plugin name %q", plugin.Name())
			}
		})
	}
}

func TestCommentApprovalCommands(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"/ok-to-test", true},
		{"/lgtm", true},
		{"/approve", true},
		{"/approve no-issue", true},
		{"/lgtm no-issue", true},
		{"/verify", true},
		{"/verify all", true},
		{"/verify cancel", true},
		{"/VeRiFy something", true},
		{"Looks good!\n/LGTM\t\r\nThanks", true},
		{"/lgtm cancel\n/approve", true},
		{"/approve\n/approve cancel", true},
		{"/ok-to-test cancel", false},
		{"/lgtm cancel", false},
		{"/approve cancel", false},
		{"/APPROVE\tCANCEL\r\n", false},
		{"/lgtm cancel\n/approve cancel", false},
		{"Please /approve", false},
		{"> /lgtm", false},
		{"`/lgtm`", false},
		{"/approve-cancel", false},
		{"/verified", false},
		{"/verify-all", false},
		{"LGTM", false},
		{"", false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			event := commentEvent(tc.body)
			event.Issue.Body = "@alice requested from Slack"
			result := New(fc, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event)
			outcome := dispatch.OutcomeFiltered
			var removed []string
			if tc.want {
				outcome = dispatch.OutcomeActed
				removed = []string{"org/repo#42:" + label}
			}
			assertResult(t, result, outcome, removed, fc)
		})
	}
}

func TestCommentHistory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		comments []github.IssueComment
		reviews  []github.Review
		events   []github.ListedIssueEvent
		want     bool
	}{
		{name: "requester in description", body: "## Summary\nChanges\n\n@alice requested from Slack\n", want: true},
		{name: "requester with CRLF", body: "@Alice requested from Slack\r\n", want: true},
		{name: "same requester as actor", body: "@bob requested from Slack"},
		{name: "unknown requester", body: "@outsider requested from Slack"},
		{name: "quoted attribution ignored", body: "> @alice requested from Slack"},
		{name: "incidental mention ignored", body: "Thanks @alice requested from Slack"},
		{name: "only known pattern accepted", body: "On behalf of @alice"},
		{name: "at most one attribution", body: "@bob requested from Slack\n@alice requested from Slack"},
		{name: "no earlier approval"},
		{name: "earlier command", comments: []github.IssueComment{historyComment("alice", "/lgtm")}, want: true},
		{name: "same person repeatedly", comments: []github.IssueComment{historyComment("bob", "/approve"), historyComment("bob", "/lgtm")}},
		{name: "outside commenter", comments: []github.IssueComment{historyComment("outsider", "/approve")}},
		{name: "bot commenter", comments: []github.IssueComment{historyComment(botLogin, "/approve")}},
		{name: "app commenter", comments: []github.IssueComment{historyComment("app[bot]", "/approve")}},
		{name: "plain comment", comments: []github.IssueComment{historyComment("alice", "Looks good")}},
		{name: "cancellation alone", comments: []github.IssueComment{historyComment("alice", "/approve cancel")}},
		{name: "later cancellation ignored", comments: []github.IssueComment{historyComment("alice", "/approve"), historyComment("alice", "/approve cancel")}, want: true},
		{name: "approved review", reviews: []github.Review{historyReview("alice", github.ReviewStateApproved, "")}, want: true},
		{name: "review body command", reviews: []github.Review{historyReview("alice", github.ReviewStateCommented, "/ok-to-test")}, want: true},
		{name: "changes requested with command", reviews: []github.Review{historyReview("alice", github.ReviewStateChangesRequested, "/verify tests")}, want: true},
		{name: "later changes requested ignored", reviews: []github.Review{historyReview("alice", github.ReviewStateApproved, ""), historyReview("alice", github.ReviewStateChangesRequested, "")}, want: true},
		{name: "non-approving review", reviews: []github.Review{historyReview("alice", github.ReviewStateChangesRequested, "")}},
		{name: "pending review with command", reviews: []github.Review{historyReview("alice", github.ReviewStatePending, "/approve")}},
		{name: "review by same actor", reviews: []github.Review{historyReview("bob", github.ReviewStateApproved, "")}},
		{name: "unknown reviewer", reviews: []github.Review{historyReview("outsider", github.ReviewStateApproved, "")}},
		{name: "dismissed command still counts", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "/approve")}, want: true},
		{name: "dismissed approval still counts", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "")}, events: []github.ListedIssueEvent{{Event: "review_dismissed", Actor: github.User{Login: "outsider"}, DismissedReview: github.DismissedReview{ReviewID: 10, State: "approved"}}}, want: true},
		{name: "dismissed changes requested does not count", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "")}, events: []github.ListedIssueEvent{{Event: "review_dismissed", DismissedReview: github.DismissedReview{ReviewID: 10, State: "changes_requested"}}}},
		{name: "unrelated dismissal does not count", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "")}, events: []github.ListedIssueEvent{{Event: "review_dismissed", DismissedReview: github.DismissedReview{ReviewID: 99, State: "approved"}}}},
		{name: "dismissed review by actor does not count twice", reviews: []github.Review{historyReview("bob", github.ReviewStateDismissed, "")}, events: []github.ListedIssueEvent{{Event: "review_dismissed", Actor: github.User{Login: "alice"}, DismissedReview: github.DismissedReview{ReviewID: 10, State: "approved"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			event := commentEvent("/approve")
			event.Issue.Body = tc.body
			fc.IssueComments[42] = tc.comments
			fc.Reviews[42] = tc.reviews
			fc.IssueEvents[42] = tc.events
			result := New(fc, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event)
			outcome := dispatch.OutcomeNoChange
			var removed []string
			if tc.want {
				outcome = dispatch.OutcomeActed
				removed = []string{"org/repo#42:" + label}
			}
			assertResult(t, result, outcome, removed, fc)
		})
	}
}

func TestDistinctApprovers(t *testing.T) {
	for _, source := range []string{"description", "comment", "review"} {
		t.Run(source, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			event := commentEvent("/approve")
			switch source {
			case "description":
				event.Issue.Body = "@alice-alt requested from Slack"
			case "comment":
				fc.IssueComments[42] = []github.IssueComment{historyComment("alice-alt", "/approve")}
			case "review":
				fc.Reviews[42] = []github.Review{historyReview("alice-alt", github.ReviewStateApproved, "")}
			}
			event.Comment.User.Login = "Alice"
			result := New(fc, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event)
			assertResult(t, result, dispatch.OutcomeNoChange, nil, fc)
		})
	}
}

func TestApprovalPermissions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actor   string
		perms   testPermissions
		outcome dispatch.Outcome
	}{
		{
			name:    "policy can grant approval independently of Cyborg",
			actor:   "outsider",
			perms:   testPermissions{"outsider": "person-1", "alice": "person-2"},
			outcome: dispatch.OutcomeActed,
		},
		{
			name:    "policy can deny the triggering actor",
			actor:   "bob",
			perms:   testPermissions{"alice": "person-1"},
			outcome: dispatch.OutcomeFiltered,
		},
		{
			name:    "policy can deny earlier approvers",
			actor:   "bob",
			perms:   testPermissions{"bob": "person-2"},
			outcome: dispatch.OutcomeNoChange,
		},
	} {
		for _, source := range []string{"description", "comment", "review"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				fc := fakegithub.NewFakeClient()
				event := commentEvent("/approve")
				switch source {
				case "description":
					event.Issue.Body = "@alice requested from Slack"
				case "comment":
					fc.IssueComments[42] = []github.IssueComment{historyComment("alice", "/approve")}
				case "review":
					fc.Reviews[42] = []github.Review{historyReview("alice", github.ReviewStateApproved, "")}
				}
				event.Comment.User.Login = tc.actor
				result := New(fc, tc.perms).HandleIssueCommentEvent(context.Background(), logger(), event)
				var removed []string
				if tc.outcome == dispatch.OutcomeActed {
					removed = []string{"org/repo#42:" + label}
				}
				assertResult(t, result, tc.outcome, removed, fc)
			})
		}
	}
}

func TestCommentFilters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*github.IssueCommentEvent)
	}{
		{"ordinary issue", func(e *github.IssueCommentEvent) { e.Issue.PullRequest = nil }},
		{"edited comment", func(e *github.IssueCommentEvent) { e.Action = github.IssueCommentActionEdited }},
		{"deleted comment", func(e *github.IssueCommentEvent) { e.Action = github.IssueCommentActionDeleted }},
		{"unknown actor", func(e *github.IssueCommentEvent) { e.Comment.User.Login = "outsider" }},
		{"designated bot actor", func(e *github.IssueCommentEvent) { e.Comment.User.Login = botLogin }},
		{"app actor", func(e *github.IssueCommentEvent) { e.Comment.User.Login = "app[bot]" }},
		{"bot type actor", func(e *github.IssueCommentEvent) {
			e.Comment.User = github.User{Login: "app", Type: github.UserTypeBot}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			event := commentEvent("/approve")
			event.Issue.Body = "@alice requested from Slack"
			tc.modify(&event)
			result := New(fc, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event)
			assertResult(t, result, dispatch.OutcomeFiltered, nil, fc)
		})
	}
}

func TestHandleReviewEvent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  github.ReviewEventAction
		state   github.ReviewState
		body    string
		actor   string
		outcome dispatch.Outcome
	}{
		{"approved review", github.ReviewActionSubmitted, github.ReviewStateApproved, "", "bob", dispatch.OutcomeActed},
		{"lowercase approved state", github.ReviewActionSubmitted, "approved", "", "bob", dispatch.OutcomeActed},
		{"commented with approval", github.ReviewActionSubmitted, github.ReviewStateCommented, "/lgtm", "bob", dispatch.OutcomeActed},
		{"changes requested with command", github.ReviewActionSubmitted, github.ReviewStateChangesRequested, "/verify all", "bob", dispatch.OutcomeActed},
		{"approved with cancel body", github.ReviewActionSubmitted, github.ReviewStateApproved, "/approve cancel", "bob", dispatch.OutcomeActed},
		{"commented without command", github.ReviewActionSubmitted, github.ReviewStateCommented, "Looks good", "bob", dispatch.OutcomeFiltered},
		{"cancellation", github.ReviewActionSubmitted, github.ReviewStateCommented, "/approve cancel", "bob", dispatch.OutcomeFiltered},
		{"pending review", github.ReviewActionSubmitted, github.ReviewStatePending, "/approve", "bob", dispatch.OutcomeFiltered},
		{"unknown actor", github.ReviewActionSubmitted, github.ReviewStateApproved, "", "outsider", dispatch.OutcomeFiltered},
		{"bot actor", github.ReviewActionSubmitted, github.ReviewStateApproved, "", botLogin, dispatch.OutcomeFiltered},
		{"app actor", github.ReviewActionSubmitted, github.ReviewStateApproved, "", "app[bot]", dispatch.OutcomeFiltered},
		{"edited review", github.ReviewActionEdited, github.ReviewStateApproved, "", "bob", dispatch.OutcomeFiltered},
		{"dismissed event", github.ReviewActionDismissed, github.ReviewStateApproved, "", "bob", dispatch.OutcomeFiltered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			fc.IssueComments[42] = []github.IssueComment{historyComment("alice", "/approve")}
			event := testhelpers.NewPR("org", "repo", 42).WithLabel(label).ReviewEvent(tc.action, tc.actor, tc.state)
			event.Review.Body = tc.body
			result := New(fc, approvalPermissions()).HandleReviewEvent(context.Background(), logger(), event)
			var removed []string
			if tc.outcome == dispatch.OutcomeActed {
				removed = []string{"org/repo#42:" + label}
			}
			assertResult(t, result, tc.outcome, removed, fc)
		})
	}
}

func TestWebhookLabelsAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		perms   permissions.IdentityChecker
		labels  []github.Label
		outcome dispatch.Outcome
	}{
		{"missing permissions checker", nil, []github.Label{{Name: label}}, dispatch.OutcomeFiltered},
		{"webhook label absent", approvalPermissions(), nil, dispatch.OutcomeNoChange},
		{"webhook label case insensitive", approvalPermissions(), []github.Label{{Name: "Needs-Human-Review"}}, dispatch.OutcomeActed},
	} {
		for _, trigger := range []string{"comment", "review"} {
			t.Run(tc.name+"/"+trigger, func(t *testing.T) {
				fc := fakegithub.NewFakeClient()
				plugin := New(fc, tc.perms)
				var result dispatch.HandlerResult
				if trigger == "comment" {
					event := commentEvent("/approve")
					event.Issue.Labels = tc.labels
					event.Issue.Body = "@alice requested from Slack"
					result = plugin.HandleIssueCommentEvent(context.Background(), logger(), event)
				} else {
					event := testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, "bob", github.ReviewStateApproved)
					event.PullRequest.Labels = tc.labels
					event.PullRequest.Body = "@alice requested from Slack"
					result = plugin.HandleReviewEvent(context.Background(), logger(), event)
				}
				var removed []string
				if tc.outcome == dispatch.OutcomeActed {
					removed = []string{"org/repo#42:" + label}
				}
				assertResult(t, result, tc.outcome, removed, fc)
			})
		}
	}
}

var errGitHub = errors.New("GitHub unavailable")

type testClient struct {
	*fakegithub.FakeClient
	operation string
	calls     []string
}

func (f *testClient) AddLabel(org, repo string, number int, value string) error {
	f.calls = append(f.calls, "add")
	if f.operation == "add" {
		return errGitHub
	}
	return f.FakeClient.AddLabel(org, repo, number, value)
}

func (f *testClient) RemoveLabel(org, repo string, number int, value string) error {
	f.calls = append(f.calls, "remove")
	if f.operation == "remove" {
		return errGitHub
	}
	return f.FakeClient.RemoveLabel(org, repo, number, value)
}

func (f *testClient) ListIssueComments(org, repo string, number int) ([]github.IssueComment, error) {
	f.calls = append(f.calls, "comments")
	if f.operation == "comments" {
		return nil, errGitHub
	}
	return f.FakeClient.ListIssueComments(org, repo, number)
}

func (f *testClient) ListReviews(org, repo string, number int) ([]github.Review, error) {
	f.calls = append(f.calls, "reviews")
	if f.operation == "reviews" {
		return nil, errGitHub
	}
	return f.FakeClient.ListReviews(org, repo, number)
}

func (f *testClient) ListIssueEvents(org, repo string, number int) ([]github.ListedIssueEvent, error) {
	f.calls = append(f.calls, "events")
	if f.operation == "events" {
		return nil, errGitHub
	}
	return f.FakeClient.ListIssueEvents(org, repo, number)
}

func TestAPIUsage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		unlabeled bool
		actor     string
		body      string
		comments  []github.IssueComment
		reviews   []github.Review
		events    []github.ListedIssueEvent
		calls     []string
		outcome   dispatch.Outcome
	}{
		{name: "unlabeled PR", unlabeled: true, outcome: dispatch.OutcomeNoChange},
		{name: "unpermitted actor", actor: "outsider", outcome: dispatch.OutcomeFiltered},
		{
			name: "attribution avoids history reads", body: "@alice requested from Slack",
			calls: []string{"remove"}, outcome: dispatch.OutcomeActed,
		},
		{
			name: "comment avoids review reads", comments: []github.IssueComment{historyComment("alice", "/approve")},
			calls: []string{"comments", "remove"}, outcome: dispatch.OutcomeActed,
		},
		{
			name: "review avoids dismissal reads", reviews: []github.Review{historyReview("alice", github.ReviewStateApproved, "")},
			calls: []string{"comments", "reviews", "remove"}, outcome: dispatch.OutcomeActed,
		},
		{
			name: "no dismissal history needed", reviews: []github.Review{historyReview("alice", github.ReviewStateChangesRequested, "")},
			calls: []string{"comments", "reviews"}, outcome: dispatch.OutcomeNoChange,
		},
		{
			name: "dismissed command avoids dismissal reads", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "/approve")},
			calls: []string{"comments", "reviews", "remove"}, outcome: dispatch.OutcomeActed,
		},
		{
			name: "dismissed approval needs dismissal history", reviews: []github.Review{historyReview("alice", github.ReviewStateDismissed, "")},
			events: []github.ListedIssueEvent{{Event: "review_dismissed", DismissedReview: github.DismissedReview{ReviewID: 10, State: "approved"}}},
			calls:  []string{"comments", "reviews", "events", "remove"}, outcome: dispatch.OutcomeActed,
		},
	} {
		for _, trigger := range []string{"comment", "review"} {
			t.Run(tc.name+"/"+trigger, func(t *testing.T) {
				fc := fakegithub.NewFakeClient()
				fc.IssueComments[42] = tc.comments
				fc.Reviews[42] = tc.reviews
				fc.IssueEvents[42] = tc.events
				client := &testClient{FakeClient: fc}
				plugin := New(client, approvalPermissions())
				actor := tc.actor
				if actor == "" {
					actor = "bob"
				}
				labels := []github.Label{{Name: label}}
				if tc.unlabeled {
					labels = nil
				}
				var result dispatch.HandlerResult
				if trigger == "comment" {
					event := commentEvent("/approve")
					event.Comment.User.Login = actor
					event.Issue.Labels = labels
					event.Issue.Body = tc.body
					result = plugin.HandleIssueCommentEvent(context.Background(), logger(), event)
				} else {
					event := testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, actor, github.ReviewStateApproved)
					event.PullRequest.Labels = labels
					event.PullRequest.Body = tc.body
					result = plugin.HandleReviewEvent(context.Background(), logger(), event)
				}
				var removed []string
				if tc.outcome == dispatch.OutcomeActed {
					removed = []string{"org/repo#42:" + label}
				}
				assertResult(t, result, tc.outcome, removed, fc)
				if diff := cmp.Diff(tc.calls, client.calls); diff != "" {
					t.Errorf("GitHub calls mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestGitHubFailures(t *testing.T) {
	for _, operation := range []string{"add", "comments", "reviews", "events", "remove"} {
		t.Run(operation, func(t *testing.T) {
			fc := fakegithub.NewFakeClient()
			fc.Reviews[42] = []github.Review{historyReview("alice", github.ReviewStateDismissed, "")}
			fc.IssueEvents[42] = []github.ListedIssueEvent{{Event: "review_dismissed", DismissedReview: github.DismissedReview{ReviewID: 10, State: "approved"}}}
			plugin := New(&testClient{FakeClient: fc, operation: operation}, approvalPermissions())
			var result dispatch.HandlerResult
			if operation == "add" {
				event := testhelpers.NewPR("org", "repo", 42).WithAuthor(botLogin).PullRequestEvent(github.PullRequestActionOpened)
				result = plugin.HandlePullRequestEvent(context.Background(), logger(), event)
			} else {
				result = plugin.HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve"))
			}
			if result.Outcome != dispatch.OutcomeErrored || !errors.Is(result.Err, errGitHub) || result.Reason == "" {
				t.Fatalf("expected GitHub error, got %+v", result)
			}
			if len(fc.IssueLabelsAdded) != 0 || len(fc.IssueLabelsRemoved) != 0 {
				t.Fatal("unexpected label mutation on error")
			}
		})
	}
}

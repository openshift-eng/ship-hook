package twohumanapprovals

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

type testPermissions map[string]bool

func (p testPermissions) IsAllowed(user, action, resource string) bool {
	return action == "approve" && resource == "pr" && p[strings.ToLower(user)]
}

func approvalPermissions() permissions.Checker {
	return testPermissions{"alice": true, "alice-alt": true, "bob": true}
}

func logger() *logrus.Entry { return logrus.NewEntry(logrus.StandardLogger()) }

func pullRequest() github.PullRequest {
	pr := testhelpers.NewPR("org", "repo", 42).WithAuthor(botLogin).PullRequestEvent(github.PullRequestActionOpened).PullRequest
	pr.Head.SHA = "head-sha"
	return pr
}

func commentEvent(body string) github.IssueCommentEvent {
	event := testhelpers.NewPR("org", "repo", 42).WithAuthor("bob").CommentEvent(body)
	event.Issue.PullRequest = &struct{}{}
	event.Issue.User.Login = botLogin
	return event
}

func historyComment(login, body string) github.IssueComment {
	return github.IssueComment{User: github.User{Login: login}, Body: body}
}

func historyReview(login string, state github.ReviewState, body string) github.Review {
	return github.Review{ID: 10, User: github.User{Login: login}, State: state, Body: body}
}

func assertResult(t *testing.T, result dispatch.HandlerResult, outcome dispatch.Outcome) {
	t.Helper()
	if result.Outcome != outcome || result.Err != nil || result.Reason == "" {
		t.Fatalf("unexpected handler result: %+v, want %s", result, outcome)
	}
}

func assertCheck(t *testing.T, client *testhelpers.CheckRunClient, sha string, passing bool) github.CheckRun {
	t.Helper()
	checks, err := client.ListCheckRuns("org", "repo", sha)
	if err != nil || len(checks.CheckRuns) != 1 {
		t.Fatalf("expected one check on %s, got %+v, %v", sha, checks, err)
	}
	check := checks.CheckRuns[0]
	status, conclusion := "in_progress", ""
	if passing {
		status, conclusion = "completed", "success"
	}
	if check.Name != pluginName || check.HeadSHA != sha || check.Status != status || check.Conclusion != conclusion || check.Output.Title == "" || check.Output.Summary == "" {
		t.Fatalf("unexpected check: %+v; want %s/%s", check, status, conclusion)
	}
	if len(client.IssueLabelsAdded) != 0 || len(client.IssueLabelsRemoved) != 0 {
		t.Fatal("plugin must not mutate labels")
	}
	return check
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
		{"/verify cancel", false},
		{"/VERIFY\tCANCEL\r\n", false},
		{"/verify all cancel", false},
		{"/verify cancel\n/verify all", true},
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
			client := testhelpers.NewCheckRunClient()
			pr := pullRequest()
			pr.Body = "@alice requested from Slack"
			client.RecordPullRequest(pr)
			result := New(client, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), commentEvent(tc.body))
			if tc.want {
				assertResult(t, result, dispatch.OutcomeActed)
				assertCheck(t, client, pr.Head.SHA, true)
			} else {
				assertResult(t, result, dispatch.OutcomeFiltered)
				if len(client.CheckRuns()) != 0 {
					t.Fatal("unexpected check for non-approving command")
				}
			}
		})
	}
}

func TestApprovalHistory(t *testing.T) {
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
		{name: "verify cancellation alone", comments: []github.IssueComment{historyComment("alice", "/verify cancel")}},
		{name: "review verify cancellation", reviews: []github.Review{historyReview("alice", github.ReviewStateCommented, "/verify cancel")}},
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
		for _, trigger := range []string{"comment", "review", "new revision"} {
			t.Run(tc.name+"/"+trigger, func(t *testing.T) {
				client := testhelpers.NewCheckRunClient()
				pr := pullRequest()
				pr.Body = tc.body
				client.RecordPullRequest(pr)
				client.IssueComments[42] = tc.comments
				client.Reviews[42] = tc.reviews
				client.IssueEvents[42] = tc.events
				plugin := New(client, approvalPermissions())
				var result dispatch.HandlerResult
				switch trigger {
				case "comment":
					result = plugin.HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve"))
				case "review":
					event := testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, "bob", github.ReviewStateApproved)
					event.PullRequest = pr
					result = plugin.HandleReviewEvent(context.Background(), logger(), event)
				case "new revision":
					// Full-history evaluation has no triggering approver to seed the count.
					client.IssueComments[42] = append(client.IssueComments[42], historyComment("bob", "/approve"))
					event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionSynchronize)
					event.PullRequest = pr
					result = plugin.HandlePullRequestEvent(context.Background(), logger(), event)
				}
				assertResult(t, result, dispatch.OutcomeActed)
				assertCheck(t, client, pr.Head.SHA, tc.want)
			})
		}
	}
}

func TestCheckLifecycle(t *testing.T) {
	client := testhelpers.NewCheckRunClient()
	plugin := New(client, approvalPermissions())
	event := testhelpers.NewPR("org", "repo", 42).WithAuthor(botLogin).PullRequestEvent(github.PullRequestActionOpened)
	event.PullRequest = pullRequest()
	client.RecordPullRequest(event.PullRequest)
	assertResult(t, plugin.HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
	waiting := assertCheck(t, client, "head-sha", false)
	// Redelivered opened events reuse the existing check without creating another.
	assertResult(t, plugin.HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeNoChange)
	assertCheck(t, client, "head-sha", false)

	client.IssueComments[42] = []github.IssueComment{historyComment("alice", "/approve")}
	assertResult(t, plugin.HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/lgtm")), dispatch.OutcomeActed)
	passing := assertCheck(t, client, "head-sha", true)
	if passing.ID != waiting.ID {
		t.Fatal("approval must update the existing check")
	}
	// Later cancellations do not undo a passing check for this revision.
	assertResult(t, plugin.HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve")), dispatch.OutcomeNoChange)

	// A different head must be evaluated from history rather than inheriting an
	// old check. The prior comments and reviews need not refer to the new SHA.
	event.Action = github.PullRequestActionSynchronize
	event.PullRequest.Head.SHA = "new-sha"
	client.Reviews[42] = []github.Review{historyReview("bob", github.ReviewStateApproved, "")}
	assertResult(t, plugin.HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
	newCheck := assertCheck(t, client, "new-sha", true)
	if newCheck.ID == passing.ID {
		t.Fatal("new revision must receive a new check")
	}
	assertCheck(t, client, "head-sha", true)
	if plugin.Name() != pluginName {
		t.Fatal("unexpected plugin name")
	}
}

func TestNewRevisionRemainsWaitingWithOneApprover(t *testing.T) {
	client := testhelpers.NewCheckRunClient()
	plugin := New(client, approvalPermissions())
	event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionSynchronize)
	event.PullRequest = pullRequest()
	event.PullRequest.Body = "@alice requested from Slack"
	if _, err := client.CreateCheckRun("org", "repo", github.CheckRun{
		Name: pluginName, HeadSHA: "old-sha", ExternalID: pluginName + ":org/repo#42",
		Status: "completed", Conclusion: "success",
	}); err != nil {
		t.Fatal(err)
	}
	client.Reviews[42] = []github.Review{historyReview("ALICE", github.ReviewStateApproved, "")}
	assertResult(t, plugin.HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
	assertCheck(t, client, "head-sha", false)
}

func TestCheckScope(t *testing.T) {
	for _, tc := range []struct {
		name, author string
		action       github.PullRequestEventAction
		outcome      dispatch.Outcome
	}{
		{"opened", botLogin, github.PullRequestActionOpened, dispatch.OutcomeActed},
		{"reopened", botLogin, github.PullRequestActionReopened, dispatch.OutcomeActed},
		{"synchronize", botLogin, github.PullRequestActionSynchronize, dispatch.OutcomeActed},
		{"case insensitive author", "RedHat-Chai-Bot", github.PullRequestActionOpened, dispatch.OutcomeActed},
		{"human author", "alice", github.PullRequestActionOpened, dispatch.OutcomeFiltered},
		{"other bot", "other[bot]", github.PullRequestActionOpened, dispatch.OutcomeFiltered},
		{"closed", botLogin, github.PullRequestActionClosed, dispatch.OutcomeFiltered},
		{"edited", botLogin, github.PullRequestActionEdited, dispatch.OutcomeFiltered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testhelpers.NewCheckRunClient()
			event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(tc.action)
			event.PullRequest = pullRequest()
			event.PullRequest.User.Login = tc.author
			assertResult(t, New(client, approvalPermissions()).HandlePullRequestEvent(context.Background(), logger(), event), tc.outcome)
			if tc.outcome == dispatch.OutcomeActed {
				assertCheck(t, client, "head-sha", false)
			} else if len(client.CheckRuns()) != 0 {
				t.Fatal("unexpected check")
			}
		})
	}
}

func TestPermissions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		perms    permissions.Checker
		expected dispatch.Outcome
		passing  bool
	}{
		{"both allowed", testPermissions{"alice": true, "bob": true}, dispatch.OutcomeActed, true},
		{"trigger denied", testPermissions{"alice": true}, dispatch.OutcomeFiltered, false},
		{"history denied", testPermissions{"bob": true}, dispatch.OutcomeActed, false},
		{"missing checker", nil, dispatch.OutcomeFiltered, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testhelpers.NewCheckRunClient()
			pr := pullRequest()
			pr.Body = "@alice requested from Slack"
			client.RecordPullRequest(pr)
			assertResult(t, New(client, tc.perms).HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve")), tc.expected)
			if tc.expected == dispatch.OutcomeActed {
				assertCheck(t, client, "head-sha", tc.passing)
			}
		})
	}
	// Missing membership must still create a waiting check on a new PR.
	client := testhelpers.NewCheckRunClient()
	event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionOpened)
	event.PullRequest = pullRequest()
	assertResult(t, New(client, nil).HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
	assertCheck(t, client, "head-sha", false)
}

func TestDistinctAccounts(t *testing.T) {
	for _, login := range []string{"ALICE", "alice-alt"} {
		t.Run(login, func(t *testing.T) {
			client := testhelpers.NewCheckRunClient()
			pr := pullRequest()
			pr.Body = "@" + login + " requested from Slack"
			client.RecordPullRequest(pr)
			event := commentEvent("/approve")
			event.Comment.User.Login = "Alice"
			assertResult(t, New(client, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
			assertCheck(t, client, "head-sha", login != "ALICE")
		})
	}
}

func TestReviewEvents(t *testing.T) {
	for _, tc := range []struct {
		name    string
		action  github.ReviewEventAction
		state   github.ReviewState
		body    string
		allowed bool
	}{
		{"approved", github.ReviewActionSubmitted, github.ReviewStateApproved, "", true},
		{"lowercase approved", github.ReviewActionSubmitted, "approved", "", true},
		{"command", github.ReviewActionSubmitted, github.ReviewStateCommented, "/lgtm", true},
		{"changes requested with command", github.ReviewActionSubmitted, github.ReviewStateChangesRequested, "/verify all", true},
		{"approval with cancel body", github.ReviewActionSubmitted, github.ReviewStateApproved, "/verify cancel", true},
		{"no command", github.ReviewActionSubmitted, github.ReviewStateCommented, "Looks good", false},
		{"cancel", github.ReviewActionSubmitted, github.ReviewStateCommented, "/verify cancel", false},
		{"pending", github.ReviewActionSubmitted, github.ReviewStatePending, "/approve", false},
		{"edited", github.ReviewActionEdited, github.ReviewStateApproved, "", false},
		{"dismissed", github.ReviewActionDismissed, github.ReviewStateApproved, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testhelpers.NewCheckRunClient()
			event := testhelpers.NewPR("org", "repo", 42).ReviewEvent(tc.action, "bob", tc.state)
			event.PullRequest = pullRequest()
			event.PullRequest.Body = "@alice requested from Slack"
			event.Review.Body = tc.body
			result := New(client, approvalPermissions()).HandleReviewEvent(context.Background(), logger(), event)
			if tc.allowed {
				assertResult(t, result, dispatch.OutcomeActed)
				assertCheck(t, client, "head-sha", true)
			} else {
				assertResult(t, result, dispatch.OutcomeFiltered)
			}
		})
	}
}

var errGitHub = errors.New("GitHub unavailable")

type testClient struct {
	*testhelpers.CheckRunClient
	operation string
	calls     []string
}

func (f *testClient) GetPullRequest(org, repo string, number int) (*github.PullRequest, error) {
	f.calls = append(f.calls, "pr")
	if f.operation == "pr" {
		return nil, errGitHub
	}
	return f.CheckRunClient.GetPullRequest(org, repo, number)
}
func (f *testClient) ListCheckRuns(org, repo, sha string) (*github.CheckRunList, error) {
	f.calls = append(f.calls, "checks")
	if f.operation == "checks" {
		return nil, errGitHub
	}
	return f.CheckRunClient.ListCheckRuns(org, repo, sha)
}
func (f *testClient) CreateCheckRun(org, repo string, check github.CheckRun) (int64, error) {
	f.calls = append(f.calls, "create")
	if f.operation == "create" {
		return 0, errGitHub
	}
	return f.CheckRunClient.CreateCheckRun(org, repo, check)
}
func (f *testClient) UpdateCheckRun(org, repo string, id int64, check github.CheckRun) error {
	f.calls = append(f.calls, "update")
	if f.operation == "update" {
		return errGitHub
	}
	return f.CheckRunClient.UpdateCheckRun(org, repo, id, check)
}
func (f *testClient) ListIssueComments(org, repo string, number int) ([]github.IssueComment, error) {
	f.calls = append(f.calls, "comments")
	if f.operation == "comments" {
		return nil, errGitHub
	}
	return f.CheckRunClient.ListIssueComments(org, repo, number)
}
func (f *testClient) ListReviews(org, repo string, number int) ([]github.Review, error) {
	f.calls = append(f.calls, "reviews")
	if f.operation == "reviews" {
		return nil, errGitHub
	}
	return f.CheckRunClient.ListReviews(org, repo, number)
}
func (f *testClient) ListIssueEvents(org, repo string, number int) ([]github.ListedIssueEvent, error) {
	f.calls = append(f.calls, "events")
	if f.operation == "events" {
		return nil, errGitHub
	}
	return f.CheckRunClient.ListIssueEvents(org, repo, number)
}

func TestAPIUsage(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		passing, attribution bool
		operation            string
		calls                []string
		outcome              dispatch.Outcome
	}{
		{name: "attribution avoids history", attribution: true, calls: []string{"pr", "checks", "create"}, outcome: dispatch.OutcomeActed},
		{name: "passing check avoids history", passing: true, calls: []string{"pr", "checks"}, outcome: dispatch.OutcomeNoChange},
		{name: "comments avoid reviews", calls: []string{"pr", "checks", "comments", "create"}, outcome: dispatch.OutcomeActed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &testClient{CheckRunClient: testhelpers.NewCheckRunClient()}
			pr := pullRequest()
			if tc.attribution {
				pr.Body = "@alice requested from Slack"
			}
			client.RecordPullRequest(pr)
			client.IssueComments[42] = []github.IssueComment{historyComment("alice", "/lgtm")}
			plugin := New(client, approvalPermissions())
			if tc.passing {
				_, err := client.CheckRunClient.CreateCheckRun("org", "repo", github.CheckRun{Name: pluginName, HeadSHA: pr.Head.SHA, ExternalID: pluginName + ":org/repo#42", Status: "completed", Conclusion: "success"})
				if err != nil {
					t.Fatal(err)
				}
			}
			assertResult(t, plugin.HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve")), tc.outcome)
			if diff := cmp.Diff(tc.calls, client.calls); diff != "" {
				t.Fatalf("calls mismatch (-want +got): %s", diff)
			}
		})
	}
	for _, actor := range []string{"outsider", botLogin, "app[bot]"} {
		client := &testClient{CheckRunClient: testhelpers.NewCheckRunClient()}
		event := commentEvent("/approve")
		event.Comment.User.Login = actor
		assertResult(t, New(client, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), event), dispatch.OutcomeFiltered)
		if len(client.calls) != 0 {
			t.Fatalf("ineligible actor caused API calls: %v", client.calls)
		}
	}
}

func TestCheckOwnership(t *testing.T) {
	client := testhelpers.NewCheckRunClient()
	pr := pullRequest()
	// Neither a different PR's check nor a differently named check is ours.
	for _, check := range []github.CheckRun{
		{Name: pluginName, HeadSHA: pr.Head.SHA, ExternalID: pluginName + ":org/repo#99", Status: "completed", Conclusion: "success"},
		{Name: "other-plugin", HeadSHA: pr.Head.SHA, ExternalID: pluginName + ":org/repo#42", Status: "completed", Conclusion: "success"},
	} {
		if _, err := client.CreateCheckRun("org", "repo", check); err != nil {
			t.Fatal(err)
		}
	}
	event := testhelpers.NewPR("org", "repo", 42).PullRequestEvent(github.PullRequestActionOpened)
	event.PullRequest = pr
	assertResult(t, New(client, approvalPermissions()).HandlePullRequestEvent(context.Background(), logger(), event), dispatch.OutcomeActed)
	checks := client.CheckRuns()
	if len(checks) != 3 || checks[2].Status != "in_progress" {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestGitHubFailures(t *testing.T) {
	for _, operation := range []string{"pr", "checks", "comments", "reviews", "events", "create", "update"} {
		t.Run(operation, func(t *testing.T) {
			client := &testClient{CheckRunClient: testhelpers.NewCheckRunClient(), operation: operation}
			pr := pullRequest()
			client.RecordPullRequest(pr)
			client.Reviews[42] = []github.Review{historyReview("alice", github.ReviewStateDismissed, "")}
			client.IssueEvents[42] = []github.ListedIssueEvent{{Event: "review_dismissed", DismissedReview: github.DismissedReview{ReviewID: 10, State: "approved"}}}
			if operation == "update" {
				if _, err := client.CheckRunClient.CreateCheckRun("org", "repo", github.CheckRun{Name: pluginName, HeadSHA: pr.Head.SHA, ExternalID: pluginName + ":org/repo#42", Status: "in_progress"}); err != nil {
					t.Fatal(err)
				}
			}
			result := New(client, approvalPermissions()).HandleIssueCommentEvent(context.Background(), logger(), commentEvent("/approve"))
			if result.Outcome != dispatch.OutcomeErrored || !errors.Is(result.Err, errGitHub) || result.Reason == "" {
				t.Fatalf("expected GitHub failure: %+v", result)
			}
			for _, check := range client.CheckRuns() {
				if check.Conclusion == "success" {
					t.Fatal("failed request must not publish success")
				}
			}
		})
	}
}

// Pause the first creation after its check lookup and history evaluation. A
// concurrent handler must wait until that run is visible before doing its lookup.
type concurrentCheckClient struct {
	*testhelpers.CheckRunClient
	lookups  atomic.Int32
	creates  atomic.Int32
	creating chan struct{}
	resume   chan struct{}
}

func (f *concurrentCheckClient) ListCheckRuns(org, repo, sha string) (*github.CheckRunList, error) {
	f.lookups.Add(1)
	return f.CheckRunClient.ListCheckRuns(org, repo, sha)
}

func (f *concurrentCheckClient) CreateCheckRun(org, repo string, check github.CheckRun) (int64, error) {
	if f.creates.Add(1) == 1 {
		close(f.creating)
		<-f.resume
	}
	return f.CheckRunClient.CreateCheckRun(org, repo, check)
}

// Observe when the second handler reaches the shared reconciliation lock.
// The mutex itself is the same implementation used in production.
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

func TestConcurrentReconciliation(t *testing.T) {
	for _, firstPasses := range []bool{false, true} {
		name := "waiting then passing"
		if firstPasses {
			name = "passing then waiting"
		}
		t.Run(name, func(t *testing.T) {
			client := &concurrentCheckClient{
				CheckRunClient: testhelpers.NewCheckRunClient(),
				creating:       make(chan struct{}), resume: make(chan struct{}),
			}
			release := sync.OnceFunc(func() { close(client.resume) })
			defer release()
			client.IssueComments[42] = []github.IssueComment{historyComment("alice", "/approve")}
			plugin := New(client, approvalPermissions())
			locks := &observedKeyMutex{KeyMutex: plugin.reconciliations, second: make(chan struct{})}
			plugin.reconciliations = locks
			firstActor, secondActor := "alice", "bob"
			if firstPasses {
				firstActor, secondActor = secondActor, firstActor
			}
			first := testhelpers.NewPR("org", "repo", 42).ReviewEvent(github.ReviewActionSubmitted, firstActor, github.ReviewStateApproved)
			first.PullRequest = pullRequest()
			second := first
			second.Review.User.Login = secondActor
			results := make(chan dispatch.HandlerResult, 2)
			go func() {
				results <- plugin.HandleReviewEvent(context.Background(), logger(), first)
			}()
			select {
			case <-client.creating:
			case <-time.After(5 * time.Second):
				t.Fatal("first handler did not reach check creation")
			}
			go func() {
				results <- plugin.HandleReviewEvent(context.Background(), logger(), second)
			}()
			select {
			case <-locks.second:
			case <-time.After(5 * time.Second):
				t.Fatal("second handler did not coordinate reconciliation")
			}
			if got := client.lookups.Load(); got != 1 {
				t.Fatalf("second handler read checks before the first completed: %d lookups", got)
			}
			release()
			for range 2 {
				select {
				case result := <-results:
					if result.Err != nil || (result.Outcome != dispatch.OutcomeActed && result.Outcome != dispatch.OutcomeNoChange) {
						t.Fatalf("unexpected reconciliation result: %+v", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("reconciliation did not finish")
				}
			}
			assertCheck(t, client.CheckRunClient, "head-sha", true)
			if client.creates.Load() != 1 {
				t.Fatalf("concurrent handlers created %d check runs", client.creates.Load())
			}
		})
	}
}

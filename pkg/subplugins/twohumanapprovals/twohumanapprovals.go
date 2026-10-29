package twohumanapprovals

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
)

const (
	pluginName = "two-human-approvals"
	botLogin   = "redhat-chai-bot"
	label      = "needs-human-review"
)

var (
	approvalCommand = regexp.MustCompile(`(?mi)^/(ok-to-test|lgtm|approve|verify)(?:[\t ]+([^\r\n]*))?[\t ]*\r?$`)
	requester       = regexp.MustCompile(`(?m)^@([a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?) requested from Slack[\t ]*\r?$`)
)

type githubClient interface {
	AddLabel(org, repo string, number int, label string) error
	RemoveLabel(org, repo string, number int, label string) error
	ListIssueComments(org, repo string, number int) ([]github.IssueComment, error)
	ListReviews(org, repo string, number int) ([]github.Review, error)
	ListIssueEvents(org, repo string, number int) ([]github.ListedIssueEvent, error)
}

type Plugin struct {
	ghc   githubClient
	perms permissions.IdentityChecker
}

// New constructs the plugin with approval permissions and identity comparison.
// A nil checker denies approvals.
func New(ghc githubClient, perms permissions.IdentityChecker) *Plugin {
	return &Plugin{ghc: ghc, perms: perms}
}

func (p *Plugin) Name() string { return pluginName }

func (p *Plugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, event github.PullRequestEvent) dispatch.HandlerResult {
	if event.Action != github.PullRequestActionOpened {
		return dispatch.Filtered("action is not opened")
	}
	if !strings.EqualFold(event.PullRequest.User.Login, botLogin) {
		return dispatch.Filtered("author is not the designated bot")
	}
	if hasLabel(event.PullRequest) {
		return dispatch.NoChange("PR already has needs-human-review label")
	}
	if err := p.ghc.AddLabel(event.Repo.Owner.Login, event.Repo.Name, event.Number, label); err != nil {
		return dispatch.Errored("failed to add needs-human-review label", err)
	}
	return dispatch.Acted("labeled PR from designated bot")
}

func (p *Plugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, event github.IssueCommentEvent) dispatch.HandlerResult {
	if !event.Issue.IsPullRequest() {
		return dispatch.Filtered("comment is not on a PR")
	}
	if event.Action != github.IssueCommentActionCreated {
		return dispatch.Filtered("action is not created")
	}
	if !hasApprovalCommand(event.Comment.Body) {
		return dispatch.Filtered("comment contains no approval command")
	}
	return p.handleApproval(event.Repo.Owner.Login, event.Repo.Name, github.PullRequest{
		Number: event.Issue.Number,
		Labels: event.Issue.Labels,
		Body:   event.Issue.Body,
	}, event.Comment.User.Login)
}

func (p *Plugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, event github.ReviewEvent) dispatch.HandlerResult {
	if event.Action != github.ReviewActionSubmitted {
		return dispatch.Filtered("action is not submitted")
	}
	if !isApproval(event.Review) {
		return dispatch.Filtered("review expresses no approval")
	}
	return p.handleApproval(event.Repo.Owner.Login, event.Repo.Name, event.PullRequest, event.Review.User.Login)
}

func (p *Plugin) handleApproval(org, repo string, pr github.PullRequest, actor string) dispatch.HandlerResult {
	// Trust the webhook snapshot to avoid fetching the PR for every approval.
	if !hasLabel(pr) {
		return dispatch.NoChange("PR has no needs-human-review label")
	}
	if !p.canApprove(actor) {
		return dispatch.Filtered("actor is not permitted to approve PRs")
	}
	number := pr.Number

	if match := requester.FindStringSubmatch(pr.Body); match != nil && p.differentApprover(match[1], actor) {
		return p.removeLabel(org, repo, number)
	}

	comments, err := p.ghc.ListIssueComments(org, repo, number)
	if err != nil {
		return dispatch.Errored("failed to list PR comments", err)
	}
	for _, comment := range comments {
		if hasApprovalCommand(comment.Body) && p.differentApprover(comment.User.Login, actor) {
			return p.removeLabel(org, repo, number)
		}
	}

	reviews, err := p.ghc.ListReviews(org, repo, number)
	if err != nil {
		return dispatch.Errored("failed to list PR reviews", err)
	}
	dismissed := make(map[int64]struct{})
	for _, review := range reviews {
		if !p.differentApprover(review.User.Login, actor) {
			continue
		}
		if isApproval(review) {
			return p.removeLabel(org, repo, number)
		}
		if strings.EqualFold(string(review.State), string(github.ReviewStateDismissed)) {
			dismissed[int64(review.ID)] = struct{}{}
		}
	}
	if len(dismissed) == 0 {
		return dispatch.NoChange("no approval from a second permitted approver")
	}

	// GitHub replaces a dismissed review's state. Issue events preserve its
	// original state; join by review ID to count the reviewer, not the dismisser.
	events, err := p.ghc.ListIssueEvents(org, repo, number)
	if err != nil {
		return dispatch.Errored("failed to list PR dismissal history", err)
	}
	for _, event := range events {
		if event.Event == "review_dismissed" && strings.EqualFold(event.DismissedReview.State, string(github.ReviewStateApproved)) {
			if _, ok := dismissed[event.DismissedReview.ReviewID]; ok {
				return p.removeLabel(org, repo, number)
			}
		}
	}
	return dispatch.NoChange("no approval from a second permitted approver")
}

func (p *Plugin) canApprove(user string) bool {
	return p.perms != nil && p.perms.IsAllowed(user, "approve", "pr")
}

func (p *Plugin) differentApprover(user, actor string) bool {
	return p.canApprove(user) && !p.perms.SameIdentity(user, actor)
}

func (p *Plugin) removeLabel(org, repo string, number int) dispatch.HandlerResult {
	if err := p.ghc.RemoveLabel(org, repo, number, label); err != nil {
		return dispatch.Errored("failed to remove needs-human-review label", err)
	}
	return dispatch.Acted("removed needs-human-review label after two permitted approvals")
}

func hasLabel(pr github.PullRequest) bool {
	for _, existing := range pr.Labels {
		if strings.EqualFold(existing.Name, label) {
			return true
		}
	}
	return false
}

func isApproval(review github.Review) bool {
	return !strings.EqualFold(string(review.State), string(github.ReviewStatePending)) &&
		(strings.EqualFold(string(review.State), string(github.ReviewStateApproved)) || hasApprovalCommand(review.Body))
}

func hasApprovalCommand(body string) bool {
	for _, match := range approvalCommand.FindAllStringSubmatch(body, -1) {
		if strings.EqualFold(match[1], "verify") || !slices.Contains(strings.Fields(strings.ToLower(match[2])), "cancel") {
			return true
		}
	}
	return false
}

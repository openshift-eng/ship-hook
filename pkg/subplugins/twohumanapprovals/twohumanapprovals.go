package twohumanapprovals

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
)

const (
	pluginName = "two-human-approvals"
	botLogin   = "redhat-chai-bot"

	commandArguments = `(?:[\t ]+[^\r\n]*)?[\t ]*\r?`
	okToTestCommand  = `/ok-to-test` + commandArguments
	lgtmCommand      = `/lgtm` + commandArguments
	approveCommand   = `/approve` + commandArguments
	verifyCommand    = `/verify` + commandArguments
)

var (
	approvalCommand = regexp.MustCompile(`(?mi)^(?:` + strings.Join([]string{
		okToTestCommand,
		lgtmCommand,
		approveCommand,
		verifyCommand,
	}, `|`) + `)$`)
	requester = regexp.MustCompile(`(?m)^@([a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?) requested from Slack[\t ]*\r?$`)
)

type githubClient interface {
	GetPullRequest(org, repo string, number int) (*github.PullRequest, error)
	ListCheckRuns(org, repo, ref string) (*github.CheckRunList, error)
	CreateCheckRun(org, repo string, check github.CheckRun) (int64, error)
	UpdateCheckRun(org, repo string, id int64, check github.CheckRun) error
	ListIssueComments(org, repo string, number int) ([]github.IssueComment, error)
	ListReviews(org, repo string, number int) ([]github.Review, error)
	ListIssueEvents(org, repo string, number int) ([]github.ListedIssueEvent, error)
}

type Plugin struct {
	ghc             githubClient
	perms           permissions.Checker
	reconciliations keymutex.KeyMutex
}

// New constructs the plugin with approval permissions.
// A nil checker denies approvals.
func New(ghc githubClient, perms permissions.Checker) *Plugin {
	return &Plugin{ghc: ghc, perms: perms, reconciliations: keymutex.NewHashed(0)}
}

func (p *Plugin) Name() string { return pluginName }

func (p *Plugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, event github.PullRequestEvent) dispatch.HandlerResult {
	switch event.Action {
	case github.PullRequestActionOpened, github.PullRequestActionSynchronize, github.PullRequestActionReopened:
	default:
		return dispatch.Filtered("action does not require reevaluation")
	}
	return p.reconcile(event.Repo.Owner.Login, event.Repo.Name, event.PullRequest, "")
}

func (p *Plugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, event github.IssueCommentEvent) dispatch.HandlerResult {
	if !event.Issue.IsPullRequest() {
		return dispatch.Filtered("comment is not on a PR")
	}
	if event.Action != github.IssueCommentActionCreated {
		return dispatch.Filtered("action is not created")
	}
	if !strings.EqualFold(event.Issue.User.Login, botLogin) {
		return dispatch.Filtered("author is not the designated bot")
	}
	if !hasApprovalCommand(event.Comment.Body) {
		return dispatch.Filtered("comment contains no approval command")
	}
	if !p.canApprove(event.Comment.User.Login) {
		return dispatch.Filtered("actor is not permitted to approve PRs")
	}
	// Comment webhooks have no head SHA. Fetch it so the check belongs to the
	// current revision; PR and review webhooks already carry that information.
	pr, err := p.ghc.GetPullRequest(event.Repo.Owner.Login, event.Repo.Name, event.Issue.Number)
	if err != nil {
		return dispatch.Errored("failed to get PR head for approval check", err)
	}
	return p.reconcile(event.Repo.Owner.Login, event.Repo.Name, *pr, event.Comment.User.Login)
}

func (p *Plugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, event github.ReviewEvent) dispatch.HandlerResult {
	if event.Action != github.ReviewActionSubmitted {
		return dispatch.Filtered("action is not submitted")
	}
	if !isApproval(event.Review) {
		return dispatch.Filtered("review expresses no approval")
	}
	if !p.canApprove(event.Review.User.Login) {
		return dispatch.Filtered("actor is not permitted to approve PRs")
	}
	return p.reconcile(event.Repo.Owner.Login, event.Repo.Name, event.PullRequest, event.Review.User.Login)
}

func (p *Plugin) reconcile(org, repo string, pr github.PullRequest, actor string) dispatch.HandlerResult {
	if !strings.EqualFold(pr.User.Login, botLogin) {
		return dispatch.Filtered("author is not the designated bot")
	}
	if pr.Head.SHA == "" {
		return dispatch.Errored("cannot report approval check without a head SHA", fmt.Errorf("PR head SHA is empty"))
	}
	// Serialize the entire read/evaluate/write cycle for a PR, across event
	// types and revisions, so concurrent handlers cannot create competing runs.
	key := strings.ToLower(fmt.Sprintf("%s/%s#%d", org, repo, pr.Number))
	p.reconciliations.LockKey(key)
	defer p.reconciliations.UnlockKey(key) //nolint:errcheck // Hashed key mutex unlock always returns nil.

	checks, err := p.ghc.ListCheckRuns(org, repo, pr.Head.SHA)
	if err != nil {
		return dispatch.Errored("failed to list approval checks", err)
	}
	// Include the PR identity because multiple PRs may point at the same commit.
	externalID := fmt.Sprintf("%s:%s/%s#%d", pluginName, org, repo, pr.Number)
	var existing *github.CheckRun
	for i := range checks.CheckRuns {
		check := &checks.CheckRuns[i]
		if check.Name == pluginName && check.ExternalID == externalID && (existing == nil || check.ID > existing.ID) {
			existing = check
		}
	}
	if existing != nil && existing.Status == "completed" && existing.Conclusion == "success" {
		return dispatch.NoChange("approval check already passes for this revision")
	}

	approved, err := p.hasTwoApprovers(org, repo, pr, actor)
	if err != nil {
		return dispatch.Errored("failed to evaluate approval history", err)
	}
	check := github.CheckRun{
		Name: pluginName, HeadSHA: pr.Head.SHA, ExternalID: externalID,
		Status: "in_progress",
		Output: github.CheckRunOutput{
			Title:   "Waiting for two humans",
			Summary: "Waiting for qualifying participation from two distinct eligible human GitHub accounts.",
		},
	}
	if approved {
		check.Status = "completed"
		check.Conclusion = "success"
		check.Output.Title = "Two humans have participated"
		check.Output.Summary = "Two distinct eligible human GitHub accounts have expressed approval in this PR's history."
	}
	if existing != nil {
		if existing.Status == check.Status && existing.Conclusion == check.Conclusion && existing.Output.Title == check.Output.Title && existing.Output.Summary == check.Output.Summary {
			return dispatch.NoChange("approval check is unchanged")
		}
		// A check's commit is immutable; head_sha belongs only in create requests.
		check.HeadSHA = ""
		if err := p.ghc.UpdateCheckRun(org, repo, existing.ID, check); err != nil {
			return dispatch.Errored("failed to update approval check", err)
		}
		return dispatch.Acted("updated approval check")
	}
	if _, err := p.ghc.CreateCheckRun(org, repo, check); err != nil {
		return dispatch.Errored("failed to create approval check", err)
	}
	return dispatch.Acted("created approval check")
}

func (p *Plugin) hasTwoApprovers(org, repo string, pr github.PullRequest, actor string) (bool, error) {
	approvers := sets.New[string]()
	add := func(login string) bool {
		if p.canApprove(login) {
			approvers.Insert(strings.ToLower(login))
		}
		return approvers.Len() >= 2
	}
	// The triggering event may not yet be visible when listing GitHub history.
	if actor != "" {
		add(actor)
	}
	if match := requester.FindStringSubmatch(pr.Body); match != nil && add(match[1]) {
		return true, nil
	}
	comments, err := p.ghc.ListIssueComments(org, repo, pr.Number)
	if err != nil {
		return false, fmt.Errorf("list PR comments: %w", err)
	}
	for _, comment := range comments {
		if hasApprovalCommand(comment.Body) && add(comment.User.Login) {
			return true, nil
		}
	}
	reviews, err := p.ghc.ListReviews(org, repo, pr.Number)
	if err != nil {
		return false, fmt.Errorf("list PR reviews: %w", err)
	}
	dismissed := make(map[int64]string)
	for _, review := range reviews {
		if isApproval(review) && add(review.User.Login) {
			return true, nil
		}
		if strings.EqualFold(string(review.State), string(github.ReviewStateDismissed)) && p.canApprove(review.User.Login) {
			dismissed[int64(review.ID)] = review.User.Login
		}
	}
	if len(dismissed) == 0 {
		return false, nil
	}
	// Dismissals preserve the original review state. Count the reviewer rather
	// than the actor who dismissed it, regardless of the reviewed commit SHA.
	events, err := p.ghc.ListIssueEvents(org, repo, pr.Number)
	if err != nil {
		return false, fmt.Errorf("list PR dismissal history: %w", err)
	}
	for _, event := range events {
		if event.Event == "review_dismissed" && strings.EqualFold(event.DismissedReview.State, string(github.ReviewStateApproved)) {
			if login, ok := dismissed[event.DismissedReview.ReviewID]; ok && add(login) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *Plugin) canApprove(user string) bool {
	return p.perms != nil && p.perms.IsAllowed(user, "approve", "pr")
}

func isApproval(review github.Review) bool {
	return !strings.EqualFold(string(review.State), string(github.ReviewStatePending)) &&
		(strings.EqualFold(string(review.State), string(github.ReviewStateApproved)) || hasApprovalCommand(review.Body))
}

func hasApprovalCommand(body string) bool {
	for _, command := range approvalCommand.FindAllString(body, -1) {
		arguments := strings.Fields(strings.ToLower(command))[1:]
		if !slices.Contains(arguments, "cancel") {
			return true
		}
	}
	return false
}

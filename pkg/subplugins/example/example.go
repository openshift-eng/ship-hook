// Package example is a reference sub-plugin implementation.
// Copy this package to create new sub-plugins.
package example

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
)

type githubClient interface {
	CreateComment(owner, repo string, number int, comment string) error
}

type Plugin struct {
	ghc githubClient
}

func New(ghc githubClient) *Plugin {
	return &Plugin{ghc: ghc}
}

func (p *Plugin) Name() string {
	return "example"
}

func (p *Plugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, event github.PullRequestEvent) dispatch.HandlerResult {
	org := event.Repo.Owner.Login
	repo := event.Repo.Name
	number := event.Number
	if err := p.ghc.CreateComment(org, repo, number, fmt.Sprintf("example plugin noticed PR #%d", number)); err != nil {
		return dispatch.Errored("failed to create comment", err)
	}
	return dispatch.Acted("commented on PR")
}

func (p *Plugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) dispatch.HandlerResult {
	return dispatch.Skipped("only handles pull request events")
}

func (p *Plugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, _ github.ReviewEvent) dispatch.HandlerResult {
	return dispatch.Skipped("only handles pull request events")
}

package readyforhumans

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/authz"
	"github.com/openshift-eng/ship-hook/pkg/config"
	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/featuregate"
)

const pluginName = "ready-for-humans"

type PluginConfig struct {
	Label    string `json:"label"`
	BotLogin string `json:"bot_login"`
}

func defaultConfig() PluginConfig {
	return PluginConfig{
		Label:    "ready-for-human-review",
		BotLogin: "coderabbitai[bot]",
	}
}

type githubClient interface {
	AddLabel(org, repo string, number int, label string) error
}

type Plugin struct {
	ghc      githubClient
	resolver *config.Resolver
	gates    *featuregate.Gates
	authz    authz.Authorizer
}

func New(ghc githubClient, resolver *config.Resolver, gates *featuregate.Gates, az authz.Authorizer) *Plugin {
	return &Plugin{ghc: ghc, resolver: resolver, gates: gates, authz: az}
}

func (p *Plugin) Name() string { return pluginName }

func (p *Plugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, pre github.PullRequestEvent) dispatch.HandlerResult {
	if !p.gates.Enabled("coderabbit-auto-label") {
		return dispatch.Filtered("coderabbit-auto-label gate disabled")
	}

	if pre.Action != github.PullRequestActionOpened {
		return dispatch.Filtered("action is not opened")
	}

	org := pre.Repo.Owner.Login
	repo := pre.Repo.Name
	cfg := config.ResolvePluginConfig(p.resolver, pluginName, defaultConfig(), org, repo)

	if prHasLabel(pre.PullRequest, cfg.Label) {
		return dispatch.NoChange("PR already has label")
	}

	author := pre.PullRequest.User.Login
	if p.authz.IsAllowed(author, "access-coderabbit", authz.Repo(org, repo)) {
		return dispatch.NoChange("author has coderabbit access")
	}

	if err := p.ghc.AddLabel(org, repo, pre.Number, cfg.Label); err != nil {
		return dispatch.Errored("failed to add ready-for-humans label", err)
	}
	return dispatch.Acted("labeled PR from author without coderabbit access")
}

func (p *Plugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) dispatch.HandlerResult {
	return dispatch.Skipped("only handles review and PR events")
}

func (p *Plugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, re github.ReviewEvent) dispatch.HandlerResult {
	org := re.Repo.Owner.Login
	repo := re.Repo.Name
	cfg := config.ResolvePluginConfig(p.resolver, pluginName, defaultConfig(), org, repo)

	if prHasLabel(re.PullRequest, cfg.Label) {
		return dispatch.NoChange("PR already has label")
	}

	if re.Review.User.Login != cfg.BotLogin {
		return dispatch.Filtered("reviewer is not the configured bot")
	}

	if re.Action != github.ReviewActionSubmitted {
		return dispatch.Filtered("action is not submitted")
	}

	if !strings.EqualFold(string(re.Review.State), string(github.ReviewStateApproved)) {
		return dispatch.NoChange("review state is not approved")
	}

	number := re.PullRequest.Number

	if err := p.ghc.AddLabel(org, repo, number, cfg.Label); err != nil {
		return dispatch.Errored("failed to add ready-for-humans label", err)
	}
	return dispatch.Acted("added ready-for-humans label")
}

func prHasLabel(pr github.PullRequest, label string) bool {
	for _, l := range pr.Labels {
		if strings.EqualFold(l.Name, label) {
			return true
		}
	}
	return false
}

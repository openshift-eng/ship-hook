// Package metahold keeps the aggregate do-not-merge/meta label in sync with other holds.
package metahold

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/dispatch"
)

const metaLabel = "do-not-merge/meta"

type githubClient interface {
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
	AddLabel(org, repo string, number int, label string) error
	RemoveLabel(org, repo string, number int, label string) error
}

type Plugin struct {
	ghc             githubClient
	reconciliations keymutex.KeyMutex
}

// New constructs a plugin that coordinates label reconciliation for each PR.
func New(ghc githubClient) *Plugin {
	return &Plugin{ghc: ghc, reconciliations: keymutex.NewHashed(0)}
}

func (p *Plugin) Name() string { return "meta-hold" }

// HandlePullRequestEvent reconciles the aggregate hold after opening, reopening,
// or label changes, using current labels rather than the webhook snapshot.
func (p *Plugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, event github.PullRequestEvent) dispatch.HandlerResult {
	switch event.Action {
	case github.PullRequestActionOpened, github.PullRequestActionReopened, github.PullRequestActionLabeled, github.PullRequestActionUnlabeled:
	default:
		return dispatch.Filtered("action does not affect hold labels")
	}

	org, repo, number := event.Repo.Owner.Login, event.Repo.Name, event.Number
	// Coordinate the entire read/evaluate/write cycle within this instance so
	// concurrent deliveries do not act on the same obsolete label state.
	key := strings.ToLower(fmt.Sprintf("%s/%s#%d", org, repo, number))
	p.reconciliations.LockKey(key)
	defer p.reconciliations.UnlockKey(key) //nolint:errcheck // Hashed key mutex unlock always returns nil.

	// Webhooks may be delayed or delivered out of order. Reconcile current labels,
	// including on events caused by our own label changes.
	labels, err := p.ghc.GetIssueLabels(org, repo, number)
	if err != nil {
		return dispatch.Errored("failed to get PR labels", err)
	}

	var hasMeta, hasHold bool
	for _, label := range labels {
		name := strings.ToLower(label.Name)
		if name == metaLabel {
			hasMeta = true
		} else if strings.HasPrefix(name, "do-not-merge/") {
			hasHold = true
		}
	}

	switch {
	case hasHold && !hasMeta:
		if err := p.ghc.AddLabel(org, repo, number, metaLabel); err != nil {
			return dispatch.Errored("failed to add meta hold label", err)
		}
		return dispatch.Acted("added meta hold label because another hold exists")
	case !hasHold && hasMeta:
		if err := p.ghc.RemoveLabel(org, repo, number, metaLabel); err != nil {
			return dispatch.Errored("failed to remove meta hold label", err)
		}
		return dispatch.Acted("removed meta hold label because no other holds exist")
	default:
		return dispatch.NoChange("meta hold label is in sync with other holds")
	}
}

func (p *Plugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) dispatch.HandlerResult {
	return dispatch.Skipped("only handles pull request events")
}

func (p *Plugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, _ github.ReviewEvent) dispatch.HandlerResult {
	return dispatch.Skipped("only handles pull request events")
}

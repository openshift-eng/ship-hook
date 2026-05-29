package testhelpers

import "sigs.k8s.io/prow/pkg/github"

// PRBuilder constructs a pull request and produces GitHub webhook events from it.
type PRBuilder struct {
	org    string
	repo   string
	number int
	author string
	labels []github.Label
}

func NewPR(org, repo string, number int) *PRBuilder {
	return &PRBuilder{org: org, repo: repo, number: number}
}

func (p *PRBuilder) WithAuthor(login string) *PRBuilder {
	p.author = login
	return p
}

func (p *PRBuilder) WithLabel(name string) *PRBuilder {
	p.labels = append(p.labels, github.Label{Name: name})
	return p
}

func (p *PRBuilder) PullRequestEvent(action github.PullRequestEventAction) github.PullRequestEvent {
	return github.PullRequestEvent{
		Action: action,
		Number: p.number,
		PullRequest: github.PullRequest{
			Number: p.number,
			User:   github.User{Login: p.author},
			Labels: p.labels,
			Base: github.PullRequestBranch{
				Repo: github.Repo{
					Owner: github.User{Login: p.org},
					Name:  p.repo,
				},
			},
		},
		Repo: github.Repo{
			Owner: github.User{Login: p.org},
			Name:  p.repo,
		},
	}
}

func (p *PRBuilder) ReviewEvent(action github.ReviewEventAction, reviewer string, state github.ReviewState) github.ReviewEvent {
	return github.ReviewEvent{
		Action: action,
		Review: github.Review{
			User:  github.User{Login: reviewer},
			State: state,
		},
		PullRequest: github.PullRequest{
			Number: p.number,
			Labels: p.labels,
		},
		Repo: github.Repo{
			Owner: github.User{Login: p.org},
			Name:  p.repo,
		},
	}
}

func (p *PRBuilder) CommentEvent(body string) github.IssueCommentEvent {
	return github.IssueCommentEvent{
		Action: github.IssueCommentActionCreated,
		Issue: github.Issue{
			Number: p.number,
		},
		Comment: github.IssueComment{
			Body: body,
			User: github.User{Login: p.author},
		},
		Repo: github.Repo{
			Owner: github.User{Login: p.org},
			Name:  p.repo,
		},
	}
}

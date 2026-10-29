package testhelpers

import (
	"fmt"
	"sort"
	"sync"

	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"
)

// CheckRunClient adds check-run support to Prow's fake GitHub client for tests
// and the dev server. Its check state is scoped by repository and commit.
type CheckRunClient struct {
	*fakegithub.FakeClient
	mu     sync.Mutex
	nextID int64
	checks map[string][]github.CheckRun
}

func NewCheckRunClient() *CheckRunClient {
	return &CheckRunClient{FakeClient: fakegithub.NewFakeClient(), checks: make(map[string][]github.CheckRun)}
}

func (f *CheckRunClient) ListCheckRuns(org, repo, ref string) (*github.CheckRunList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	checks := append([]github.CheckRun(nil), f.checks[org+"/"+repo+"@"+ref]...)
	return &github.CheckRunList{Total: len(checks), CheckRuns: checks}, nil
}

func (f *CheckRunClient) CreateCheckRun(org, repo string, check github.CheckRun) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	check.ID = f.nextID
	key := org + "/" + repo + "@" + check.HeadSHA
	f.checks[key] = append(f.checks[key], check)
	return check.ID, nil
}

func (f *CheckRunClient) UpdateCheckRun(org, repo string, id int64, update github.CheckRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, checks := range f.checks {
		for i := range checks {
			if checks[i].ID == id && key == org+"/"+repo+"@"+checks[i].HeadSHA {
				update.ID = id
				update.HeadSHA = checks[i].HeadSHA
				checks[i] = update
				return nil
			}
		}
	}
	return fmt.Errorf("check run %d does not exist in %s/%s", id, org, repo)
}

// CheckRuns returns all check runs in creation order for dev-state inspection.
func (f *CheckRunClient) CheckRuns() []github.CheckRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []github.CheckRun
	for _, checks := range f.checks {
		result = append(result, checks...)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (f *CheckRunClient) ResetCheckRuns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = make(map[string][]github.CheckRun)
}

// RecordPullRequest makes a webhook's PR available for subsequent comment
// handlers, whose webhook payloads do not include the head SHA.
func (f *CheckRunClient) RecordPullRequest(pr github.PullRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PullRequests[pr.Number] = &pr
}

func (f *CheckRunClient) GetPullRequest(org, repo string, number int) (*github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.FakeClient.GetPullRequest(org, repo, number)
}

// RecordIssueComment and RecordReview retain delivered participation so dev-server
// revision events can query earlier webhook history through the fake client.
func (f *CheckRunClient) RecordIssueComment(number int, comment github.IssueComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.IssueComments[number] = append(f.IssueComments[number], comment)
}

func (f *CheckRunClient) RecordReview(number int, review github.Review) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Reviews[number] = append(f.Reviews[number], review)
}

func (f *CheckRunClient) ListIssueComments(org, repo string, number int) ([]github.IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.FakeClient.ListIssueComments(org, repo, number)
}

func (f *CheckRunClient) ListReviews(org, repo string, number int) ([]github.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.FakeClient.ListReviews(org, repo, number)
}

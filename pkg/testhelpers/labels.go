package testhelpers

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"sigs.k8s.io/prow/pkg/github"
	"sigs.k8s.io/prow/pkg/github/fakegithub"
)

// LabelClient gives tests and the dev server mutable label snapshots while
// retaining Prow's fake client mutation tracking.
type LabelClient struct {
	*fakegithub.FakeClient
	mu      sync.Mutex
	labels  map[string][]github.Label
	added   map[string]bool
	removed map[string]bool
}

func NewLabelClient(ghc *fakegithub.FakeClient) *LabelClient {
	return &LabelClient{FakeClient: ghc, labels: make(map[string][]github.Label), added: make(map[string]bool), removed: make(map[string]bool)}
}

func labelKey(org, repo string, number int) string {
	return strings.ToLower(fmt.Sprintf("%s/%s#%d", org, repo, number))
}

// RecordLabels replaces a PR's current labels with a newly delivered snapshot.
func (f *LabelClient) RecordLabels(org, repo string, number int, labels []github.Label) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[labelKey(org, repo, number)] = slices.Clone(labels)
}

func (f *LabelClient) GetIssueLabels(org, repo string, number int) ([]github.Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.labels[labelKey(org, repo, number)]), nil
}

func (f *LabelClient) AddLabel(org, repo string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := labelKey(org, repo, number)
	// Prow's fake records each operation once. Keep snapshots mutable across
	// repeated hold cycles without asking it to record a duplicate operation.
	operation := key + ":" + strings.ToLower(label)
	if !f.added[operation] {
		if err := f.FakeClient.AddLabel(org, repo, number, label); err != nil {
			return err
		}
		f.added[operation] = true
	}
	if !slices.ContainsFunc(f.labels[key], func(l github.Label) bool { return strings.EqualFold(l.Name, label) }) {
		f.labels[key] = append(f.labels[key], github.Label{Name: label})
	}
	return nil
}

func (f *LabelClient) RemoveLabel(org, repo string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := labelKey(org, repo, number)
	operation := key + ":" + strings.ToLower(label)
	if !f.removed[operation] {
		if err := f.FakeClient.RemoveLabel(org, repo, number, label); err != nil {
			return err
		}
		f.removed[operation] = true
	}
	f.labels[key] = slices.DeleteFunc(f.labels[key], func(l github.Label) bool { return strings.EqualFold(l.Name, label) })
	return nil
}

func (f *LabelClient) ResetLabels() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels = make(map[string][]github.Label)
	f.added = make(map[string]bool)
	f.removed = make(map[string]bool)
}

package dispatch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/config"
	"github.com/openshift-eng/ship-hook/pkg/testhelpers"
)

type fakePlugin struct{}

func (f *fakePlugin) Name() string { return "fake" }
func (f *fakePlugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, _ github.PullRequestEvent) HandlerResult {
	return HandlerResult{}
}
func (f *fakePlugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) HandlerResult {
	return HandlerResult{}
}
func (f *fakePlugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, _ github.ReviewEvent) HandlerResult {
	return HandlerResult{}
}

type recordingPlugin struct {
	name               string
	mu                 sync.Mutex
	prEvents           []github.PullRequestEvent
	issueCommentEvents []github.IssueCommentEvent
	reviewEvents       []github.ReviewEvent
}

func (r *recordingPlugin) Name() string { return r.name }

func (r *recordingPlugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, event github.PullRequestEvent) HandlerResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prEvents = append(r.prEvents, event)
	return Acted("")
}

func (r *recordingPlugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, event github.IssueCommentEvent) HandlerResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.issueCommentEvents = append(r.issueCommentEvents, event)
	return Acted("")
}

func (r *recordingPlugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, event github.ReviewEvent) HandlerResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviewEvents = append(r.reviewEvents, event)
	return Acted("")
}

func enabledResolver(pluginNames ...string) *config.Resolver {
	var plugins []config.PluginConfig
	for _, name := range pluginNames {
		plugins = append(plugins, config.PluginConfig{Name: name})
	}
	return config.NewResolver(&config.Config{Plugins: plugins})
}

func TestDispatcherRegister(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("fake"))
	d.Register(&fakePlugin{})

	if len(d.plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(d.plugins))
	}
	if d.plugins[0].Name() != "fake" {
		t.Errorf("expected plugin name 'fake', got %q", d.plugins[0].Name())
	}
}

func TestDispatcherHandlePullRequestEvent(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("plugin-1", "plugin-2"))
	p1 := &recordingPlugin{name: "plugin-1"}
	p2 := &recordingPlugin{name: "plugin-2"}
	d.Register(p1)
	d.Register(p2)

	event := testhelpers.NewPullRequestEvent("org", "repo", 1, "opened")
	d.HandlePullRequestEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	waitForEvents(t, func() int {
		p1.mu.Lock()
		p2.mu.Lock()
		defer p1.mu.Unlock()
		defer p2.mu.Unlock()
		if len(p1.prEvents) == 1 && len(p2.prEvents) == 1 {
			return 2
		}
		return 0
	}, 2)

	if p1.prEvents[0].Number != 1 {
		t.Errorf("plugin-1: expected PR #1, got #%d", p1.prEvents[0].Number)
	}
	if p2.prEvents[0].Number != 1 {
		t.Errorf("plugin-2: expected PR #1, got #%d", p2.prEvents[0].Number)
	}
}

func TestDispatcherHandleIssueCommentEvent(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("recorder"))
	p := &recordingPlugin{name: "recorder"}
	d.Register(p)

	event := testhelpers.NewIssueCommentEvent("org", "repo", 5, "/test")
	d.HandleIssueCommentEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	waitForEvents(t, func() int {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.issueCommentEvents)
	}, 1)

	if p.issueCommentEvents[0].Comment.Body != "/test" {
		t.Errorf("expected comment body '/test', got %q", p.issueCommentEvents[0].Comment.Body)
	}
}

func TestDispatcherDisabledPlugin(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("enabled-plugin"))
	enabled := &recordingPlugin{name: "enabled-plugin"}
	disabled := &recordingPlugin{name: "disabled-plugin"}
	d.Register(enabled)
	d.Register(disabled)

	event := testhelpers.NewPullRequestEvent("org", "repo", 1, "opened")
	d.HandlePullRequestEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	waitForEvents(t, func() int {
		enabled.mu.Lock()
		defer enabled.mu.Unlock()
		return len(enabled.prEvents)
	}, 1)

	// Give a short window for the disabled plugin to incorrectly receive events
	time.Sleep(50 * time.Millisecond)
	disabled.mu.Lock()
	defer disabled.mu.Unlock()
	if len(disabled.prEvents) != 0 {
		t.Errorf("disabled plugin should not have received events, got %d", len(disabled.prEvents))
	}
}

func TestDispatcherReviewEventGating(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("enabled"))
	enabled := &recordingPlugin{name: "enabled"}
	disabled := &recordingPlugin{name: "disabled"}
	d.Register(enabled)
	d.Register(disabled)

	event := testhelpers.NewReviewEvent("org", "repo", 1, "submitted", "user", "APPROVED")
	d.HandleReviewEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	waitForEvents(t, func() int {
		enabled.mu.Lock()
		defer enabled.mu.Unlock()
		return len(enabled.reviewEvents)
	}, 1)

	time.Sleep(50 * time.Millisecond)
	disabled.mu.Lock()
	defer disabled.mu.Unlock()
	if len(disabled.reviewEvents) != 0 {
		t.Errorf("disabled plugin should not have received review events, got %d", len(disabled.reviewEvents))
	}
}

func TestDispatcherShutdownWaitsForHandlers(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("slow"))

	started := make(chan struct{})
	blocking := &blockingPlugin{name: "slow", started: started, unblock: make(chan struct{})}
	d.Register(blocking)

	event := testhelpers.NewPullRequestEvent("org", "repo", 1, "opened")
	d.HandlePullRequestEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	<-started

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- d.Shutdown(context.Background())
	}()

	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned before handler finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(blocking.unblock)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Errorf("Shutdown returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after handler finished")
	}
}

func TestDispatcherShutdownCancelsContext(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("ctx-aware"))

	ctxCancelled := make(chan struct{})
	p := &contextAwarePlugin{
		name:         "ctx-aware",
		ctxCancelled: ctxCancelled,
	}
	d.Register(p)

	event := testhelpers.NewPullRequestEvent("org", "repo", 1, "opened")
	d.HandlePullRequestEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	d.cancel()

	select {
	case <-ctxCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not observe context cancellation")
	}
}

func TestDispatcherShutdownRespectsDeadline(t *testing.T) {
	d := NewDispatcher(logrus.NewEntry(logrus.StandardLogger()), enabledResolver("stuck"))

	started := make(chan struct{})
	blocking := &blockingPlugin{name: "stuck", started: started, unblock: make(chan struct{})}
	d.Register(blocking)

	event := testhelpers.NewPullRequestEvent("org", "repo", 1, "opened")
	d.HandlePullRequestEvent(logrus.NewEntry(logrus.StandardLogger()), event)

	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := d.Shutdown(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	close(blocking.unblock)
}

type blockingPlugin struct {
	name    string
	started chan struct{}
	unblock chan struct{}
}

func (b *blockingPlugin) Name() string { return b.name }
func (b *blockingPlugin) HandlePullRequestEvent(_ context.Context, _ *logrus.Entry, _ github.PullRequestEvent) HandlerResult {
	close(b.started)
	<-b.unblock
	return HandlerResult{}
}
func (b *blockingPlugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) HandlerResult {
	return HandlerResult{}
}
func (b *blockingPlugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, _ github.ReviewEvent) HandlerResult {
	return HandlerResult{}
}

type contextAwarePlugin struct {
	name         string
	ctxCancelled chan struct{}
}

func (c *contextAwarePlugin) Name() string { return c.name }
func (c *contextAwarePlugin) HandlePullRequestEvent(ctx context.Context, _ *logrus.Entry, _ github.PullRequestEvent) HandlerResult {
	<-ctx.Done()
	close(c.ctxCancelled)
	return HandlerResult{}
}
func (c *contextAwarePlugin) HandleIssueCommentEvent(_ context.Context, _ *logrus.Entry, _ github.IssueCommentEvent) HandlerResult {
	return HandlerResult{}
}
func (c *contextAwarePlugin) HandleReviewEvent(_ context.Context, _ *logrus.Entry, _ github.ReviewEvent) HandlerResult {
	return HandlerResult{}
}

func waitForEvents(t *testing.T, countFn func() int, expected int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if countFn() >= expected {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %d", expected, countFn())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

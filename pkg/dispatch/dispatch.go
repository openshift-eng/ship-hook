package dispatch

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/prow/pkg/github"

	"github.com/openshift-eng/ship-hook/pkg/config"
)

var pluginHandleDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "shiphook_plugin_handle_duration_seconds",
	Help:    "How long a sub-plugin took to handle an event.",
	Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
}, []string{"event_type", "plugin", "took_action"})

func init() {
	prometheus.MustRegister(pluginHandleDuration)
}

// HandlerResult is returned by sub-plugin handlers to report whether the event
// was relevant to the plugin.
type HandlerResult struct {
	Relevant bool
	Reason   string
}

func Irrelevant(reason string) HandlerResult {
	return HandlerResult{Relevant: false, Reason: reason}
}

func Handled(reason string) HandlerResult {
	return HandlerResult{Relevant: true, Reason: reason}
}

// SubPlugin defines the interface that all SHIP Hook sub-plugins must implement.
type SubPlugin interface {
	Name() string
	HandlePullRequestEvent(context.Context, *logrus.Entry, github.PullRequestEvent) HandlerResult
	HandleIssueCommentEvent(context.Context, *logrus.Entry, github.IssueCommentEvent) HandlerResult
	HandleReviewEvent(context.Context, *logrus.Entry, github.ReviewEvent) HandlerResult
}

// Dispatcher multiplexes GitHub webhook events to registered sub-plugins.
// It holds a cancellable context to bridge the gap between Prow's context-free
// handler signatures and sub-plugins that need shutdown signaling.
// See specs/005-graceful-shutdown.md for the rationale.
type Dispatcher struct {
	plugins  []SubPlugin
	resolver *config.Resolver
	logger   *logrus.Entry
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewDispatcher(logger *logrus.Entry, resolver *config.Resolver) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		resolver: resolver,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (d *Dispatcher) Register(p SubPlugin) {
	d.logger.WithField("plugin", p.Name()).Info("Registering sub-plugin")
	d.plugins = append(d.plugins, p)
}

func (d *Dispatcher) HandlePullRequestEvent(l *logrus.Entry, event github.PullRequestEvent) {
	org := event.Repo.Owner.Login
	repo := event.Repo.Name
	l = d.logger.WithFields(l.Data).WithFields(logrus.Fields{
		"event_type": "pull_request",
		"org":        org,
		"repo":       repo,
		"pr":         event.Number,
		"action":     string(event.Action),
	})
	l.Info("Received event")

	for _, p := range d.plugins {
		plugin := p
		if !d.resolver.IsEnabled(plugin.Name(), org, repo) {
			l.WithField("plugin", plugin.Name()).Trace("Plugin not enabled, skipping")
			continue
		}
		l.WithField("plugin", plugin.Name()).Debug("Dispatching to plugin")
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			start := time.Now()
			result := plugin.HandlePullRequestEvent(d.ctx, l.WithField("plugin", plugin.Name()), event)
			recordResult(l, "pull_request", plugin.Name(), result, time.Since(start))
		}()
	}
}

func (d *Dispatcher) HandleIssueCommentEvent(l *logrus.Entry, event github.IssueCommentEvent) {
	org := event.Repo.Owner.Login
	repo := event.Repo.Name
	l = d.logger.WithFields(l.Data).WithFields(logrus.Fields{
		"event_type": "issue_comment",
		"org":        org,
		"repo":       repo,
		"pr":         event.Issue.Number,
		"action":     string(event.Action),
	})
	l.Info("Received event")

	for _, p := range d.plugins {
		plugin := p
		if !d.resolver.IsEnabled(plugin.Name(), org, repo) {
			l.WithField("plugin", plugin.Name()).Trace("Plugin not enabled, skipping")
			continue
		}
		l.WithField("plugin", plugin.Name()).Debug("Dispatching to plugin")
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			start := time.Now()
			result := plugin.HandleIssueCommentEvent(d.ctx, l.WithField("plugin", plugin.Name()), event)
			recordResult(l, "issue_comment", plugin.Name(), result, time.Since(start))
		}()
	}
}

func (d *Dispatcher) HandleReviewEvent(l *logrus.Entry, event github.ReviewEvent) {
	org := event.Repo.Owner.Login
	repo := event.Repo.Name
	l = d.logger.WithFields(l.Data).WithFields(logrus.Fields{
		"event_type": "pull_request_review",
		"org":        org,
		"repo":       repo,
		"pr":         event.PullRequest.Number,
		"action":     string(event.Action),
	})
	l.Info("Received event")

	for _, p := range d.plugins {
		plugin := p
		if !d.resolver.IsEnabled(plugin.Name(), org, repo) {
			l.WithField("plugin", plugin.Name()).Trace("Plugin not enabled, skipping")
			continue
		}
		l.WithField("plugin", plugin.Name()).Debug("Dispatching to plugin")
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			start := time.Now()
			result := plugin.HandleReviewEvent(d.ctx, l.WithField("plugin", plugin.Name()), event)
			recordResult(l, "pull_request_review", plugin.Name(), result, time.Since(start))
		}()
	}
}

func recordResult(l *logrus.Entry, eventType, pluginName string, result HandlerResult, duration time.Duration) {
	tookAction := strconv.FormatBool(result.Relevant)
	pluginHandleDuration.WithLabelValues(eventType, pluginName, tookAction).Observe(duration.Seconds())

	entry := l.WithFields(logrus.Fields{
		"plugin":   pluginName,
		"relevant": result.Relevant,
		"duration": duration.String(),
	})
	if result.Reason != "" {
		entry = entry.WithField("reason", result.Reason)
	}
	if result.Relevant {
		entry.Info("Plugin completed")
	} else {
		entry.Debug("Plugin completed")
	}
}

// Shutdown signals all in-flight handlers to stop and waits for them to finish.
// Returns the context error if the provided context expires before all handlers complete.
func (d *Dispatcher) Shutdown(ctx context.Context) error {
	d.cancel()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	prowconfig "sigs.k8s.io/prow/pkg/config"
	"sigs.k8s.io/prow/pkg/flagutil"
	"sigs.k8s.io/prow/pkg/githubeventserver"
	"sigs.k8s.io/prow/pkg/interrupts"
	"sigs.k8s.io/prow/pkg/logrusutil"
	"sigs.k8s.io/prow/pkg/metrics"
	"sigs.k8s.io/prow/pkg/pjutil"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"

	"github.com/openshift-eng/ship-hook/pkg/authz"
	"github.com/openshift-eng/ship-hook/pkg/config"
	"github.com/openshift-eng/ship-hook/pkg/dispatch"
	"github.com/openshift-eng/ship-hook/pkg/featuregate"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/example"
	"github.com/openshift-eng/ship-hook/pkg/subplugins/readyforhumans"
)

type options struct {
	eventServerOptions     githubeventserver.Options
	instrumentationOptions flagutil.InstrumentationOptions
	github                 flagutil.GitHubOptions
	hmacSecretFile         string
	dryRun                 bool
	logLevel               string
	configPath             string
	supplementalDir        string
	cyborgCredentialsFile  string
}

func gatherOptions() options {
	o := options{}
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	o.eventServerOptions.Bind(fs)
	o.instrumentationOptions.AddFlags(fs)
	o.github.AddFlags(fs)
	fs.StringVar(&o.hmacSecretFile, "hmac-secret-file", "/etc/webhook/hmac", "Path to the file containing the GitHub HMAC secret")
	fs.BoolVar(&o.dryRun, "dry-run", true, "Dry run for testing (uses no mutations)")
	fs.StringVar(&o.logLevel, "log-level", "debug", "Log level (trace, debug, info, warn, error, fatal, panic)")
	fs.StringVar(&o.configPath, "config-path", "", "Path to the ship-hook config file")
	fs.StringVar(&o.supplementalDir, "supplemental-config-dir", "", "Path to a directory of supplemental config files")
	fs.StringVar(&o.cyborgCredentialsFile, "cyborg.credentials-file", "", "Path to GCP service account JSON for cyborg queries")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	return o
}

func (o *options) validate() error {
	level, err := logrus.ParseLevel(o.logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", o.logLevel, err)
	}
	logrus.SetLevel(level)

	if err := o.eventServerOptions.DefaultAndValidate(); err != nil {
		return fmt.Errorf("invalid event server options: %w", err)
	}

	return nil
}

func main() {
	logrusutil.ComponentInit()
	logger := logrus.WithField("component", "ship-hook")

	o := gatherOptions()
	if err := o.validate(); err != nil {
		logger.WithError(err).Fatal("Invalid options")
	}

	cfg, err := config.Load(o.configPath, o.supplementalDir)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load config")
	}
	resolver := config.NewResolver(cfg)

	reloadCtx, reloadCancel := context.WithCancel(context.Background())
	go config.StartReloadLoop(reloadCtx, resolver, o.configPath, o.supplementalDir, 5*time.Minute, logger)

	gates := featuregate.New(os.LookupEnv, featuregate.AllGates...)
	gates.LogStates(logger)

	policy := authz.NewPolicy().
		Allow(authz.KnownToCyborg(), "access-coderabbit", authz.Global())

	var identityResolver authz.IdentityResolver
	ctx, cyborgCancel := context.WithCancel(context.Background())
	defer cyborgCancel()
	if o.cyborgCredentialsFile == "" {
		logger.Warn("No cyborg credentials configured: all cyborg-dependent authorization checks will deny")
	} else {
		creds, err := os.ReadFile(o.cyborgCredentialsFile)
		if err != nil {
			logger.WithError(err).Fatal("Failed to read cyborg credentials file")
		}

		gcsSource, err := orgdatacore.NewGCSDataSourceWithSDK(ctx,
			"resolved-org", "orgdata/comprehensive_index_dump.json",
			orgdatacore.WithCredentialsJSON(string(creds)),
			orgdatacore.WithProjectID("openshift-crt"),
		)
		if err != nil {
			logger.WithError(err).Fatal("Failed to create cyborg GCS data source")
		}

		cyborgService := orgdatacore.NewService()
		if err := cyborgService.LoadFromDataSource(ctx, gcsSource); err != nil {
			logger.WithError(err).Fatal("Failed to load cyborg data")
		}
		logger.Info("Cyborg data loaded successfully")

		go func() {
			if err := cyborgService.StartDataSourceWatcher(ctx, gcsSource); err != nil {
				logger.WithError(err).Error("Cyborg data source watcher failed")
			}
		}()

		identityResolver = cyborgService
	}
	authorizer := authz.NewAuthorizer(policy, identityResolver)

	hmacTokenGenerator := func() []byte {
		data, err := os.ReadFile(o.hmacSecretFile)
		if err != nil {
			logger.WithError(err).Error("Failed to read HMAC secret file")
			return nil
		}
		return data
	}

	ghc, err := o.github.GitHubClient(o.dryRun)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create GitHub client")
	}

	metrics.ExposeMetrics("ship-hook", prowconfig.PushGateway{}, o.instrumentationOptions.MetricsPort)
	health := pjutil.NewHealthOnPort(o.instrumentationOptions.HealthPort)
	health.ServeReady()

	eventServer := githubeventserver.New(o.eventServerOptions, hmacTokenGenerator, logger)

	dispatcher := dispatch.NewDispatcher(logger, resolver)
	dispatcher.Register(example.New(ghc))
	dispatcher.Register(readyforhumans.New(ghc, resolver, gates, authorizer))

	eventServer.RegisterHandlePullRequestEvent(dispatcher.HandlePullRequestEvent)
	eventServer.RegisterHandleIssueCommentEvent(dispatcher.HandleIssueCommentEvent)
	eventServer.RegisterReviewEventHandler(dispatcher.HandleReviewEvent)

	interrupts.OnInterrupt(func() {
		reloadCancel()
		eventServer.GracefulShutdown()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := dispatcher.Shutdown(shutdownCtx); err != nil {
			logger.WithError(err).Error("Error waiting for in-flight handlers")
		}
		cyborgCancel()
		if err := eventServer.Shutdown(shutdownCtx); err != nil {
			logger.WithError(err).Error("Error shutting down event server")
		}
	})

	if err := eventServer.ListenAndServe(); err != nil {
		logger.WithError(err).Fatal("Event server failed")
	}
}

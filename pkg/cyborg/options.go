package cyborg

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
	"sigs.k8s.io/prow/pkg/config/secret"
)

// Options configures access to the Cyborg directory. An empty CredentialsFile
// leaves Cyborg access disabled.
type Options struct {
	CredentialsFile string

	credentials func() []byte
}

func (options *Options) AddFlags(flags *flag.FlagSet) {
	flags.StringVar(&options.CredentialsFile, "cyborg.credentials-file", options.CredentialsFile, "Path to GCP service account JSON for cyborg queries")
}

// Validate loads and registers the credentials with Prow's secret agent.
func (options *Options) Validate() error {
	if options.CredentialsFile == "" || options.credentials != nil {
		return nil
	}
	credentials, err := secret.AddWithParser(options.CredentialsFile, func(raw []byte) ([]byte, error) {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("cyborg credentials file must contain valid JSON")
		}
		return raw, nil
	})
	if err != nil {
		return fmt.Errorf("load Cyborg credentials: %w", err)
	}
	options.credentials = credentials
	return nil
}

// NewService loads the directory using the configured credentials. It returns
// nil when access is disabled. The caller must close the returned service.
// Credentials are captured during construction; changes require a restart.
func (options *Options) NewService(ctx context.Context) (Service, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if options.CredentialsFile == "" {
		return nil, nil
	}

	source, err := orgdatacore.NewGCSDataSourceWithSDK(ctx,
		"resolved-org", "orgdata/comprehensive_index_dump.json",
		orgdatacore.WithCredentialsJSON(string(options.credentials())),
		orgdatacore.WithProjectID("openshift-crt"),
	)
	if err != nil {
		return nil, fmt.Errorf("create Cyborg GCS data source: %w", err)
	}
	return NewService(ctx, source)
}

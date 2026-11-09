package cyborg

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/prow/pkg/config/secret"
)

func credentialsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOptionsAddFlags(t *testing.T) {
	options := Options{}
	flags := flag.NewFlagSet("cyborg", flag.ContinueOnError)
	options.AddFlags(flags)
	if options.CredentialsFile != "" {
		t.Fatal("Cyborg should be disabled by default")
	}
	path := credentialsFile(t, `{}`)
	if err := flags.Parse([]string{"--cyborg.credentials-file=" + path}); err != nil {
		t.Fatal(err)
	}
	if options.CredentialsFile != path {
		t.Errorf("credentials file = %q, want %q", options.CredentialsFile, path)
	}
}

func TestOptionsValidate(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path string
		fail bool
	}{
		{name: "disabled"},
		{name: "JSON credentials", path: credentialsFile(t, `{"type":"service_account"}`)},
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing"), fail: true},
		{name: "directory", path: t.TempDir(), fail: true},
		{name: "invalid JSON", path: credentialsFile(t, "{"), fail: true},
		{name: "empty file", path: credentialsFile(t, ""), fail: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			options := Options{CredentialsFile: testCase.path}
			if err := options.Validate(); (err != nil) != testCase.fail {
				t.Errorf("Validate() = %v, want error %v", err, testCase.fail)
			}
			if !testCase.fail && testCase.path != "" {
				raw := secret.GetSecret(testCase.path)
				if string(raw) != `{"type":"service_account"}` {
					t.Fatalf("secret agent credentials = %q, want registered JSON", raw)
				}
				if string(secret.Censor(raw)) == string(raw) {
					t.Fatal("registered credentials were not censored")
				}
			}
			if testCase.fail {
				client, err := options.NewService(context.Background())
				if err == nil || client != nil {
					t.Fatalf("NewService() = %v, %v; expected validation failure", client, err)
				}
			}
		})
	}
}

func TestOptionsNewServiceDisabled(t *testing.T) {
	options := Options{}
	client, err := options.NewService(context.Background())
	if client != nil || err != nil {
		t.Fatalf("disabled NewService() = %v, %v; want nil, nil", client, err)
	}
}

func TestOptionsNewServiceInvalidCredentials(t *testing.T) {
	options := Options{CredentialsFile: credentialsFile(t, `{"type":"unsupported"}`)}
	client, err := options.NewService(context.Background())
	if client != nil || err == nil || !strings.Contains(err.Error(), "create Cyborg GCS data source") {
		t.Fatalf("NewService() = %v, %v; expected credential construction failure", client, err)
	}
}

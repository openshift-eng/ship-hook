package cyborg

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

func TestNormalizeGitHubIDs(t *testing.T) {
	data := orgdatacore.CreateTestData()
	data.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{
		"GhUser1": "testuser1", "GHUSER1": "testuser1",
		"GitHubAlias": "testuser1", "GHUSER2": "testuser2",
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	source := orgdatacore.NewFakeDataSource(string(raw))
	normalized := NormalizeGitHubIDs(source)
	reader, err := normalized.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	var got orgdatacore.Data
	if err := json.NewDecoder(reader).Decode(&got); err != nil {
		t.Fatal(err)
	}
	expected := *data
	expected.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{
		"ghuser1": "testuser1", "githubalias": "testuser1", "ghuser2": "testuser2",
	}
	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("normalized data mismatch (-want +got):\n%s", diff)
	}
	if normalized.String() != source.String() {
		t.Error("source description was not preserved")
	}
	if err := normalized.Close(); err != nil {
		t.Fatal(err)
	}
	if !source.CloseCalled {
		t.Error("source was not closed")
	}
}

func TestNormalizeGitHubIDsErrors(t *testing.T) {
	loadErr := errors.New("source unavailable")
	for _, tc := range []struct {
		name string
		data string
		err  error
	}{
		{name: "source error", err: loadErr},
		{name: "invalid JSON", data: "{"},
		{
			name: "conflicting case variants",
			data: `{"indexes":{"github_id_mappings":{"github_id_to_uid":{"Alice":"one","ALICE":"two"}}}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := orgdatacore.NewFakeDataSource(tc.data)
			source.LoadError = tc.err
			reader, err := NormalizeGitHubIDs(source).Load(context.Background())
			if err == nil || reader != nil {
				t.Fatalf("expected failed load, got reader %v, error %v", reader, err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Errorf("expected source error %v, got %v", tc.err, err)
			}
		})
	}
}

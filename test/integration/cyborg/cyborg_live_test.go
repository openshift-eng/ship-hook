//go:build integration && cyborg_live

package cyborg

import (
	"context"
	"testing"
	"time"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

func TestCyborgLiveGCSLookup(t *testing.T) {
	ctx := context.Background()

	gcsSource, err := orgdatacore.NewGCSDataSourceWithSDK(ctx,
		"resolved-org", "orgdata/comprehensive_index_dump.json",
		orgdatacore.WithProjectID("openshift-crt"),
		orgdatacore.WithCheckInterval(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("Failed to create GCS data source: %v", err)
	}
	defer gcsSource.Close()

	service := orgdatacore.NewService()
	if err := service.LoadFromDataSource(ctx, gcsSource); err != nil {
		t.Fatalf("Failed to load cyborg data: %v", err)
	}

	version := service.GetVersion()
	t.Logf("Loaded cyborg data: %d employees, %d orgs", version.EmployeeCount, version.OrgCount)

	if version.EmployeeCount == 0 {
		t.Fatal("Expected non-zero employee count")
	}

	if emp := service.GetEmployeeByGitHubID("definitely-not-a-real-github-user-12345"); emp != nil {
		t.Errorf("Expected nil for non-existent GitHub user, got %+v", emp)
	}
}

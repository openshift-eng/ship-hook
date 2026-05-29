//go:build integration

package cyborg

import (
	"context"
	"testing"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"

	"github.com/openshift-eng/ship-hook/pkg/permissions"
)

func TestCyborgEmployeeLookup(t *testing.T) {
	ctx := context.Background()

	source := orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())
	service := orgdatacore.NewService()
	if err := service.LoadFromDataSource(ctx, source); err != nil {
		t.Fatalf("Failed to load cyborg data: %v", err)
	}

	version := service.GetVersion()
	t.Logf("Loaded cyborg data: %d employees, %d orgs", version.EmployeeCount, version.OrgCount)

	if emp := service.GetEmployeeByGitHubID("ghuser1"); emp == nil {
		t.Fatal("Expected employee for ghuser1, got nil")
	} else if emp.UID != "testuser1" {
		t.Errorf("Expected UID testuser1, got %s", emp.UID)
	}

	if emp := service.GetEmployeeByGitHubID("nonexistent"); emp != nil {
		t.Errorf("Expected nil for nonexistent GitHub user, got %+v", emp)
	}
}

func TestCyborgPermissionsIntegration(t *testing.T) {
	ctx := context.Background()

	source := orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())
	service := orgdatacore.NewService()
	if err := service.LoadFromDataSource(ctx, source); err != nil {
		t.Fatalf("Failed to load cyborg data: %v", err)
	}

	perms := permissions.NewStub(func(login string) bool {
		return service.GetEmployeeByGitHubID(login) != nil
	})

	if !perms.IsAllowed("ghuser1", "access", "coderabbit") {
		t.Error("Expected ghuser1 (known employee) to have coderabbit access")
	}
	if perms.IsAllowed("outsider", "access", "coderabbit") {
		t.Error("Expected outsider to not have coderabbit access")
	}
}

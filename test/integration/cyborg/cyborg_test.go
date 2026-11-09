//go:build integration

package cyborg

import (
	"context"
	"encoding/json"
	"testing"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"

	"github.com/openshift-eng/ship-hook/pkg/cyborg"
	"github.com/openshift-eng/ship-hook/pkg/permissions"
)

func TestCyborgEmployeeLookup(t *testing.T) {
	ctx := context.Background()

	source := orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())
	service, err := cyborg.NewService(ctx, source)
	if err != nil {
		t.Fatalf("Failed to load cyborg data: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})

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
	service, err := cyborg.NewService(ctx, source)
	if err != nil {
		t.Fatalf("Failed to load cyborg data: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})

	knownRedHatter := func(login string) bool {
		return service.GetEmployeeByGitHubID(login) != nil
	}
	perms := permissions.NewStub(map[permissions.Operation]func(string) bool{
		{Action: "access", Resource: "coderabbit"}: knownRedHatter,
		{Action: "approve", Resource: "pr"}:        knownRedHatter,
	})

	if !perms.IsAllowed("ghuser1", "access", "coderabbit") {
		t.Error("Expected ghuser1 (known employee) to have coderabbit access")
	}
	if perms.IsAllowed("outsider", "access", "coderabbit") {
		t.Error("Expected outsider to not have coderabbit access")
	}
	if !perms.IsAllowed("ghuser1", "approve", "pr") {
		t.Error("Expected ghuser1 (known employee) to be allowed to approve PRs")
	}
	if perms.IsAllowed("outsider", "approve", "pr") {
		t.Error("Expected outsider to not be allowed to approve PRs")
	}
}

type updatingDataSource struct {
	*orgdatacore.FakeDataSource
	next string
}

func (source *updatingDataSource) Watch(ctx context.Context, callback func() error) error {
	if err := source.FakeDataSource.Watch(ctx, callback); err != nil {
		return err
	}
	source.Data = source.next
	return callback()
}

func TestCyborgMixedCaseLookupAndReload(t *testing.T) {
	ctx := context.Background()
	data := orgdatacore.CreateTestData()
	data.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{
		"GhUsEr1": "testuser1", "ALias": "testuser1", "GHUSER2": "testuser2",
	}
	initial, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	data.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{
		"GhUser3": "testuser1", "GhUser2": "testuser2",
	}
	next, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	source := &updatingDataSource{
		FakeDataSource: orgdatacore.NewFakeDataSource(string(initial)), next: string(next),
	}
	service, err := cyborg.NewService(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, login := range []string{"ghuser1", "GHUSER1", "GhUser1", "aLiAs", "ghuser2"} {
		if service.GetEmployeeByGitHubID(login) == nil {
			t.Errorf("mixed-case account %q was not recognized", login)
		}
	}
	if service.GetEmployeeByGitHubID("outsider") != nil {
		t.Error("unknown GitHub account was incorrectly recognized")
	}
	if err := service.Watch(ctx); err != nil {
		t.Fatal(err)
	}
	if !source.WatchCalled {
		t.Fatal("watcher was not forwarded to the underlying data source")
	}
	if service.GetEmployeeByGitHubID("gHuSeR3") == nil {
		t.Error("watcher reload did not recognize the newly added account")
	}
	for _, login := range []string{"GHUSER1", "ALIAS"} {
		if service.GetEmployeeByGitHubID(login) != nil {
			t.Errorf("account %q removed by the reload was still recognized", login)
		}
	}
}

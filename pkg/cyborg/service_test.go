package cyborg

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

func TestService(t *testing.T) {
	source := orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())
	client, err := NewService(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if employee := client.GetEmployeeByGitHubID("GHUSER1"); employee == nil || employee.UID != "testuser1" {
		t.Fatalf("unexpected employee: %+v", employee)
	}
	if employee := client.GetEmployeeByGitHubID("outsider"); employee != nil {
		t.Fatalf("unexpected employee for outsider: %+v", employee)
	}
	if err := client.Watch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !source.WatchCalled {
		t.Fatal("directory source is not being watched")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if !source.CloseCalled {
		t.Fatal("directory source was not closed")
	}
}

func TestServiceLoadFailure(t *testing.T) {
	loadErr := errors.New("source unavailable")
	for _, testCase := range []struct {
		name string
		data string
		err  error
	}{
		{name: "source unavailable", err: loadErr},
		{name: "invalid data", data: "{"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := orgdatacore.NewFakeDataSource(testCase.data)
			source.LoadError = testCase.err
			client, err := NewService(context.Background(), source)
			if client != nil || err == nil {
				t.Fatalf("NewService() = %v, %v; expected load failure", client, err)
			}
			if testCase.err != nil && !errors.Is(err, testCase.err) {
				t.Errorf("load error was not preserved: %v", err)
			}
			if !source.CloseCalled {
				t.Error("failed construction leaked the data source")
			}
		})
	}
}

func TestServiceWatchFailure(t *testing.T) {
	watchErr := errors.New("watch unavailable")
	source := orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON())
	source.WatchError = watchErr
	client, err := NewService(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Watch(context.Background()); !errors.Is(err, watchErr) {
		t.Errorf("Watch() = %v, want %v", err, watchErr)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

type updatingSource struct {
	*orgdatacore.FakeDataSource
	next    string
	updated chan struct{}
}

func (source *updatingSource) Watch(ctx context.Context, callback func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source.Data = source.next
	if err := callback(); err != nil {
		return err
	}
	close(source.updated)
	<-ctx.Done()
	return ctx.Err()
}

func TestServiceUpdatesAndCancellation(t *testing.T) {
	for _, stop := range []string{"watch context", "close"} {
		t.Run(stop, func(t *testing.T) {
			data := orgdatacore.CreateTestData()
			data.Indexes.GitHubIDMappings.GitHubIDToUID = map[string]string{"New-Account": "testuser1"}
			next, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			source := &updatingSource{
				FakeDataSource: orgdatacore.NewFakeDataSource(orgdatacore.CreateTestDataJSON()),
				next:           string(next), updated: make(chan struct{}),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			loadCtx, finishLoad := context.WithCancel(context.Background())
			client, err := NewService(loadCtx, source)
			finishLoad()
			if err != nil {
				t.Fatal(err)
			}
			watchDone := make(chan error, 1)
			go func() { watchDone <- client.Watch(ctx) }()
			select {
			case <-source.updated:
			case err := <-watchDone:
				t.Fatalf("directory watcher stopped before updating: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("directory update did not complete")
			}
			if employee := client.GetEmployeeByGitHubID("NEW-ACCOUNT"); employee == nil || employee.UID != "testuser1" {
				t.Errorf("updated account was not recognized: %+v", employee)
			}
			if employee := client.GetEmployeeByGitHubID("ghuser1"); employee != nil {
				t.Error("removed account was still recognized")
			}
			if stop == "watch context" {
				cancel()
			} else if err := client.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-watchDone:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("Watch() = %v, want context cancellation", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("directory watcher did not stop")
			}
			if stop == "watch context" {
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

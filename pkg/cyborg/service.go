package cyborg

import (
	"context"
	"errors"
	"fmt"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

// Queries is the directory query interface used by ship-hook. The upstream
// service implements it directly.
type Queries interface {
	GetEmployeeByGitHubID(login string) *orgdatacore.Employee
}

// Service provides directory queries and updates from its configured source.
// Watch may block; run it in a separate goroutine and cancel its context before
// closing the service.
type Service interface {
	Queries
	Watch(ctx context.Context) error
	Close() error
}

type service struct {
	*orgdatacore.Service
	source orgdatacore.DataSource
}

// NewService loads a directory from source and takes ownership of that source,
// closing it on construction failure or when the returned service is closed.
func NewService(ctx context.Context, source orgdatacore.DataSource) (Service, error) {
	directory := orgdatacore.NewService()
	if err := directory.LoadFromDataSource(ctx, source); err != nil {
		return nil, errors.Join(fmt.Errorf("load Cyborg data: %w", err), source.Close())
	}
	return &service{Service: directory, source: source}, nil
}

func (service *service) Watch(ctx context.Context) error {
	return service.StartDataSourceWatcher(ctx, service.source)
}

func (service *service) Close() error {
	service.StopWatcher()
	return service.source.Close()
}

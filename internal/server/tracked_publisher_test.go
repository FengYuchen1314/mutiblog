package server

import (
	"context"
	"errors"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type publisherFunction func(context.Context) (publisher.BuildReport, error)

func (function publisherFunction) Build(ctx context.Context) (publisher.BuildReport, error) {
	return function(ctx)
}

func TestTrackedPublisherExposesRealState(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	tracked := newTrackedSitePublisher(publisherFunction(func(context.Context) (publisher.BuildReport, error) {
		close(started)
		<-release
		return publisher.BuildReport{}, nil
	}))
	done := make(chan struct{})
	go func() {
		_, _ = tracked.Build(context.Background())
		close(done)
	}()
	<-started
	if status := tracked.Status(); status != "building" {
		t.Fatalf("active status = %q", status)
	}
	close(release)
	<-done
	if status := tracked.Status(); status != "idle" {
		t.Fatalf("completed status = %q", status)
	}

	failed := newTrackedSitePublisher(publisherFunction(func(context.Context) (publisher.BuildReport, error) {
		return publisher.BuildReport{}, errors.New("failed")
	}))
	_, _ = failed.Build(context.Background())
	if status := failed.Status(); status != "error" {
		t.Fatalf("failed status = %q", status)
	}
}

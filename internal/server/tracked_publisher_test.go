package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type closablePublisher struct{ closed atomic.Bool }

func (*closablePublisher) Build(context.Context) (publisher.BuildReport, error) {
	return publisher.BuildReport{}, nil
}

func (publisher *closablePublisher) Close() { publisher.closed.Store(true) }

type publisherFunction func(context.Context) (publisher.BuildReport, error)

func (function publisherFunction) Build(ctx context.Context) (publisher.BuildReport, error) {
	return function(ctx)
}

type prepareContextKey struct{}

type preparingPublisher struct{}

func (preparingPublisher) PrepareBuild(ctx context.Context) (context.Context, error) {
	return context.WithValue(ctx, prepareContextKey{}, "prepared"), nil
}

func (preparingPublisher) Build(context.Context) (publisher.BuildReport, error) {
	return publisher.BuildReport{}, nil
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

func TestTrackedPublisherForwardsClose(t *testing.T) {
	delegate := &closablePublisher{}
	newTrackedSitePublisher(delegate).Close()
	if !delegate.closed.Load() {
		t.Fatal("tracked publisher did not close its delegate")
	}
}

func TestTrackedPublisherExposesOptionalDurableBuildPreparation(t *testing.T) {
	ctx := context.Background()
	prepared, err := newTrackedSitePublisher(preparingPublisher{}).PrepareBuild(ctx)
	if err != nil || prepared.Value(prepareContextKey{}) != "prepared" {
		t.Fatalf("prepared context = %#v, %v", prepared.Value(prepareContextKey{}), err)
	}

	unchanged, err := newTrackedSitePublisher(publisherFunction(func(context.Context) (publisher.BuildReport, error) {
		return publisher.BuildReport{}, nil
	})).PrepareBuild(ctx)
	if err != nil || unchanged != ctx {
		t.Fatalf("unsupported preparation changed context: %#v, %v", unchanged, err)
	}
}

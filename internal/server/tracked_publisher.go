package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/FengYuchen1314/mutiblog/internal/publisher"
)

type trackedSitePublisher struct {
	delegate SitePublisher
	gate     sync.Mutex
	active   atomic.Int64
	state    atomic.Value
}

func (p *trackedSitePublisher) Preview(ctx context.Context, themeID, baseURL string) (publisher.PreviewRecord, error) {
	p.gate.Lock()
	defer p.gate.Unlock()

	delegate, ok := p.delegate.(interface {
		Preview(context.Context, string, string) (publisher.PreviewRecord, error)
	})
	if !ok {
		return publisher.PreviewRecord{}, errors.New("theme previews are unavailable")
	}
	p.active.Add(1)
	p.state.Store("building")
	record, err := delegate.Preview(ctx, themeID, baseURL)
	remaining := p.active.Add(-1)
	if remaining > 0 {
		p.state.Store("building")
	} else if err != nil {
		p.state.Store("error")
	} else {
		p.state.Store("idle")
	}
	return record, err
}

func (p *trackedSitePublisher) PreviewRoot(id string) (publisher.PreviewRecord, string, error) {
	delegate, ok := p.delegate.(interface {
		PreviewRoot(string) (publisher.PreviewRecord, string, error)
	})
	if !ok {
		return publisher.PreviewRecord{}, "", publisher.ErrPreviewNotFound
	}
	return delegate.PreviewRoot(id)
}

func newTrackedSitePublisher(delegate SitePublisher) *trackedSitePublisher {
	tracked := &trackedSitePublisher{delegate: delegate}
	tracked.state.Store("idle")
	return tracked
}

func (p *trackedSitePublisher) Build(ctx context.Context) (publisher.BuildReport, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	return p.buildWhileLocked(ctx)
}

func (p *trackedSitePublisher) buildWhileLocked(ctx context.Context) (publisher.BuildReport, error) {
	p.active.Add(1)
	p.state.Store("building")
	report, err := p.delegate.Build(ctx)
	remaining := p.active.Add(-1)
	if remaining > 0 {
		p.state.Store("building")
	} else if err != nil {
		p.state.Store("error")
	} else {
		p.state.Store("idle")
	}
	return report, err
}

// lockBuildTransaction lets a theme transaction replace its private package,
// run the same tracked build, and commit or roll back before any background
// publication can observe the intermediate theme state.
func (p *trackedSitePublisher) lockBuildTransaction() (func(context.Context) (publisher.BuildReport, error), func()) {
	p.gate.Lock()
	return p.buildWhileLocked, p.gate.Unlock
}

func lockPublisherTransaction(sitePublisher SitePublisher) (func(context.Context) (publisher.BuildReport, error), func()) {
	if tracked, ok := sitePublisher.(*trackedSitePublisher); ok {
		return tracked.lockBuildTransaction()
	}
	return sitePublisher.Build, func() {}
}

func (p *trackedSitePublisher) Status() string {
	status, _ := p.state.Load().(string)
	if status == "" {
		return "idle"
	}
	return status
}

func publisherStatus(sitePublisher SitePublisher) string {
	if tracked, ok := sitePublisher.(interface{ Status() string }); ok {
		return tracked.Status()
	}
	return "idle"
}

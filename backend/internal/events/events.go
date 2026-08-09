// Package events is the small, synchronous-in-process hook bus.
package events

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fengyuchen/mutiblog/internal/model"
)

type Event interface{ Name() string }
type Handler func(context.Context, Event) error
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]Handler
	log  *slog.Logger
}

func New(log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	return &Bus{subs: map[string][]Handler{}, log: log}
}
func (b *Bus) Subscribe(name string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[name] = append(b.subs[name], h)
}
func (b *Bus) handlers(name string) []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]Handler(nil), b.subs[name]...)
}
func (b *Bus) Publish(ctx context.Context, e Event) {
	for _, h := range b.handlers(e.Name()) {
		go func(h Handler) {
			if err := h(ctx, e); err != nil {
				b.log.Error("event handler failed", "event", e.Name(), "err", err)
			}
		}(h)
	}
}
func (b *Bus) PublishSync(ctx context.Context, e Event) error {
	for _, h := range b.handlers(e.Name()) {
		if err := h(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

type ArticleCreated struct {
	ID     model.ArticleID
	Type   model.ContentType
	Locale model.Locale
}

func (ArticleCreated) Name() string { return "ArticleCreated" }

type ArticleUpdated struct {
	ID            model.ArticleID
	Locale        model.Locale
	BodyChanged   bool
	Before, After *model.Article
}

func (ArticleUpdated) Name() string { return "ArticleUpdated" }

type ArticlePublished struct {
	ID           model.ArticleID
	Locale       model.Locale
	FirstPublish bool
}

func (ArticlePublished) Name() string { return "ArticlePublished" }

type ArticleUnpublished struct {
	ID     model.ArticleID
	Locale model.Locale
}

func (ArticleUnpublished) Name() string { return "ArticleUnpublished" }

type ArticleDeleted struct {
	ID   model.ArticleID
	Type model.ContentType
}

func (ArticleDeleted) Name() string { return "ArticleDeleted" }

type BundleChanged struct{ Dir string }

func (BundleChanged) Name() string { return "BundleChanged" }

type BundleRemoved struct{ Dir string }

func (BundleRemoved) Name() string { return "BundleRemoved" }

type TaxonomyChanged struct{ Kind string }

func (TaxonomyChanged) Name() string { return "TaxonomyChanged" }

type ConfigChanged struct{}

func (ConfigChanged) Name() string { return "ConfigChanged" }

type ThemeDistChanged struct{}

func (ThemeDistChanged) Name() string { return "ThemeDistChanged" }

type CategoryChanged struct{ ID, Op string }

func (CategoryChanged) Name() string { return "CategoryChanged" }

type TagChanged struct{ ID, Op string }

func (TagChanged) Name() string { return "TagChanged" }

type LinkChanged struct{ ID, Op string }

func (LinkChanged) Name() string { return "LinkChanged" }

type MenuChanged struct{ ID string }

func (MenuChanged) Name() string { return "MenuChanged" }

type SettingsChanged struct{ Keys []string }

func (SettingsChanged) Name() string { return "SettingsChanged" }

type ThemeChanged struct{ Theme string }

func (ThemeChanged) Name() string { return "ThemeChanged" }

type ThemeSettingsChanged struct{ Theme string }

func (ThemeSettingsChanged) Name() string { return "ThemeSettingsChanged" }

type BeforeRender struct{ Unit, Props any }

func (BeforeRender) Name() string { return "BeforeRender" }

type AfterRender struct {
	Unit any
	HTML *string
}

func (AfterRender) Name() string { return "AfterRender" }

type RenderCompleted struct {
	Units    int
	Duration time.Duration
}

func (RenderCompleted) Name() string { return "RenderCompleted" }

type MediaUploaded struct {
	Path string
	Size int64
}

func (MediaUploaded) Name() string { return "MediaUploaded" }

type MediaDeleted struct{ Path string }

func (MediaDeleted) Name() string { return "MediaDeleted" }

type TranslationStarted struct {
	ArticleID model.ArticleID
	Target    model.Locale
}

func (TranslationStarted) Name() string { return "TranslationStarted" }

type TranslationCompleted struct {
	ArticleID model.ArticleID
	Target    model.Locale
	Tokens    int
}

func (TranslationCompleted) Name() string { return "TranslationCompleted" }

type TranslationFailed struct {
	ArticleID model.ArticleID
	Target    model.Locale
	Err       error
}

func (TranslationFailed) Name() string { return "TranslationFailed" }

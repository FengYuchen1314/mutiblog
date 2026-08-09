// Package index provides the in-memory query views over canonical content files.
package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
)

type BodyMode int

const (
	BodyResident BodyMode = iota
	BodyLazy
)

type Options struct {
	ContentRoot            string
	BodyResidentLimitBytes int64
	ForceMode              string
}
type typeLocKey struct {
	typ    model.ContentType
	locale model.Locale
}
type slugKey struct {
	typ    model.ContentType
	locale model.Locale
	slug   string
}
type CategoryNode struct {
	Category *model.Category
	Children []*CategoryNode
}
type Stats struct {
	Articles, Posts, Pages, Locales int
	Generation                      uint64
	BodyMode                        BodyMode
	BodyBytes                       int64
}
type ListQuery struct {
	Type                  model.ContentType
	Locale                model.Locale
	Status                []model.Status
	Category, Tag, Author string
	Year, Month           int
	Search                string
	TransStatus           model.TranslationStatus
	Sort                  string
	Page, PerPage         int
}
type Index struct {
	mu         sync.RWMutex
	options    Options
	articles   map[model.ArticleID]*model.Article
	byTypeLoc  map[typeLocKey][]model.ArticleID
	slugIdx    map[slugKey]model.ArticleID
	byCategory map[string][]model.ArticleID
	byTag      map[string][]model.ArticleID
	archive    map[model.Locale]map[int]map[int][]model.ArticleID
	categories map[string]*model.Category
	catTree    []*CategoryNode
	tags       map[string]*model.Tag
	links      map[string]*model.Link
	linkGroups []*model.LinkGroup
	menus      map[string]*model.Menu
	users      map[string]*model.User
	errors     []content.ScanError
	stats      Stats
}

func New(opts Options) *Index {
	if opts.BodyResidentLimitBytes == 0 {
		opts.BodyResidentLimitBytes = 64 << 20
	}
	return &Index{options: opts, articles: map[model.ArticleID]*model.Article{}, byTypeLoc: map[typeLocKey][]model.ArticleID{}, slugIdx: map[slugKey]model.ArticleID{}, byCategory: map[string][]model.ArticleID{}, byTag: map[string][]model.ArticleID{}, archive: map[model.Locale]map[int]map[int][]model.ArticleID{}, categories: map[string]*model.Category{}, tags: map[string]*model.Tag{}, links: map[string]*model.Link{}, menus: map[string]*model.Menu{}, users: map[string]*model.User{}}
}
func (ix *Index) RebuildAll(ctx context.Context, store *content.Store, tax *taxonomy.Store) error {
	articles, errs, err := store.ScanAll(ctx)
	if err != nil {
		return err
	}
	result := tax.LoadAll()
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.articles = map[model.ArticleID]*model.Article{}
	ix.categories = result.Categories
	ix.tags = result.Tags
	ix.links = result.Links
	ix.linkGroups = result.LinkGroups
	ix.menus = result.Menus
	ix.users = result.Users
	ix.errors = append(errs, scanErrors(result.Errors)...)
	for _, article := range articles {
		ix.articles[article.ID] = article
	}
	ix.rebuildViewsLocked()
	return nil
}
func scanErrors(errs []error) []content.ScanError {
	out := make([]content.ScanError, len(errs))
	for i, err := range errs {
		out[i] = content.ScanError{Err: err}
	}
	return out
}
func (ix *Index) UpsertArticle(a *model.Article) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.articles[a.ID] = a
	ix.rebuildViewsLocked()
}
func (ix *Index) RemoveArticle(id model.ArticleID) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	delete(ix.articles, id)
	ix.rebuildViewsLocked()
}
func (ix *Index) RemoveBundle(bundle string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for id, article := range ix.articles {
		if article.BundleDir == bundle {
			delete(ix.articles, id)
			break
		}
	}
	ix.rebuildViewsLocked()
}
func (ix *Index) ReloadTaxonomy(store *taxonomy.Store) error {
	result := store.LoadAll()
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.categories = result.Categories
	ix.tags = result.Tags
	ix.links = result.Links
	ix.linkGroups = result.LinkGroups
	ix.menus = result.Menus
	ix.users = result.Users
	ix.rebuildViewsLocked()
	return nil
}
func (ix *Index) rebuildViewsLocked() {
	ix.byTypeLoc = map[typeLocKey][]model.ArticleID{}
	ix.slugIdx = map[slugKey]model.ArticleID{}
	ix.byCategory = map[string][]model.ArticleID{}
	ix.byTag = map[string][]model.ArticleID{}
	ix.archive = map[model.Locale]map[int]map[int][]model.ArticleID{}
	ix.catTree = buildTree(ix.categories)
	var bodyBytes int64
	locales := map[model.Locale]bool{}
	for id, article := range ix.articles {
		for loc, version := range article.Versions {
			locales[loc] = true
			key := typeLocKey{article.Type, loc}
			ix.byTypeLoc[key] = append(ix.byTypeLoc[key], id)
			ix.slugIdx[slugKey{article.Type, loc, version.Front.Slug}] = id
			for _, category := range ancestors(version.Front.Categories, ix.categories) {
				ix.byCategory[category] = append(ix.byCategory[category], id)
			}
			for _, tag := range version.Front.Tags {
				ix.byTag[tag] = append(ix.byTag[tag], id)
			}
			year, month := version.Front.Date.Year(), int(version.Front.Date.Month())
			if ix.archive[loc] == nil {
				ix.archive[loc] = map[int]map[int][]model.ArticleID{}
			}
			if ix.archive[loc][year] == nil {
				ix.archive[loc][year] = map[int][]model.ArticleID{}
			}
			ix.archive[loc][year][month] = append(ix.archive[loc][year][month], id)
			bodyBytes += int64(len(version.Body))
		}
	}
	for key, ids := range ix.byTypeLoc {
		sort.SliceStable(ids, func(i, j int) bool { return newer(ix.articles[ids[i]], ix.articles[ids[j]], key.locale) })
		ix.byTypeLoc[key] = ids
	}
	mode := BodyResident
	if ix.options.ForceMode == "lazy" || (ix.options.ForceMode == "" && bodyBytes > ix.options.BodyResidentLimitBytes) {
		mode = BodyLazy
		for _, article := range ix.articles {
			for _, version := range article.Versions {
				version.Body = ""
			}
		}
	}
	if ix.options.ForceMode == "resident" {
		mode = BodyResident
	}
	posts, pages := 0, 0
	for _, article := range ix.articles {
		if article.Type == model.ContentPost {
			posts++
		} else {
			pages++
		}
	}
	ix.stats = Stats{Articles: len(ix.articles), Posts: posts, Pages: pages, Locales: len(locales), Generation: ix.stats.Generation + 1, BodyMode: mode, BodyBytes: bodyBytes}
}
func newer(a, b *model.Article, loc model.Locale) bool {
	av, bv := a.Versions[loc], b.Versions[loc]
	if av.Front.Pinned != bv.Front.Pinned {
		return av.Front.Pinned
	}
	if !av.Front.Date.Equal(bv.Front.Date) {
		return av.Front.Date.After(bv.Front.Date)
	}
	// Index maps have intentionally unspecified iteration order. A stable
	// tie-breaker keeps list pages, RSS, search JSON, and verify byte-stable.
	return string(a.ID) < string(b.ID)
}
func ancestors(cats []string, all map[string]*model.Category) []string {
	seen := map[string]bool{}
	for _, id := range cats {
		for id != "" && !seen[id] {
			seen[id] = true
			c := all[id]
			if c == nil {
				break
			}
			id = c.Parent
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}
func buildTree(cats map[string]*model.Category) []*CategoryNode {
	nodes := map[string]*CategoryNode{}
	for id, c := range cats {
		nodes[id] = &CategoryNode{Category: c}
	}
	var roots []*CategoryNode
	for id, n := range nodes {
		if p := cats[id].Parent; p != "" && nodes[p] != nil {
			nodes[p].Children = append(nodes[p].Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Category.Order < roots[j].Category.Order })
	return roots
}
func (ix *Index) Article(id model.ArticleID) (*model.Article, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	a, ok := ix.articles[id]
	return a, ok
}

// ArticleByDir returns the article currently indexed for a bundle directory,
// or false when the bundle is not indexed. It is used by the watcher to
// snapshot the pre-edit state before an external change replaces it.
func (ix *Index) ArticleByDir(bundle string) (*model.Article, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for _, article := range ix.articles {
		if article.BundleDir == bundle {
			return article, true
		}
	}
	return nil, false
}
func (ix *Index) Articles() []*model.Article {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.Article, 0, len(ix.articles))
	for _, article := range ix.articles {
		items = append(items, article)
	}
	return items
}
func (ix *Index) BySlug(typ model.ContentType, loc model.Locale, slug string) (*model.Article, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	id, ok := ix.slugIdx[slugKey{typ, loc, slug}]
	if !ok {
		return nil, false
	}
	a := ix.articles[id]
	return a, true
}
func (ix *Index) List(q ListQuery) ([]*model.Article, int) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	ids := ix.byTypeLoc[typeLocKey{q.Type, q.Locale}]
	out := make([]*model.Article, 0, len(ids))
	for _, id := range ids {
		a := ix.articles[id]
		v := a.Versions[q.Locale]
		if v == nil {
			continue
		}
		if len(q.Status) > 0 && !contains(q.Status, v.Front.Status) {
			continue
		}
		if q.Category != "" && !containsID(ix.byCategory[q.Category], id) {
			continue
		}
		if q.Tag != "" && !containsID(ix.byTag[q.Tag], id) {
			continue
		}
		if q.Author != "" && v.Front.Author != q.Author {
			continue
		}
		if q.Year > 0 && (v.Front.Date.Year() != q.Year || (q.Month > 0 && int(v.Front.Date.Month()) != q.Month)) {
			continue
		}
		if q.TransStatus != "" && (a.Trans[q.Locale] == nil || a.Trans[q.Locale].Status != q.TransStatus) {
			continue
		}
		if q.Search != "" && !strings.Contains(strings.ToLower(v.Front.Title+" "+v.Front.Description), strings.ToLower(q.Search)) {
			continue
		}
		out = append(out, a)
	}
	if q.Sort == "date_asc" {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Versions[q.Locale].Front.Date.Before(out[j].Versions[q.Locale].Front.Date)
		})
	} else if q.Sort == "title_asc" {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Versions[q.Locale].Front.Title < out[j].Versions[q.Locale].Front.Title
		})
	}
	total := len(out)
	if q.PerPage > 0 {
		page := q.Page
		if page < 1 {
			page = 1
		}
		start := (page - 1) * q.PerPage
		if start >= len(out) {
			return []*model.Article{}, total
		}
		end := start + q.PerPage
		if end > len(out) {
			end = len(out)
		}
		out = out[start:end]
	}
	return out, total
}
func contains[T comparable](items []T, value T) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func containsID(items []model.ArticleID, value model.ArticleID) bool { return contains(items, value) }
func (ix *Index) PublishedPosts(loc model.Locale) []*model.Article   { return ix.listPublished(loc) }
func (ix *Index) listPublished(loc model.Locale) []*model.Article {
	items, _ := ix.List(ListQuery{Type: model.ContentPost, Locale: loc, Status: []model.Status{model.StatusPublished}})
	return items
}
func (ix *Index) AdjacentPosts(loc model.Locale, id model.ArticleID) (*model.Article, *model.Article) {
	items := ix.listPublished(loc)
	for i, item := range items {
		if item.ID == id {
			var prev, next *model.Article
			if i > 0 {
				prev = items[i-1]
			}
			if i+1 < len(items) {
				next = items[i+1]
			}
			return prev, next
		}
	}
	return nil, nil
}
func (ix *Index) CategoryTree(loc model.Locale) []*CategoryNode {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.catTree
}
func (ix *Index) Categories() []*model.Category {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.Category, 0, len(ix.categories))
	for _, category := range ix.categories {
		copy := *category
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Order != items[j].Order {
			return items[i].Order < items[j].Order
		}
		return items[i].Slug < items[j].Slug
	})
	return items
}
func (ix *Index) Category(id string) (*model.Category, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	category, ok := ix.categories[id]
	if !ok {
		return nil, false
	}
	copy := *category
	return &copy, true
}
func (ix *Index) Tag(id string) (*model.Tag, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	tag, ok := ix.tags[id]
	if !ok {
		return nil, false
	}
	copy := *tag
	return &copy, true
}
func (ix *Index) Tags() []*model.Tag {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.Tag, 0, len(ix.tags))
	for _, tag := range ix.tags {
		copy := *tag
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Slug < items[j].Slug })
	return items
}
func (ix *Index) Links() []*model.Link {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.Link, 0, len(ix.links))
	for _, link := range ix.links {
		if link.Status != "" && link.Status != "active" {
			continue
		}
		copy := *link
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Order != items[j].Order {
			return items[i].Order < items[j].Order
		}
		return items[i].Name < items[j].Name
	})
	return items
}
func (ix *Index) LinkGroups() []*model.LinkGroup {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.LinkGroup, 0, len(ix.linkGroups))
	for _, group := range ix.linkGroups {
		copy := *group
		items = append(items, &copy)
	}
	return items
}
func (ix *Index) Link(id string) (*model.Link, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	item, ok := ix.links[id]
	if !ok {
		return nil, false
	}
	copy := *item
	return &copy, true
}
func (ix *Index) Menus() []*model.Menu {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	items := make([]*model.Menu, 0, len(ix.menus))
	for _, menu := range ix.menus {
		copy := *menu
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
func (ix *Index) Menu(id string) (*model.Menu, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	item, ok := ix.menus[id]
	if !ok {
		return nil, false
	}
	copy := *item
	return &copy, true
}
func (ix *Index) Stats() Stats { ix.mu.RLock(); defer ix.mu.RUnlock(); return ix.stats }
func (ix *Index) Errors() []content.ScanError {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return append([]content.ScanError(nil), ix.errors...)
}
func (ix *Index) Body(id model.ArticleID, loc model.Locale) (string, error) {
	ix.mu.RLock()
	a := ix.articles[id]
	if a == nil || a.Versions[loc] == nil {
		ix.mu.RUnlock()
		return "", os.ErrNotExist
	}
	body := a.Versions[loc].Body
	path := a.Versions[loc].FilePath
	mode := ix.stats.BodyMode
	root := ix.options.ContentRoot
	ix.mu.RUnlock()
	if mode == BodyResident {
		return body, nil
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	_, body, err = content.Parse(raw)
	return body, err
}
func (ix *Index) String() string { return fmt.Sprintf("index(%d articles)", ix.Stats().Articles) }

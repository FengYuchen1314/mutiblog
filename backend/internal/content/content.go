// Package content reads and writes Markdown article bundles, the canonical content store.
package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/FengYuchen1314/mutiblog/internal/fsutil"
	"github.com/FengYuchen1314/mutiblog/internal/model"
)

var ErrNoFrontMatter = errors.New("missing YAML front matter")

type Store struct {
	root string
	mu   fsutil.KeyedMutex
}
type ScanError struct {
	Path string
	Err  error
}
type SaveOpts struct{ BumpSourceRevision, Snapshot, MarkManualEdit, MirrorAuthoritative bool }
type Draft struct {
	Front   model.FrontMatter
	Body    string
	SavedAt time.Time
}
type RevisionInfo struct {
	Revision  int
	Locale    model.Locale
	CreatedAt time.Time
	Path      string
}

func NewStore(root string) *Store { return &Store{root: root} }
func (s *Store) Root() string     { return s.root }

func SplitFrontMatter(raw []byte) ([]byte, string, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	for bytes.HasPrefix(raw, []byte("\n")) {
		raw = raw[1:]
	}
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, "", ErrNoFrontMatter
	}
	rest := raw[4:]
	i := bytes.Index(rest, []byte("\n---\n"))
	if i < 0 {
		return nil, "", ErrNoFrontMatter
	}
	body := string(rest[i+5:])
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return rest[:i], body, nil
}

func Parse(raw []byte) (model.FrontMatter, string, error) {
	yml, body, err := SplitFrontMatter(raw)
	if err != nil {
		return model.FrontMatter{}, "", err
	}
	var f model.FrontMatter
	if err = yaml.Unmarshal(yml, &f); err != nil {
		return f, "", err
	}
	if err = f.Validate(); err != nil {
		return f, "", err
	}
	return f, body, nil
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
func boolNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprintf("%t", value)}
}
func intNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(value)}
}
func add(n *yaml.Node, key string, value any) {
	if value == nil {
		return
	}
	n.Content = append(n.Content, scalar(key))
	switch v := value.(type) {
	case string:
		n.Content = append(n.Content, scalar(v))
	case model.ArticleID:
		n.Content = append(n.Content, scalar(string(v)))
	case model.Locale:
		n.Content = append(n.Content, scalar(string(v)))
	case model.Status:
		n.Content = append(n.Content, scalar(string(v)))
	case time.Time:
		n.Content = append(n.Content, scalar(v.Format(time.RFC3339)))
	case *time.Time:
		if v != nil {
			n.Content = append(n.Content, scalar(v.Format(time.RFC3339)))
		} else {
			n.Content = n.Content[:len(n.Content)-1]
		}
	case bool:
		n.Content = append(n.Content, boolNode(v))
	case int:
		n.Content = append(n.Content, intNode(v))
	default:
		var node yaml.Node
		if err := node.Encode(value); err != nil {
			panic(err)
		}
		n.Content = append(n.Content, &node)
	}
}
func Serialize(front model.FrontMatter, body string) ([]byte, error) {
	if err := front.Validate(); err != nil {
		return nil, err
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add(n, "id", front.ID)
	add(n, "title", front.Title)
	add(n, "slug", front.Slug)
	if front.Description != "" {
		add(n, "description", front.Description)
	}
	add(n, "date", front.Date)
	add(n, "updated", front.Updated)
	add(n, "status", front.Status)
	if len(front.Categories) > 0 {
		add(n, "categories", front.Categories)
	}
	if len(front.Tags) > 0 {
		add(n, "tags", front.Tags)
	}
	if front.Cover != "" {
		add(n, "cover", front.Cover)
	}
	add(n, "author", front.Author)
	add(n, "sourceLocale", front.SourceLocale)
	add(n, "locale", front.Locale)
	if front.Pinned {
		add(n, "pinned", true)
	}
	add(n, "toc", front.TOC)
	add(n, "comments", front.Comments)
	if front.Template != "" {
		add(n, "template", front.Template)
	}
	if front.Order != 0 {
		add(n, "order", front.Order)
	}
	if front.ShowInMenu {
		add(n, "showInMenu", true)
	}
	add(n, "seo", front.SEO)
	keys := make([]string, 0, len(front.Extra))
	for key := range front.Extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(n, key, front.Extra[key])
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	_ = enc.Close()
	body = strings.TrimRight(body, "\n") + "\n"
	return append(append([]byte("---\n"), out.Bytes()...), append([]byte("---\n\n"), []byte(body)...)...), nil
}

func (s *Store) LoadBundle(bundleDir string) (*model.Article, error) {
	abs, err := fsutil.SafeJoin(s.root, bundleDir)
	if err != nil {
		return nil, err
	}
	metaBytes, err := os.ReadFile(filepath.Join(abs, "metadata.yaml"))
	if err != nil {
		return nil, err
	}
	var meta model.BundleMetadata
	if err = yaml.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}
	if meta.ID == "" || meta.SourceLocale == "" || meta.Type == "" {
		return nil, fmt.Errorf("invalid metadata in %s", bundleDir)
	}
	a := &model.Article{
		ID:        meta.ID,
		Type:      meta.Type,
		BundleDir: filepath.ToSlash(bundleDir),
		Source:    meta.SourceLocale,
		SourceRev: meta.SourceRevision,
		CreatedAt: meta.CreatedAt,
		Versions:  map[model.Locale]*model.ArticleVersion{},
		Trans:     meta.Translations,
	}
	if a.Trans == nil {
		a.Trans = map[model.Locale]*model.TranslationState{}
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "index.") || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		loc := model.Locale(strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "index."), ".md"))
		loc = canonicalFileLocale(loc)
		path := filepath.Join(abs, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		front, body, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if front.ID != meta.ID {
			front.ID = meta.ID
		}
		front.Locale = loc
		info, _ := entry.Info()
		sum := sha256.Sum256([]byte(body))
		a.Versions[loc] = &model.ArticleVersion{
			Locale:      loc,
			FilePath:    filepath.ToSlash(filepath.Join(bundleDir, entry.Name())),
			Front:       front,
			Body:        body,
			BodyHash:    fmt.Sprintf("%x", sum),
			FileModTime: info.ModTime(),
		}
		if _, ok := a.Trans[loc]; !ok {
			status := model.TSCompleted
			if loc == a.Source {
				status = model.TSOriginal
			}
			a.Trans[loc] = &model.TranslationState{Status: status}
		}
	}
	if len(a.Versions) == 0 {
		return nil, fmt.Errorf("bundle %s has no language files", bundleDir)
	}
	return a, nil
}
func canonicalFileLocale(loc model.Locale) model.Locale {
	bits := strings.Split(string(loc), "-")
	for i := range bits {
		if i == 0 {
			bits[i] = strings.ToLower(bits[i])
		} else if len(bits[i]) == 2 {
			bits[i] = strings.ToUpper(bits[i])
		}
	}
	return model.Locale(strings.Join(bits, "-"))
}
func (s *Store) ScanAll(ctx context.Context) ([]*model.Article, []ScanError, error) {
	roots := []string{"posts", "pages"}
	dirs := make([]string, 0)
	for _, root := range roots {
		base := filepath.Join(s.root, root)
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if _, err := os.Stat(filepath.Join(path, "metadata.yaml")); err == nil {
					rel, _ := filepath.Rel(s.root, path)
					dirs = append(dirs, filepath.ToSlash(rel))
					return filepath.SkipDir
				}
			}
			return nil
		})
	}
	type result struct {
		a   *model.Article
		err ScanError
	}
	jobs := make(chan string)
	results := make(chan result, len(dirs))
	workers := min(8, max(1, len(dirs)))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for dir := range jobs {
				if ctx.Err() != nil {
					return
				}
				a, err := s.LoadBundle(dir)
				if err != nil {
					results <- result{err: ScanError{Path: dir, Err: err}}
				} else {
					results <- result{a: a}
				}
			}
		}()
	}
	go func() {
		for _, dir := range dirs {
			jobs <- dir
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var articles []*model.Article
	var errs []ScanError
	for result := range results {
		if result.a != nil {
			articles = append(articles, result.a)
		} else {
			errs = append(errs, result.err)
		}
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].BundleDir < articles[j].BundleDir })
	return articles, errs, nil
}
func (s *Store) SaveMetadata(a *model.Article) error {
	unlock := s.mu.Lock(a.BundleDir)
	defer unlock()
	meta := model.BundleMetadata{
		ID:             a.ID,
		Type:           a.Type,
		SourceLocale:   a.Source,
		SourceRevision: a.SourceRev,
		CreatedAt:      a.CreatedAt,
		Translations:   a.Trans,
	}
	b, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(s.root, filepath.FromSlash(a.BundleDir), "metadata.yaml"), b, 0o644)
}

func (s *Store) SaveVersion(
	a *model.Article,
	loc model.Locale,
	front model.FrontMatter,
	body string,
	opts SaveOpts,
) error {
	unlock := s.mu.Lock(a.BundleDir)
	defer unlock()
	if opts.MirrorAuthoritative && loc != a.Source {
		if src := a.Versions[a.Source]; src != nil {
			mirror(&src.Front, &front)
		}
	}
	front.Locale = loc
	front.ID = a.ID
	b, err := Serialize(front, body)
	if err != nil {
		return err
	}
	file := filepath.Join(s.root, filepath.FromSlash(a.BundleDir), "index."+strings.ToLower(string(loc))+".md")
	if err = fsutil.AtomicWrite(file, b, 0o644); err != nil {
		return err
	}
	if a.Versions == nil {
		a.Versions = map[model.Locale]*model.ArticleVersion{}
	}
	sum := sha256.Sum256([]byte(body))
	a.Versions[loc] = &model.ArticleVersion{
		Locale:      loc,
		FilePath:    filepath.ToSlash(filepath.Join(a.BundleDir, filepath.Base(file))),
		Front:       front,
		Body:        body,
		BodyHash:    fmt.Sprintf("%x", sum),
		FileModTime: time.Now(),
	}
	if opts.BumpSourceRevision && loc == a.Source {
		a.SourceRev++
		for target, v := range a.Versions {
			if target != loc {
				mirror(&front, &v.Front)
			}
		}
	}
	if opts.MarkManualEdit && loc != a.Source {
		state := a.Trans[loc]
		if state == nil {
			state = &model.TranslationState{}
			a.Trans[loc] = state
		}
		state.ManualEdited = true
		state.Status = model.TSManual
		state.ManualEditedAt = ptr(time.Now())
	}
	if opts.Snapshot {
		_, err = s.snapshotLocked(a, loc)
	}
	if err != nil {
		return err
	}
	return s.saveMetadataLocked(a)
}
func ptr(t time.Time) *time.Time { return &t }
func mirror(src, dst *model.FrontMatter) {
	dst.ID = src.ID
	dst.Date = src.Date
	dst.Status = src.Status
	dst.Categories = append([]string(nil), src.Categories...)
	dst.Tags = append([]string(nil), src.Tags...)
	dst.Author = src.Author
	dst.Pinned = src.Pinned
	dst.Cover = src.Cover
	dst.SourceLocale = src.SourceLocale
	dst.Comments = src.Comments
}
func (s *Store) saveMetadataLocked(a *model.Article) error {
	meta := model.BundleMetadata{
		ID:             a.ID,
		Type:           a.Type,
		SourceLocale:   a.Source,
		SourceRevision: a.SourceRev,
		CreatedAt:      a.CreatedAt,
		Translations:   a.Trans,
	}
	b, err := yaml.Marshal(meta)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(s.root, filepath.FromSlash(a.BundleDir), "metadata.yaml"), b, 0o644)
}

func (s *Store) CreateBundle(
	typ model.ContentType,
	loc model.Locale,
	front model.FrontMatter,
	body string,
) (*model.Article, error) {
	if front.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		front.ID = model.ArticleID(id.String())
	}
	front.Locale = loc
	if front.SourceLocale == "" {
		front.SourceLocale = loc
	}
	if front.Date.IsZero() {
		front.Date = time.Now()
	}
	dirName := safeDir(front.Slug, front.ID)
	parent := "pages"
	if typ == model.ContentPost {
		parent = filepath.Join("posts", fmt.Sprint(front.Date.Year()))
	}
	for n := 2; ; n++ {
		candidate := filepath.ToSlash(filepath.Join(parent, dirName))
		if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(candidate))); errors.Is(err, os.ErrNotExist) {
			a := &model.Article{
				ID:        front.ID,
				Type:      typ,
				BundleDir: candidate,
				Source:    loc,
				CreatedAt: front.Date,
				Versions:  map[model.Locale]*model.ArticleVersion{},
				Trans:     map[model.Locale]*model.TranslationState{loc: {Status: model.TSOriginal, Revision: 1}},
			}
			if err := s.SaveVersion(a, loc, front, body, SaveOpts{}); err != nil {
				return nil, err
			}
			return a, nil
		}
		dirName = fmt.Sprintf("%s-%d", safeDir(front.Slug, front.ID), n)
	}
}
func safeDir(slug string, id model.ArticleID) string {
	var b strings.Builder
	for _, r := range strings.ToLower(slug) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	v := strings.Trim(b.String(), "-")
	if len(v) < 2 {
		v = time.Now().Format("20060102") + "-" + string(id)[:min(6, len(id))]
	}
	return v
}
func (s *Store) SaveDraft(id model.ArticleID, loc model.Locale, front model.FrontMatter, body string) error {
	b, err := Serialize(front, body)
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(
		filepath.Join(s.root, ".drafts", string(id)+"."+strings.ToLower(string(loc))+".md"),
		b,
		0o600,
	)
}
func (s *Store) LoadDraft(id model.ArticleID, loc model.Locale) (*Draft, error) {
	raw, err := os.ReadFile(filepath.Join(s.root, ".drafts", string(id)+"."+strings.ToLower(string(loc))+".md"))
	if err != nil {
		return nil, err
	}
	front, body, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return &Draft{Front: front, Body: body}, nil
}
func (s *Store) DiscardDraft(id model.ArticleID, loc model.Locale) error {
	err := os.Remove(filepath.Join(s.root, ".drafts", string(id)+"."+strings.ToLower(string(loc))+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Store) SnapshotRevision(a *model.Article, loc model.Locale) (int, error) {
	unlock := s.mu.Lock(a.BundleDir)
	defer unlock()
	return s.snapshotLocked(a, loc)
}
func (s *Store) snapshotLocked(a *model.Article, loc model.Locale) (int, error) {
	v := a.Versions[loc]
	if v == nil {
		return 0, os.ErrNotExist
	}
	dir := filepath.Join(s.root, ".revisions", string(a.ID))
	entries, _ := os.ReadDir(dir)
	revision := 1
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "%d.", &n); err == nil && n >= revision {
			revision = n + 1
		}
	}
	b, err := Serialize(v.Front, v.Body)
	if err != nil {
		return 0, err
	}
	return revision, fsutil.AtomicWrite(
		filepath.Join(dir, fmt.Sprintf("%04d.%s.md", revision, strings.ToLower(string(loc)))),
		b,
		0o600,
	)
}

func (s *Store) ListRevisions(id model.ArticleID, loc model.Locale) ([]RevisionInfo, error) {
	dir := filepath.Join(s.root, ".revisions", string(id))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []RevisionInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	needle := "." + strings.ToLower(string(loc)) + ".md"
	out := make([]RevisionInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), needle) {
			continue
		}
		var revision int
		if _, err := fmt.Sscanf(entry.Name(), "%d.", &revision); err != nil {
			continue
		}
		info, _ := entry.Info()
		out = append(
			out,
			RevisionInfo{
				Revision:  revision,
				Locale:    loc,
				CreatedAt: info.ModTime(),
				Path:      filepath.ToSlash(filepath.Join(".revisions", string(id), entry.Name())),
			},
		)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}
func (s *Store) ReadRevision(id model.ArticleID, loc model.Locale, rev int) (model.FrontMatter, string, error) {
	path := filepath.Join(
		s.root,
		".revisions",
		string(id),
		fmt.Sprintf("%04d.%s.md", rev, strings.ToLower(string(loc))),
	)
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.FrontMatter{}, "", err
	}
	return Parse(raw)
}
func (s *Store) DeleteBundle(a *model.Article) error {
	if a == nil {
		return os.ErrNotExist
	}
	unlock := s.mu.Lock(a.BundleDir)
	defer unlock()
	bundle, err := fsutil.SafeJoin(s.root, filepath.ToSlash(a.BundleDir))
	if err != nil {
		return err
	}
	revisions, err := fsutil.SafeJoin(s.root, filepath.ToSlash(filepath.Join(".revisions", string(a.ID))))
	if err != nil {
		return err
	}
	if err := os.RemoveAll(bundle); err != nil {
		return err
	}
	if err := os.RemoveAll(revisions); err != nil {
		return err
	}
	drafts, err := fsutil.SafeJoin(s.root, ".drafts")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(drafts)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := string(a.ID) + "."
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if err := os.Remove(filepath.Join(drafts, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Package feed produces deterministic RSS, sitemap, and client-side search artifacts.
package feed

import (
	"encoding/json"
	"encoding/xml"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/model"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Generator struct {
	Output, BaseURL, SiteTitle string
	Index                      *index.Index
	ExtraURLs                  []string
	Prefix                     string
	RobotsTxt                  string
	Prefixes                   map[model.Locale]string
	DefaultLocale              model.Locale
}
type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Title string    `xml:"title"`
	Link  string    `xml:"link"`
	Items []rssItem `xml:"item"`
}
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func (g *Generator) Generate(loc model.Locale) error {
	posts := g.Index.PublishedPosts(loc)
	prefix := g.Prefix
	if prefix == "" {
		prefix = strings.ToLower(string(loc))
	}
	base := strings.TrimRight(g.BaseURL, "/") + "/" + prefix
	items := make([]rssItem, 0, len(posts))
	search := make([]map[string]string, 0, len(posts))
	urls := make([]sitemapURL, 0, len(posts)+1)
	urls = append(urls, sitemapURL{Loc: base + "/"})
	for _, post := range posts {
		v := post.Versions[loc]
		url := base + "/posts/" + v.Front.Slug + "/"
		items = append(items, rssItem{Title: v.Front.Title, Link: url, Description: v.Front.Description, PubDate: v.Front.Date.Format(time.RFC1123Z)})
		search = append(search, map[string]string{"id": string(post.ID), "title": v.Front.Title, "description": v.Front.Description, "url": "/" + prefix + "/posts/" + v.Front.Slug + "/"})
		urls = append(urls, sitemapURL{Loc: url, Alternates: g.articleAlternates(post)})
	}
	for _, extra := range g.ExtraURLs {
		urls = append(urls, sitemapURL{Loc: strings.TrimRight(g.BaseURL, "/") + extra})
	}
	sort.Slice(urls, func(i, j int) bool { return urls[i].Loc < urls[j].Loc })
	rssData, _ := xml.MarshalIndent(rss{Version: "2.0", Channel: rssChannel{Title: g.SiteTitle, Link: base + "/", Items: items}}, "", "  ")
	rssData = append([]byte(xml.Header), rssData...)
	searchData, _ := json.Marshal(search)
	var sitemap strings.Builder
	sitemap.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?><urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\" xmlns:xhtml=\"http://www.w3.org/1999/xhtml\">")
	for _, url := range urls {
		sitemap.WriteString("<url><loc>")
		sitemap.WriteString(xmlEscape(url.Loc))
		sitemap.WriteString("</loc>")
		for _, alternate := range url.Alternates {
			sitemap.WriteString("<xhtml:link rel=\"alternate\" hreflang=\"")
			sitemap.WriteString(xmlEscape(alternate.Lang))
			sitemap.WriteString("\" href=\"")
			sitemap.WriteString(xmlEscape(alternate.Href))
			sitemap.WriteString("\"/>")
		}
		sitemap.WriteString("</url>")
	}
	sitemap.WriteString("</urlset>")
	dir := filepath.Join(g.Output, prefix)
	files := map[string][]byte{filepath.Join(dir, "rss.xml"): rssData, filepath.Join(dir, "search-index.json"): searchData, filepath.Join(g.Output, "sitemap-"+prefix+".xml"): []byte(sitemap.String())}
	if g.RobotsTxt != "" {
		files[filepath.Join(g.Output, "robots.txt")] = []byte(strings.ReplaceAll(g.RobotsTxt, "{{baseURL}}", strings.TrimRight(g.BaseURL, "/")))
	}
	if entries, err := filepath.Glob(filepath.Join(g.Output, "sitemap-*.xml")); err == nil {
		current := filepath.Join(g.Output, "sitemap-"+prefix+".xml")
		seen := map[string]bool{}
		entries = append(entries, current)
		unique := entries[:0]
		for _, entry := range entries {
			if seen[entry] {
				continue
			}
			seen[entry] = true
			unique = append(unique, entry)
		}
		entries = unique
		sort.Strings(entries)
		var indexXML strings.Builder
		indexXML.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?><sitemapindex xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">")
		for _, entry := range entries {
			name := filepath.Base(entry)
			indexXML.WriteString("<sitemap><loc>")
			indexXML.WriteString(xmlEscape(strings.TrimRight(g.BaseURL, "/") + "/" + name))
			indexXML.WriteString("</loc></sitemap>")
		}
		indexXML.WriteString("</sitemapindex>")
		files[filepath.Join(g.Output, "sitemap.xml")] = []byte(indexXML.String())
	}
	return fsutil.AtomicWriteBatch(files, 0o644)
}

type sitemapURL struct {
	Loc        string
	Alternates []sitemapAlternate
}
type sitemapAlternate struct{ Lang, Href string }

func (g *Generator) articleAlternates(article *model.Article) []sitemapAlternate {
	locales := make([]model.Locale, 0, len(article.Versions))
	for locale, version := range article.Versions {
		if version.Front.Status == model.StatusPublished {
			locales = append(locales, locale)
		}
	}
	sort.Slice(locales, func(i, j int) bool { return locales[i] < locales[j] })
	result := make([]sitemapAlternate, 0, len(locales)+1)
	for _, locale := range locales {
		result = append(result, sitemapAlternate{Lang: string(locale), Href: g.articleURL(locale, article.Versions[locale].Front.Slug)})
	}
	if version := article.Versions[g.DefaultLocale]; version != nil && version.Front.Status == model.StatusPublished {
		result = append(result, sitemapAlternate{Lang: "x-default", Href: g.articleURL(g.DefaultLocale, version.Front.Slug)})
	}
	return result
}
func (g *Generator) articleURL(locale model.Locale, slug string) string {
	prefix := g.Prefixes[locale]
	if prefix == "" {
		prefix = strings.ToLower(string(locale))
	}
	return strings.TrimRight(g.BaseURL, "/") + "/" + strings.Trim(prefix, "/") + "/posts/" + slug + "/"
}
func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

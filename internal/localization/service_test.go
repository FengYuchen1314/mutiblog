package localization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	linkservice "github.com/FengYuchen1314/mutiblog/internal/links"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	menuservice "github.com/FengYuchen1314/mutiblog/internal/menus"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
	themeservice "github.com/FengYuchen1314/mutiblog/internal/themes"
)

type prefixTranslator struct{}

func (prefixTranslator) TranslateContent(_ context.Context, _, target string, source domain.LocalizedMarkdown) (domain.LocalizedMarkdown, error) {
	prefix := strings.ToUpper(target) + " "
	source.Title = prefix + source.Title
	source.Summary = prefix + source.Summary
	source.SEOTitle = prefix + source.SEOTitle
	source.SEODescription = prefix + source.SEODescription
	source.Markdown = prefix + source.Markdown
	return source, nil
}

func (prefixTranslator) TranslateFields(_ context.Context, _, target string, source map[string]string) (map[string]string, error) {
	translated := make(map[string]string, len(source))
	for key, value := range source {
		if value == "" {
			translated[key] = ""
		} else {
			translated[key] = strings.ToUpper(target) + " " + value
		}
	}
	return translated, nil
}

type mutatingPrefixTranslator struct {
	onFirstContent func() error
	fieldSources   []map[string]string
}

func (t *mutatingPrefixTranslator) TranslateContent(ctx context.Context, sourceLocale, targetLocale string, source domain.LocalizedMarkdown) (domain.LocalizedMarkdown, error) {
	if mutate := t.onFirstContent; mutate != nil {
		t.onFirstContent = nil
		if err := mutate(); err != nil {
			return domain.LocalizedMarkdown{}, err
		}
	}
	return (prefixTranslator{}).TranslateContent(ctx, sourceLocale, targetLocale, source)
}

func (t *mutatingPrefixTranslator) TranslateFields(ctx context.Context, sourceLocale, targetLocale string, source map[string]string) (map[string]string, error) {
	copyOfSource := make(map[string]string, len(source))
	for key, value := range source {
		copyOfSource[key] = value
	}
	t.fieldSources = append(t.fieldSources, copyOfSource)
	return (prefixTranslator{}).TranslateFields(ctx, sourceLocale, targetLocale, source)
}

func TestProvisionCoversEveryPublicEntityAndThemeText(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	}
	site := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion, SourceLocale: "zh-CN", ActiveTheme: "earth",
		Locales:   map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站", Subtitle: "副标题", Description: "说明"}},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", site, false); err != nil {
		t.Fatal(err)
	}
	if err := dictionary.Ensure(repository); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "source-post", Title: "文章", Markdown: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := contentService.CreatePage(content.CreatePageInput{ID: "source-page", Title: "页面", Markdown: "页面正文"})
	if err != nil {
		t.Fatal(err)
	}
	taxonomyService := taxonomy.NewService(repository)
	category, err := taxonomyService.Create("Category", taxonomy.CreateInput{ID: "news", Name: "新闻", Description: "新闻说明"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := taxonomyService.Create("Tag", taxonomy.CreateInput{ID: "release", Name: "发布"}); err != nil {
		t.Fatal(err)
	}
	linksService := linkservice.NewService(repository)
	group, err := linksService.CreateGroup(linkservice.CreateGroupInput{ID: "friends", Name: "朋友"})
	if err != nil {
		t.Fatal(err)
	}
	link, err := linksService.CreateLink(linkservice.CreateLinkInput{ID: "example", GroupID: group.ID, URL: "https://example.com", Name: "示例"})
	if err != nil {
		t.Fatal(err)
	}
	menuService := menuservice.NewService(repository)
	menu, err := menuService.Create(menuservice.CreateInput{ID: "primary", Label: "主菜单"})
	if err != nil {
		t.Fatal(err)
	}
	menu, err = menuService.AddItem(menu.ID, menuservice.AddItemInput{ID: "home", TargetKind: "internal", URL: "/", Label: "首页", ExpectedRevision: menu.Revision})
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(repository, contentService, prefixTranslator{})
	report, err := service.Provision(context.Background(), "ja")
	if err != nil {
		t.Fatal(err)
	}
	if report.Site != 1 || report.Dictionaries != len(dictionary.SourceValues()) || report.Posts != 1 || report.Pages != 1 || report.Taxonomies != 2 || report.Links != 2 || report.Menus != 2 || report.Theme < 1 {
		t.Fatalf("provision report = %#v", report)
	}
	translatedPost, err := contentService.GetPost(post.Meta.ID)
	if err != nil || translatedPost.Content["ja"].Title != "JA 文章" || translatedPost.Meta.Locales["ja"].Origin != domain.LocaleOriginAI {
		t.Fatalf("translated post = %#v, %v", translatedPost, err)
	}
	translatedPage, err := contentService.GetPage(page.Meta.ID)
	if err != nil || translatedPage.Content["ja"].Title != "JA 页面" {
		t.Fatalf("translated page = %#v, %v", translatedPage, err)
	}
	translatedCategory, err := taxonomyService.Get("Category", category.ID)
	if err != nil || translatedCategory.Locales["ja"].Name != "JA 新闻" || translatedCategory.Locales["ja"].Origin != domain.LocaleOriginAI {
		t.Fatalf("translated category = %#v, %v", translatedCategory, err)
	}
	translatedLink, err := linksService.ListLinks()
	if err != nil || len(translatedLink) != 1 || translatedLink[0].Locales["ja"].Name != "JA 示例" || translatedLink[0].ID != link.ID {
		t.Fatalf("translated link = %#v, %v", translatedLink, err)
	}
	translatedMenu, err := menuService.Get(menu.ID)
	if err != nil || translatedMenu.Locales["ja"].Label != "JA 主菜单" || translatedMenu.Items[0].Locales["ja"].Label != "JA 首页" {
		t.Fatalf("translated menu = %#v, %v", translatedMenu, err)
	}
	dictionaries, err := dictionary.Read(repository)
	if err != nil || !strings.HasPrefix(dictionaries["ja"]["home"], "JA ") {
		t.Fatalf("translated dictionary = %#v, %v", dictionaries["ja"], err)
	}
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil || site.Locales["ja"].Title != "JA 测试站" {
		t.Fatalf("translated site = %#v, %v", site, err)
	}
	themeRuntime, err := themeservice.NewService(repository).RuntimeFor("earth")
	if err != nil || !strings.HasPrefix(themeRuntime.LocalizedSettings["ja"]["layout.heroKicker"], "JA ") {
		t.Fatalf("translated theme settings = %#v, %v", themeRuntime.LocalizedSettings, err)
	}
}

func TestProvisionRepairsPublishedReleaseBehindUnpublishedSourceHead(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		Enabled: []domain.LocaleDefinition{
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		ActiveTheme:   "earth",
		Locales:       map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := dictionary.Ensure(repository); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "published-source-edit", Title: "公开标题", Markdown: "公开正文"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.UpdateLocale(post.Meta.ID, "zh-CN", content.UpdateLocaleInput{
		ExpectedRevision: post.Meta.Revision,
		Title:            "未发布标题",
		Markdown:         "未发布正文",
	})
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(repository, contentService, prefixTranslator{})
	report, err := service.Provision(context.Background(), "ja")
	if err != nil {
		t.Fatal(err)
	}
	if report.Posts != 1 {
		t.Fatalf("published provision report = %#v", report)
	}
	head, err := contentService.GetPost(post.Meta.ID)
	if err != nil || head.Content["ja"].Title != "JA 未发布标题" {
		t.Fatalf("translated unpublished head = %#v, %v", head.Content["ja"], err)
	}
	released, err := contentService.GetPublishedRelease("Post", post.Meta.ID)
	if err != nil || released.Content["ja"].Title != "JA 公开标题" {
		t.Fatalf("translated public release = %#v, %v", released.Content["ja"], err)
	}
	retry, err := service.Provision(context.Background(), "ja")
	if err != nil || retry.Posts != 0 {
		t.Fatalf("idempotent provision retry = %#v, %v", retry, err)
	}
}

func TestProvisionTranslatesLegacyEntityFromItsImmutableSource(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacyLocales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled: []domain.LocaleDefinition{
			{Code: "en", Label: "English", Enabled: true},
			{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{"zh-CN"},
	}
	if err := repository.WriteYAML("config/locales.yaml", legacyLocales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "zh-CN",
		ActiveTheme:   "earth",
		Locales:       map[string]domain.LocalizedSite{"zh-CN": {Title: "测试站"}},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := dictionary.Ensure(repository); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "legacy-english-source", Title: "Legacy title", Markdown: "Legacy body"})
	if err != nil {
		t.Fatal(err)
	}
	legacyLocales.SourceLocale = "zh-CN"
	if err := repository.WriteYAML("config/locales.yaml", legacyLocales, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(repository, contentService, prefixTranslator{}).Provision(context.Background(), "ja"); err != nil {
		t.Fatal(err)
	}
	translated, err := contentService.GetPost(post.Meta.ID)
	if err != nil || translated.Meta.SourceLocale != "en" || translated.Content["ja"].Title != "JA Legacy title" {
		t.Fatalf("legacy source translation = %#v, %v", translated, err)
	}
}

func TestProvisionRetranslatesFingerprintTrackedResourcesAfterLaterFailureAndSourceChanges(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  localeconfig.FixedSourceLocale,
		Enabled: []domain.LocaleDefinition{
			{Code: localeconfig.FixedSourceLocale, Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady},
			{Code: "ja", Label: "日本語", Enabled: true, Status: domain.LocaleStatusProvisioning},
		},
		Fallback: []string{localeconfig.FixedSourceLocale},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  localeconfig.FixedSourceLocale,
		ActiveTheme:   "earth",
		Locales: map[string]domain.LocalizedSite{
			localeconfig.FixedSourceLocale: {Title: "初始站点", Subtitle: "初始副标题", Description: "初始说明"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := dictionary.Ensure(repository); err != nil {
		t.Fatal(err)
	}
	themeService := themeservice.NewService(repository)
	if _, err := themeService.SaveSettings("earth", map[string]any{
		"layout": map[string]any{"heroKicker": "初始主题标语"},
	}); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "retry-source", Title: "初始文章", Markdown: "初始正文"})
	if err != nil {
		t.Fatal(err)
	}
	translator := &mutatingPrefixTranslator{}
	translator.onFirstContent = func() error {
		current, getErr := contentService.GetPost(post.Meta.ID)
		if getErr != nil {
			return getErr
		}
		source := current.Content[localeconfig.FixedSourceLocale]
		_, updateErr := contentService.UpdateLocale(post.Meta.ID, localeconfig.FixedSourceLocale, content.UpdateLocaleInput{
			ExpectedRevision: current.Meta.Revision,
			Title:            "失败后文章",
			Summary:          source.Summary,
			SEOTitle:         source.SEOTitle,
			SEODescription:   source.SEODescription,
			Markdown:         source.Markdown,
		})
		return updateErr
	}
	service := NewService(repository, contentService, translator)
	firstReport, err := service.Provision(context.Background(), "ja")
	if !errors.Is(err, content.ErrSourceChanged) {
		t.Fatalf("first provision error = %v, want content.ErrSourceChanged", err)
	}
	if firstReport.Site != 1 || firstReport.Dictionaries == 0 || firstReport.Theme == 0 {
		t.Fatalf("resources were not durably written before later failure: %#v", firstReport)
	}
	firstFingerprints, err := service.readSourceFingerprints("ja")
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprints.Sources.Site == "" || firstFingerprints.Sources.Dictionary == "" || firstFingerprints.Sources.Theme == nil || firstFingerprints.Sources.Theme.Fingerprint == "" {
		t.Fatalf("first source fingerprints = %#v", firstFingerprints)
	}

	var changedSite domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &changedSite); err != nil {
		t.Fatal(err)
	}
	changedSite.Locales[localeconfig.FixedSourceLocale] = domain.LocalizedSite{Title: "更新站点", Subtitle: "更新副标题", Description: "更新说明"}
	changedSite.UpdatedAt = time.Now().UTC()
	if err := repository.WriteYAML("config/site.yaml", changedSite, false); err != nil {
		t.Fatal(err)
	}
	changedDictionary, err := dictionary.ReadSource(repository)
	if err != nil {
		t.Fatal(err)
	}
	changedDictionary["home"] = "更新首页"
	if err := repository.WriteYAML("content/dictionaries/zh-CN.yaml", changedDictionary, false); err != nil {
		t.Fatal(err)
	}
	if _, err := themeService.SaveSettings("earth", map[string]any{
		"layout": map[string]any{"heroKicker": "更新主题标语"},
	}); err != nil {
		t.Fatal(err)
	}

	retryReport, err := service.Provision(context.Background(), "ja")
	if err != nil {
		t.Fatal(err)
	}
	if retryReport.Site != 1 || retryReport.Dictionaries != len(changedDictionary) || retryReport.Theme == 0 || retryReport.Posts != 1 {
		t.Fatalf("retry provision report = %#v", retryReport)
	}
	if len(translator.fieldSources) != 6 {
		t.Fatalf("field translation calls = %d, want site/dictionary/theme twice", len(translator.fieldSources))
	}
	if translator.fieldSources[3]["title"] != "更新站点" || translator.fieldSources[4]["home"] != "更新首页" || translator.fieldSources[5]["layout.heroKicker"] != "更新主题标语" {
		t.Fatalf("retry source snapshots = %#v", translator.fieldSources[3:])
	}

	var translatedSite domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &translatedSite); err != nil {
		t.Fatal(err)
	}
	if translatedSite.Locales["ja"].Title != "JA 更新站点" {
		t.Fatalf("retry site translation = %#v", translatedSite.Locales["ja"])
	}
	translatedDictionaries, err := dictionary.Read(repository)
	if err != nil || translatedDictionaries["ja"]["home"] != "JA 更新首页" {
		t.Fatalf("retry dictionary translation = %#v, %v", translatedDictionaries["ja"], err)
	}
	themeRuntime, err := themeService.RuntimeFor("earth")
	if err != nil || themeRuntime.LocalizedSettings["ja"]["layout.heroKicker"] != "JA 更新主题标语" {
		t.Fatalf("retry theme translation = %#v, %v", themeRuntime.LocalizedSettings["ja"], err)
	}

	retryFingerprints, err := service.readSourceFingerprints("ja")
	if err != nil {
		t.Fatal(err)
	}
	if retryFingerprints.Sources.Site == firstFingerprints.Sources.Site ||
		retryFingerprints.Sources.Dictionary == firstFingerprints.Sources.Dictionary ||
		retryFingerprints.Sources.Theme == nil || retryFingerprints.Sources.Theme.Fingerprint == firstFingerprints.Sources.Theme.Fingerprint {
		t.Fatalf("source fingerprints were not advanced: first=%#v retry=%#v", firstFingerprints, retryFingerprints)
	}
}

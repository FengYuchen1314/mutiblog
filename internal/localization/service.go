package localization

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	"golang.org/x/text/language"
)

var (
	ErrLocaleUnavailable = errors.New("locale is unavailable for site-wide localization")
	ErrSourceChanged     = errors.New("site-wide localization source changed")
)

type Translator interface {
	TranslateContent(context.Context, string, string, domain.LocalizedMarkdown) (domain.LocalizedMarkdown, error)
	TranslateFields(context.Context, string, string, map[string]string) (map[string]string, error)
}

type Report struct {
	Locale       string `json:"locale"`
	Site         int    `json:"site"`
	Dictionaries int    `json:"dictionaries"`
	Posts        int    `json:"posts"`
	Pages        int    `json:"pages"`
	Taxonomies   int    `json:"taxonomies"`
	Links        int    `json:"links"`
	Menus        int    `json:"menus"`
	Theme        int    `json:"theme"`
}

type Service struct {
	repository *fsrepo.Repository
	content    *content.Service
	taxonomies *taxonomy.Service
	links      *linkservice.Service
	menus      *menuservice.Service
	themes     *themeservice.Service
	translator Translator
}

func NewService(repository *fsrepo.Repository, contentService *content.Service, translator Translator) *Service {
	return &Service{
		repository: repository,
		content:    contentService,
		taxonomies: taxonomy.NewService(repository),
		links:      linkservice.NewService(repository),
		menus:      menuservice.NewService(repository),
		themes:     themeservice.NewService(repository),
		translator: translator,
	}
}

type contentPlan struct {
	kind           string
	id             string
	sourceRevision int
	targetRevision int
	translated     domain.LocalizedMarkdown
}

type releaseContentPlan struct {
	contentPlan
	releaseRevision int
}

type contentPromotionPlan struct {
	kind           string
	id             string
	sourceRevision int
	allowManual    bool
}

type taxonomyPlan struct {
	kind           string
	id             string
	sourceRevision int
	targetRevision int
	translated     domain.LocalizedTaxonomy
}

type linkPlan struct {
	kind           string
	id             string
	sourceRevision int
	targetRevision int
	translated     domain.LocalizedLink
}

type menuPlan struct {
	menuID         string
	itemID         string
	sourceRevision int
	targetRevision int
	label          string
}

// Provision creates a complete target-language snapshot without rebuilding
// the public site. Callers keep the locale in provisioning state while this
// runs, mark it ready only after every optimistic apply succeeds, and then
// perform one full build.
func (s *Service) Provision(ctx context.Context, rawLocale string) (Report, error) {
	if s == nil || s.repository == nil || s.content == nil || s.translator == nil {
		return Report{}, errors.New("localization service is unavailable")
	}
	tag, err := language.Parse(strings.TrimSpace(rawLocale))
	if err != nil || strings.TrimSpace(rawLocale) == "" || tag.String() == localeconfig.FixedSourceLocale {
		return Report{}, ErrLocaleUnavailable
	}
	locale := tag.String()
	if err := s.requireTarget(locale); err != nil {
		return Report{}, err
	}
	report := Report{Locale: locale}

	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		return report, err
	}
	if site.SourceLocale != localeconfig.FixedSourceLocale {
		return report, ErrLocaleUnavailable
	}
	sourceSite, exists := site.Locales[localeconfig.FixedSourceLocale]
	if !exists || strings.TrimSpace(sourceSite.Title) == "" {
		return report, ErrLocaleUnavailable
	}
	fingerprints, err := s.readSourceFingerprints(locale)
	if err != nil {
		return report, fmt.Errorf("read localization source fingerprints: %w", err)
	}
	siteFingerprint := localizedSiteFingerprint(sourceSite)
	translatedSite := site.Locales[locale]
	siteTargetComplete := completeLocalizedSite(sourceSite, translatedSite)
	siteReusable := siteTargetComplete && fingerprints.Sources.Site == siteFingerprint
	if !siteReusable {
		translatedSiteFields, translateErr := s.translator.TranslateFields(ctx, localeconfig.FixedSourceLocale, locale, map[string]string{
			"title": sourceSite.Title, "subtitle": sourceSite.Subtitle, "description": sourceSite.Description,
		})
		if translateErr != nil {
			return report, fmt.Errorf("translate site settings: %w", translateErr)
		}
		translatedSite = domain.LocalizedSite{Title: strings.TrimSpace(translatedSiteFields["title"]), Subtitle: strings.TrimSpace(translatedSiteFields["subtitle"]), Description: strings.TrimSpace(translatedSiteFields["description"])}
		if !completeLocalizedSite(sourceSite, translatedSite) {
			return report, errors.New("translated site fields are incomplete")
		}
	}

	sourceDictionary, err := dictionary.ReadSource(s.repository)
	if err != nil {
		return report, fmt.Errorf("read source framework dictionary: %w", err)
	}
	dictionaries, err := dictionary.Read(s.repository)
	if err != nil {
		return report, fmt.Errorf("read framework dictionaries: %w", err)
	}
	dictionaryFingerprint := dictionarySourceFingerprint(sourceDictionary)
	translatedDictionary := dictionaries[locale]
	dictionaryTargetComplete := dictionary.ValidateComplete(s.repository, locale, translatedDictionary) == nil
	dictionaryReusable := dictionaryTargetComplete && fingerprints.Sources.Dictionary == dictionaryFingerprint
	if !dictionaryReusable {
		translatedDictionary, err = s.translator.TranslateFields(ctx, localeconfig.FixedSourceLocale, locale, sourceDictionary)
		if err != nil {
			return report, fmt.Errorf("translate framework dictionary: %w", err)
		}
	}
	themeID := effectiveThemeID(site.ActiveTheme)
	themeSource, err := s.themes.LocalizableText(themeID)
	if err != nil {
		return report, fmt.Errorf("read localizable theme settings: %w", err)
	}
	if themeID != "earth" && len(themeSource) == 0 {
		return report, fmt.Errorf("theme %s has no public localization contract: %w", themeID, ErrLocaleUnavailable)
	}
	themeRuntime, err := s.themes.RuntimeFor(themeID)
	if err != nil {
		return report, fmt.Errorf("read localized theme settings: %w", err)
	}
	themeFingerprint := themeSourceTextFingerprint(themeID, themeSource)
	translatedTheme := themeRuntime.LocalizedSettings[locale]
	themeTargetComplete := completeStringMap(themeSource, translatedTheme)
	themeReusable := themeTargetComplete && fingerprints.Sources.Theme != nil &&
		fingerprints.Sources.Theme.ID == themeID && fingerprints.Sources.Theme.Fingerprint == themeFingerprint
	if len(themeSource) > 0 && !themeReusable {
		translatedTheme, err = s.translator.TranslateFields(ctx, localeconfig.FixedSourceLocale, locale, themeSource)
		if err != nil {
			return report, fmt.Errorf("translate theme settings: %w", err)
		}
	}

	contentPlans := make([]contentPlan, 0)
	releaseContentPlans := make([]releaseContentPlan, 0)
	promotionPlans := make([]contentPromotionPlan, 0)
	posts, err := s.content.ListPosts()
	if err != nil {
		return report, err
	}
	publicPosts, err := s.content.ListPostsForBuild()
	if err != nil {
		return report, err
	}
	contentPlans, releaseContentPlans, promotionPlans, err = s.planContentKind(ctx, "Post", posts, publicPosts, locale, contentPlans, releaseContentPlans, promotionPlans)
	if err != nil {
		return report, err
	}
	pages, err := s.content.ListPages()
	if err != nil {
		return report, err
	}
	publicPages, err := s.content.ListPagesForBuild()
	if err != nil {
		return report, err
	}
	contentPlans, releaseContentPlans, promotionPlans, err = s.planContentKind(ctx, "Page", pages, publicPages, locale, contentPlans, releaseContentPlans, promotionPlans)
	if err != nil {
		return report, err
	}

	taxonomyPlans := make([]taxonomyPlan, 0)
	for _, kind := range []string{"Category", "Tag"} {
		items, listErr := s.taxonomies.List(kind)
		if listErr != nil {
			return report, listErr
		}
		for _, item := range items {
			plan, needed, planErr := s.planTaxonomy(ctx, item, locale)
			if planErr != nil {
				return report, planErr
			}
			if needed {
				taxonomyPlans = append(taxonomyPlans, plan)
			}
		}
	}

	linkPlans := make([]linkPlan, 0)
	groups, err := s.links.ListGroups()
	if err != nil {
		return report, err
	}
	for _, item := range groups {
		plan, needed, planErr := s.planLink(ctx, item.Kind, item.ID, item.SourceLocale, item.Locales, locale)
		if planErr != nil {
			return report, planErr
		}
		if needed {
			linkPlans = append(linkPlans, plan)
		}
	}
	links, err := s.links.ListLinks()
	if err != nil {
		return report, err
	}
	for _, item := range links {
		plan, needed, planErr := s.planLink(ctx, item.Kind, item.ID, item.SourceLocale, item.Locales, locale)
		if planErr != nil {
			return report, planErr
		}
		if needed {
			linkPlans = append(linkPlans, plan)
		}
	}

	menuPlans := make([]menuPlan, 0)
	menus, err := s.menus.List()
	if err != nil {
		return report, err
	}
	for _, menu := range menus {
		source, exists := menu.Locales[menu.SourceLocale]
		if !exists || strings.TrimSpace(source.Label) == "" {
			return report, fmt.Errorf("menu %s: %w", menu.ID, ErrLocaleUnavailable)
		}
		if locale != menu.SourceLocale && !currentMenuLocale(menu.Locales, menu.SourceLocale, locale) {
			fields, translateErr := s.translator.TranslateFields(ctx, menu.SourceLocale, locale, map[string]string{"label": source.Label})
			if translateErr != nil {
				return report, fmt.Errorf("translate menu %s: %w", menu.ID, translateErr)
			}
			menuPlans = append(menuPlans, menuPlan{menuID: menu.ID, sourceRevision: source.Revision, targetRevision: menu.Locales[locale].Revision, label: fields["label"]})
		}
		for _, item := range menu.Items {
			if locale == menu.SourceLocale || currentMenuLocale(item.Locales, menu.SourceLocale, locale) {
				continue
			}
			itemSource, exists := item.Locales[menu.SourceLocale]
			if !exists || strings.TrimSpace(itemSource.Label) == "" {
				return report, fmt.Errorf("menu item %s.%s: %w", menu.ID, item.ID, ErrLocaleUnavailable)
			}
			fields, translateErr := s.translator.TranslateFields(ctx, menu.SourceLocale, locale, map[string]string{"label": itemSource.Label})
			if translateErr != nil {
				return report, fmt.Errorf("translate menu item %s.%s: %w", menu.ID, item.ID, translateErr)
			}
			menuPlans = append(menuPlans, menuPlan{menuID: menu.ID, itemID: item.ID, sourceRevision: itemSource.Revision, targetRevision: item.Locales[locale].Revision, label: fields["label"]})
		}
	}

	// Revalidate each source immediately before writing its target. A source
	// fingerprint is claimed only after the target write and a second source
	// check. Clearing an older claim first makes a crash between the target and
	// fingerprint writes safe: the next retry must translate again.
	if err := s.revalidateResourceSources(siteFingerprint, dictionaryFingerprint, themeID, themeFingerprint); err != nil {
		return report, err
	}
	latestSite, err := s.revalidateSiteSource(siteFingerprint, themeID)
	if err != nil {
		return report, err
	}
	if !siteReusable {
		if fingerprints.Sources.Site != "" {
			fingerprints.Sources.Site = ""
			if err := s.writeSourceFingerprints(fingerprints); err != nil {
				return report, fmt.Errorf("invalidate site source fingerprint: %w", err)
			}
		}
		latestSite, err = s.revalidateSiteSource(siteFingerprint, themeID)
		if err != nil {
			return report, err
		}
		if latestSite.Locales == nil {
			latestSite.Locales = make(map[string]domain.LocalizedSite)
		}
		latestSite.Locales[locale] = translatedSite
		latestSite.UpdatedAt = time.Now().UTC()
		if err := s.repository.WriteYAML("config/site.yaml", latestSite, false); err != nil {
			return report, err
		}
		if _, err := s.revalidateSiteSource(siteFingerprint, themeID); err != nil {
			return report, err
		}
		fingerprints.Sources.Site = siteFingerprint
		if err := s.writeSourceFingerprints(fingerprints); err != nil {
			return report, fmt.Errorf("write site source fingerprint: %w", err)
		}
		report.Site = 1
	}

	if err := s.revalidateDictionarySource(dictionaryFingerprint); err != nil {
		return report, err
	}
	if !dictionaryReusable {
		if fingerprints.Sources.Dictionary != "" {
			fingerprints.Sources.Dictionary = ""
			if err := s.writeSourceFingerprints(fingerprints); err != nil {
				return report, fmt.Errorf("invalidate dictionary source fingerprint: %w", err)
			}
		}
		if err := s.revalidateDictionarySource(dictionaryFingerprint); err != nil {
			return report, err
		}
		if err := dictionary.WriteComplete(s.repository, locale, translatedDictionary); err != nil {
			return report, fmt.Errorf("write framework dictionary: %w", err)
		}
		if err := s.revalidateDictionarySource(dictionaryFingerprint); err != nil {
			return report, err
		}
		fingerprints.Sources.Dictionary = dictionaryFingerprint
		if err := s.writeSourceFingerprints(fingerprints); err != nil {
			return report, fmt.Errorf("write dictionary source fingerprint: %w", err)
		}
		report.Dictionaries = len(translatedDictionary)
	}

	if err := s.revalidateThemeSource(siteFingerprint, themeID, themeFingerprint); err != nil {
		return report, err
	}
	if !themeReusable {
		if fingerprints.Sources.Theme != nil {
			fingerprints.Sources.Theme = nil
			if err := s.writeSourceFingerprints(fingerprints); err != nil {
				return report, fmt.Errorf("invalidate theme source fingerprint: %w", err)
			}
		}
		if err := s.revalidateThemeSource(siteFingerprint, themeID, themeFingerprint); err != nil {
			return report, err
		}
		if len(themeSource) > 0 {
			if err := s.themes.WriteLocalizedText(themeID, locale, translatedTheme); err != nil {
				return report, fmt.Errorf("write localized theme settings: %w", err)
			}
		}
		if err := s.revalidateThemeSource(siteFingerprint, themeID, themeFingerprint); err != nil {
			return report, err
		}
		fingerprints.Sources.Theme = &themeSourceFingerprint{ID: themeID, Fingerprint: themeFingerprint}
		if err := s.writeSourceFingerprints(fingerprints); err != nil {
			return report, fmt.Errorf("write theme source fingerprint: %w", err)
		}
		report.Theme = len(translatedTheme)
	}

	// Entity services below have revision CAS, but the repository-owned source
	// documents do not. Make one last all-source check before those durable
	// writes so a stale translated snapshot cannot complete provisioning.
	if err := s.revalidateResourceSources(siteFingerprint, dictionaryFingerprint, themeID, themeFingerprint); err != nil {
		return report, err
	}

	translatedPosts := make(map[string]struct{})
	translatedPages := make(map[string]struct{})
	markTranslatedContent := func(kind, id string) {
		if kind == "Post" {
			translatedPosts[id] = struct{}{}
		} else {
			translatedPages[id] = struct{}{}
		}
	}
	for _, plan := range contentPlans {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		expectedTarget := plan.targetRevision
		input := content.ApplyAITranslationInput{ExpectedSourceRevision: plan.sourceRevision, ExpectedTargetRevision: &expectedTarget, OverwriteManual: true, Content: plan.translated}
		if plan.kind == "Post" {
			if _, err := s.content.ApplyAITranslation(plan.id, locale, input); err != nil {
				return report, fmt.Errorf("apply post %s: %w", plan.id, err)
			}
		} else {
			if _, err := s.content.ApplyAIPageTranslation(plan.id, locale, input); err != nil {
				return report, fmt.Errorf("apply page %s: %w", plan.id, err)
			}
		}
		markTranslatedContent(plan.kind, plan.id)
		if err := s.content.PromoteAITranslation(plan.kind, plan.id, locale, plan.sourceRevision); err != nil {
			return report, fmt.Errorf("promote %s %s: %w", strings.ToLower(plan.kind), plan.id, err)
		}
	}
	for _, plan := range promotionPlans {
		promote := s.content.PromoteAITranslation
		if plan.allowManual {
			promote = s.content.PromoteCurrentTranslation
		}
		if err := promote(plan.kind, plan.id, locale, plan.sourceRevision); err != nil {
			return report, fmt.Errorf("repair promoted %s %s: %w", strings.ToLower(plan.kind), plan.id, err)
		}
		markTranslatedContent(plan.kind, plan.id)
	}
	for _, plan := range releaseContentPlans {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := s.content.ApplyAIReleaseTranslation(plan.kind, plan.id, locale, content.ApplyAIReleaseTranslationInput{
			ExpectedReleaseRevision: plan.releaseRevision,
			ExpectedSourceRevision:  plan.sourceRevision,
			ExpectedTargetRevision:  plan.targetRevision,
			Content:                 plan.translated,
		}); err != nil {
			return report, fmt.Errorf("apply public %s %s: %w", strings.ToLower(plan.kind), plan.id, err)
		}
		markTranslatedContent(plan.kind, plan.id)
	}
	report.Posts = len(translatedPosts)
	report.Pages = len(translatedPages)

	for _, plan := range taxonomyPlans {
		_, err := s.taxonomies.ApplyAITranslation(plan.kind, plan.id, locale, taxonomy.ApplyAITranslationInput{
			ExpectedSourceRevision: plan.sourceRevision, ExpectedTargetRevision: plan.targetRevision,
			Name: plan.translated.Name, Description: plan.translated.Description, SEOTitle: plan.translated.SEOTitle, SEODescription: plan.translated.SEODescription,
		})
		if err != nil {
			return report, fmt.Errorf("apply taxonomy %s: %w", plan.id, err)
		}
		report.Taxonomies++
	}
	for _, plan := range linkPlans {
		input := linkservice.ApplyAILocaleInput{ExpectedSourceRevision: plan.sourceRevision, ExpectedTargetRevision: plan.targetRevision, Name: plan.translated.Name, Description: plan.translated.Description}
		if plan.kind == "LinkGroup" {
			_, err = s.links.ApplyAIGroupLocale(plan.id, locale, input)
		} else {
			_, err = s.links.ApplyAILinkLocale(plan.id, locale, input)
		}
		if err != nil {
			return report, fmt.Errorf("apply link resource %s: %w", plan.id, err)
		}
		report.Links++
	}
	for _, plan := range menuPlans {
		input := menuservice.ApplyAILocaleInput{ExpectedSourceRevision: plan.sourceRevision, ExpectedTargetRevision: plan.targetRevision, Label: plan.label}
		if plan.itemID == "" {
			_, err = s.menus.ApplyAIMenuLocale(plan.menuID, locale, input)
		} else {
			_, err = s.menus.ApplyAIItemLocale(plan.menuID, plan.itemID, locale, input)
		}
		if err != nil {
			return report, fmt.Errorf("apply menu resource %s.%s: %w", plan.menuID, plan.itemID, err)
		}
		report.Menus++
	}
	return report, nil
}

func (s *Service) revalidateResourceSources(siteFingerprint, dictionaryFingerprint, themeID, themeFingerprint string) error {
	if _, err := s.revalidateSiteSource(siteFingerprint, themeID); err != nil {
		return err
	}
	if err := s.revalidateDictionarySource(dictionaryFingerprint); err != nil {
		return err
	}
	return s.revalidateThemeSource(siteFingerprint, themeID, themeFingerprint)
}

func (s *Service) revalidateSiteSource(expectedFingerprint, expectedThemeID string) (domain.SiteConfig, error) {
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		return domain.SiteConfig{}, err
	}
	if site.SourceLocale != localeconfig.FixedSourceLocale || effectiveThemeID(site.ActiveTheme) != expectedThemeID {
		return domain.SiteConfig{}, ErrSourceChanged
	}
	source, exists := site.Locales[localeconfig.FixedSourceLocale]
	if !exists || localizedSiteFingerprint(source) != expectedFingerprint {
		return domain.SiteConfig{}, ErrSourceChanged
	}
	return site, nil
}

func (s *Service) revalidateDictionarySource(expectedFingerprint string) error {
	source, err := dictionary.ReadSource(s.repository)
	if err != nil {
		return err
	}
	if dictionarySourceFingerprint(source) != expectedFingerprint {
		return ErrSourceChanged
	}
	return nil
}

func (s *Service) revalidateThemeSource(siteFingerprint, themeID, expectedFingerprint string) error {
	if _, err := s.revalidateSiteSource(siteFingerprint, themeID); err != nil {
		return err
	}
	source, err := s.themes.LocalizableText(themeID)
	if err != nil {
		return err
	}
	if themeSourceTextFingerprint(themeID, source) != expectedFingerprint {
		return ErrSourceChanged
	}
	return nil
}

func effectiveThemeID(raw string) string {
	if themeID := strings.TrimSpace(raw); themeID != "" {
		return themeID
	}
	return "earth"
}

func (s *Service) requireTarget(locale string) error {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return err
	}
	if config.SourceLocale != localeconfig.FixedSourceLocale {
		return ErrLocaleUnavailable
	}
	for _, definition := range config.Enabled {
		if definition.Code == locale && definition.Enabled {
			return nil
		}
	}
	return ErrLocaleUnavailable
}

func (s *Service) planContent(ctx context.Context, kind string, item domain.Post, locale string) (contentPlan, bool, error) {
	sourceLocale := item.Meta.SourceLocale
	sourceState, sourceExists := item.Meta.Locales[sourceLocale]
	source, contentExists := item.Content[sourceLocale]
	if !sourceExists || !contentExists || strings.TrimSpace(source.Title) == "" {
		return contentPlan{}, false, fmt.Errorf("%s %s: %w", strings.ToLower(kind), item.Meta.ID, ErrLocaleUnavailable)
	}
	if locale == sourceLocale {
		return contentPlan{}, false, nil
	}
	if currentContentLocale(item, locale) {
		return contentPlan{}, false, nil
	}
	translated, err := s.translator.TranslateContent(ctx, sourceLocale, locale, source)
	if err != nil {
		return contentPlan{}, false, fmt.Errorf("translate %s %s: %w", strings.ToLower(kind), item.Meta.ID, err)
	}
	if !completeLocalizedMarkdown(source, translated) {
		return contentPlan{}, false, fmt.Errorf("translate %s %s: translated fields are incomplete", strings.ToLower(kind), item.Meta.ID)
	}
	return contentPlan{kind: kind, id: item.Meta.ID, sourceRevision: sourceState.Revision, targetRevision: item.Meta.Locales[locale].Revision, translated: translated}, true, nil
}

func (s *Service) planContentKind(
	ctx context.Context,
	kind string,
	heads, public []domain.Post,
	locale string,
	headPlans []contentPlan,
	releasePlans []releaseContentPlan,
	promotions []contentPromotionPlan,
) ([]contentPlan, []releaseContentPlan, []contentPromotionPlan, error) {
	publicByID := make(map[string]domain.Post, len(public))
	for _, item := range public {
		if item.Meta.Status == domain.ContentStatusPublished {
			publicByID[item.Meta.ID] = item
		}
	}
	for _, head := range heads {
		plan, needed, err := s.planContent(ctx, kind, head, locale)
		if err != nil {
			return headPlans, releasePlans, promotions, err
		}
		if needed {
			headPlans = append(headPlans, plan)
		}
		released, published := publicByID[head.Meta.ID]
		if !published || locale == released.Meta.SourceLocale {
			continue
		}
		if sameContentSource(head, released) {
			if !needed && currentContentLocale(head, locale) && !currentContentLocale(released, locale) {
				promotions = append(promotions, contentPromotionPlan{
					kind: kind, id: head.Meta.ID, sourceRevision: head.Meta.Locales[head.Meta.SourceLocale].Revision,
					allowManual: head.Meta.Locales[locale].Origin == domain.LocaleOriginManual,
				})
			}
			continue
		}
		releasePlan, releaseNeeded, err := s.planContent(ctx, kind, released, locale)
		if err != nil {
			return headPlans, releasePlans, promotions, err
		}
		if releaseNeeded {
			releasePlans = append(releasePlans, releaseContentPlan{contentPlan: releasePlan, releaseRevision: released.Meta.Revision})
		}
	}
	return headPlans, releasePlans, promotions, nil
}

func (s *Service) planTaxonomy(ctx context.Context, item domain.Taxonomy, locale string) (taxonomyPlan, bool, error) {
	source := item.Locales[item.SourceLocale]
	if strings.TrimSpace(source.Name) == "" {
		return taxonomyPlan{}, false, fmt.Errorf("taxonomy %s: %w", item.ID, ErrLocaleUnavailable)
	}
	if locale == item.SourceLocale {
		return taxonomyPlan{}, false, nil
	}
	if currentTaxonomyLocale(source, item.Locales[locale]) {
		return taxonomyPlan{}, false, nil
	}
	fields, err := s.translator.TranslateFields(ctx, item.SourceLocale, locale, map[string]string{
		"name": source.Name, "description": source.Description, "seoTitle": source.SEOTitle, "seoDescription": source.SEODescription,
	})
	if err != nil {
		return taxonomyPlan{}, false, fmt.Errorf("translate taxonomy %s: %w", item.ID, err)
	}
	translated := domain.LocalizedTaxonomy{Name: fields["name"], Description: fields["description"], SEOTitle: fields["seoTitle"], SEODescription: fields["seoDescription"]}
	return taxonomyPlan{kind: item.Kind, id: item.ID, sourceRevision: source.Revision, targetRevision: item.Locales[locale].Revision, translated: translated}, true, nil
}

func (s *Service) planLink(ctx context.Context, kind, id, sourceLocale string, locales map[string]domain.LocalizedLink, locale string) (linkPlan, bool, error) {
	source := locales[sourceLocale]
	if strings.TrimSpace(source.Name) == "" {
		return linkPlan{}, false, fmt.Errorf("link resource %s: %w", id, ErrLocaleUnavailable)
	}
	if locale == sourceLocale {
		return linkPlan{}, false, nil
	}
	if currentLinkLocale(source, locales[locale]) {
		return linkPlan{}, false, nil
	}
	fields, err := s.translator.TranslateFields(ctx, sourceLocale, locale, map[string]string{"name": source.Name, "description": source.Description})
	if err != nil {
		return linkPlan{}, false, fmt.Errorf("translate link resource %s: %w", id, err)
	}
	return linkPlan{kind: kind, id: id, sourceRevision: source.Revision, targetRevision: locales[locale].Revision, translated: domain.LocalizedLink{Name: fields["name"], Description: fields["description"]}}, true, nil
}

func currentTaxonomyLocale(source, target domain.LocalizedTaxonomy) bool {
	validOrigin := target.Origin == domain.LocaleOriginAI || target.Origin == domain.LocaleOriginManual
	return validOrigin && target.State == "current" && target.SourceRevision == source.Revision && completeStringFields(
		[][2]string{{source.Name, target.Name}, {source.Description, target.Description}, {source.SEOTitle, target.SEOTitle}, {source.SEODescription, target.SEODescription}},
	)
}

func currentLinkLocale(source, target domain.LocalizedLink) bool {
	validOrigin := target.Origin == domain.LocaleOriginAI || target.Origin == domain.LocaleOriginManual
	return validOrigin && target.State == "current" && target.SourceRevision == source.Revision && completeStringFields(
		[][2]string{{source.Name, target.Name}, {source.Description, target.Description}},
	)
}

func currentMenuLocale(locales map[string]domain.LocalizedMenu, sourceLocale, locale string) bool {
	source := locales[sourceLocale]
	target, exists := locales[locale]
	validOrigin := target.Origin == domain.LocaleOriginAI || target.Origin == domain.LocaleOriginManual
	return exists && validOrigin && target.State == "current" && target.SourceRevision == source.Revision && strings.TrimSpace(target.Label) != ""
}

func currentContentLocale(item domain.Post, locale string) bool {
	source, sourceExists := item.Content[item.Meta.SourceLocale]
	sourceState, stateExists := item.Meta.Locales[item.Meta.SourceLocale]
	target, targetExists := item.Content[locale]
	targetState, targetStateExists := item.Meta.Locales[locale]
	validOrigin := targetState.Origin == domain.LocaleOriginAI || targetState.Origin == domain.LocaleOriginManual
	return sourceExists && stateExists && targetExists && targetStateExists && validOrigin && targetState.State == "current" && targetState.SourceRevision == sourceState.Revision && completeLocalizedMarkdown(source, target)
}

func completeLocalizedMarkdown(source, target domain.LocalizedMarkdown) bool {
	return completeStringFields([][2]string{
		{source.Title, target.Title},
		{source.Summary, target.Summary},
		{source.SEOTitle, target.SEOTitle},
		{source.SEODescription, target.SEODescription},
		{source.Markdown, target.Markdown},
	})
}

func completeStringFields(fields [][2]string) bool {
	for _, pair := range fields {
		if strings.TrimSpace(pair[0]) != "" && strings.TrimSpace(pair[1]) == "" {
			return false
		}
	}
	return true
}

func completeLocalizedSite(source, target domain.LocalizedSite) bool {
	if strings.TrimSpace(target.Title) == "" {
		return false
	}
	return (strings.TrimSpace(source.Subtitle) == "" || strings.TrimSpace(target.Subtitle) != "") &&
		(strings.TrimSpace(source.Description) == "" || strings.TrimSpace(target.Description) != "")
}

func completeStringMap(source, target map[string]string) bool {
	if len(source) != len(target) {
		return false
	}
	for key := range source {
		if strings.TrimSpace(target[key]) == "" {
			return false
		}
	}
	return true
}

func sameContentSource(head, released domain.Post) bool {
	if head.Meta.SourceLocale != released.Meta.SourceLocale {
		return false
	}
	locale := head.Meta.SourceLocale
	headState, headStateExists := head.Meta.Locales[locale]
	releaseState, releaseStateExists := released.Meta.Locales[locale]
	headSource, headSourceExists := head.Content[locale]
	releaseSource, releaseSourceExists := released.Content[locale]
	return headStateExists && releaseStateExists && headSourceExists && releaseSourceExists && headState.Revision == releaseState.Revision && headSource == releaseSource
}

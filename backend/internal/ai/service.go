package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/jobs"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/state"
)

var ErrManualProtected = errors.New("translation is manually maintained")

// hreflangMergeWindow defers sibling-locale re-renders so a burst of
// completed translations coalesces into one sweep instead of O(N²) renders
// (docs/13 P21). jobs.Enqueue slides run_after forward on each arrival.
const hreflangMergeWindow = 30 * time.Second

type Task struct {
	ID             int64           `json:"id"`
	ArticleID      model.ArticleID `json:"articleID"`
	Source         model.Locale    `json:"sourceLocale"`
	Target         model.Locale    `json:"targetLocale"`
	SourceRevision int             `json:"sourceRevision"`
	SegmentsTotal  int             `json:"segmentsTotal"`
	SegmentsDone   int             `json:"segmentsDone"`
	Status         string          `json:"status"`
	JobID          int64           `json:"jobID"`
	TokensIn       int             `json:"tokensIn"`
	TokensOut      int             `json:"tokensOut"`
	Provider       string          `json:"provider"`
	Model          string          `json:"model"`
	Error          string          `json:"error"`
	CreatedAt      *time.Time      `json:"createdAt"`
	StartedAt      *time.Time      `json:"startedAt"`
	FinishedAt     *time.Time      `json:"finishedAt"`
}

type jobPayload struct {
	TaskID    int64  `json:"taskID"`
	ArticleID string `json:"articleID"`
	Target    string `json:"target"`
	Force     bool   `json:"force"`
}

type Service struct {
	Provider Provider
	Store    *content.Store
	Index    *index.Index
	Jobs     *jobs.Queue
	DB       *state.DB
	Budget   int
}

func (s *Service) Enqueue(ctx context.Context, articleID model.ArticleID, target model.Locale, force bool) (Task, error) {
	article, ok := s.Index.Article(articleID)
	if !ok {
		return Task{}, fmt.Errorf("article %s not found", articleID)
	}
	if target == "" || target == article.Source {
		return Task{}, errors.New("target locale must differ from source locale")
	}
	if existing := article.Trans[target]; existing != nil && existing.ManualEdited && !force {
		return Task{}, ErrManualProtected
	}
	if article.Trans == nil {
		article.Trans = map[model.Locale]*model.TranslationState{}
	}
	article.Trans[target] = &model.TranslationState{Status: model.TSPending, TranslatedFromRevision: article.SourceRev, Revision: revision(article.Trans[target])}
	if err := s.Store.SaveMetadata(article); err != nil {
		return Task{}, err
	}
	s.Index.UpsertArticle(article)
	now := time.Now().UTC()
	result, err := s.DB.Write().ExecContext(ctx, "INSERT INTO translation_tasks(article_id,source_locale,target_locale,source_revision,status,created_at) VALUES(?,?,?,?,?,?)", article.ID, article.Source, target, article.SourceRev, "pending", now.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, err
	}
	taskID, _ := result.LastInsertId()
	payload, _ := json.Marshal(jobPayload{TaskID: taskID, ArticleID: string(article.ID), Target: string(target), Force: force})
	jobID, err := s.Jobs.Enqueue(ctx, jobs.Job{Kind: "translate", DedupeKey: "translate:" + string(article.ID) + ":" + string(target), Payload: payload, Priority: 10})
	if err != nil {
		return Task{}, err
	}
	if _, err = s.DB.Write().ExecContext(ctx, "UPDATE translation_tasks SET job_id=? WHERE id=?", jobID, taskID); err != nil {
		return Task{}, err
	}
	return Task{ID: taskID, ArticleID: article.ID, Source: article.Source, Target: target, SourceRevision: article.SourceRev, Status: "pending", JobID: jobID, CreatedAt: &now}, nil
}

func (s *Service) Run(ctx context.Context, raw json.RawMessage) error {
	if s.Provider == nil {
		return errors.New("AI provider is not configured")
	}
	var payload jobPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	article, ok := s.Index.Article(model.ArticleID(payload.ArticleID))
	if !ok {
		return fmt.Errorf("article %s not found", payload.ArticleID)
	}
	if current := article.Trans[model.Locale(payload.Target)]; current != nil && current.ManualEdited && !payload.Force {
		return ErrManualProtected
	}
	now := time.Now().UTC()
	if _, err := s.DB.Write().ExecContext(ctx, "UPDATE translation_tasks SET status='translating',started_at=? WHERE id=?", now.Format(time.RFC3339Nano), payload.TaskID); err != nil {
		return err
	}
	if article.Trans == nil {
		article.Trans = map[model.Locale]*model.TranslationState{}
	}
	article.Trans[model.Locale(payload.Target)] = &model.TranslationState{Status: model.TSTranslating, TranslatedFromRevision: article.SourceRev, Revision: revision(article.Trans[model.Locale(payload.Target)])}
	if err := s.Store.SaveMetadata(article); err != nil {
		return err
	}
	source := article.Versions[article.Source]
	if source == nil {
		return errors.New("source locale version is missing")
	}
	if source.Body == "" {
		loaded, err := s.Store.LoadBundle(article.BundleDir)
		if err != nil {
			return err
		}
		article, source = loaded, loaded.Versions[loaded.Source]
	}
	result, err := TranslateMarkdown(ctx, s.Provider, source.Body, article.Source, model.Locale(payload.Target), source.Front.Title, s.Budget)
	if err != nil {
		return s.fail(ctx, article, payload, err)
	}
	front, err := s.translateFront(ctx, source.Front, article.Source, model.Locale(payload.Target))
	if err != nil {
		return s.fail(ctx, article, payload, err)
	}
	front.Updated = &now
	target := model.Locale(payload.Target)
	previous := article.Trans[target]
	article.Trans[target] = &model.TranslationState{Status: model.TSCompleted, TranslatedFromRevision: article.SourceRev, Revision: revision(previous) + 1, Provider: s.Provider.Name(), UpdatedAt: &now, TokensUsed: result.Usage.PromptTokens + result.Usage.CompletionTokens}
	if err := s.Store.SaveVersion(article, target, front, result.Markdown, content.SaveOpts{MirrorAuthoritative: true}); err != nil {
		return s.fail(ctx, article, payload, err)
	}
	s.Index.UpsertArticle(article)
	_, _ = s.Jobs.Enqueue(ctx, jobs.Job{Kind: "render", DedupeKey: string(article.ID) + ":" + string(target), Payload: mustJSON(map[string]string{"articleID": string(article.ID), "locale": string(target)}), Priority: 10})
	// hreflang alternates are symmetric: every other locale page must list the
	// freshly translated version. Coalesce those refreshes into one deferred
	// render per locale within the merge window.
	for loc := range article.Versions {
		if loc == target {
			continue
		}
		payload := mustJSON(map[string]string{"articleID": string(article.ID), "locale": string(loc)})
		_, _ = s.Jobs.Enqueue(ctx, jobs.Job{Kind: "render", DedupeKey: "hreflang:" + string(article.ID) + ":" + string(loc), Payload: payload, Priority: 20, RunAfter: time.Now().Add(hreflangMergeWindow)})
	}
	_, err = s.DB.Write().ExecContext(ctx, "UPDATE translation_tasks SET status='completed',segments_total=?,segments_done=?,tokens_in=?,tokens_out=?,provider=?,model=?,finished_at=?,error=NULL WHERE id=?", lenMustSegments(source.Body), lenMustSegments(source.Body), result.Usage.PromptTokens, result.Usage.CompletionTokens, s.Provider.Name(), providerModel(s.Provider), time.Now().UTC().Format(time.RFC3339Nano), payload.TaskID)
	return err
}

func (s *Service) translateFront(ctx context.Context, source model.FrontMatter, src, dst model.Locale) (model.FrontMatter, error) {
	front := source
	front.Locale = dst
	front.SEO = cloneSEO(source.SEO)
	var segs []Segment
	if source.Title != "" {
		segs = append(segs, Segment{Index: len(segs), Text: source.Title})
	}
	if source.Description != "" {
		segs = append(segs, Segment{Index: len(segs), Text: source.Description})
	}
	if source.SEO != nil && source.SEO.Title != "" {
		segs = append(segs, Segment{Index: len(segs), Text: source.SEO.Title})
	}
	if source.SEO != nil && source.SEO.Description != "" {
		segs = append(segs, Segment{Index: len(segs), Text: source.SEO.Description})
	}
	if len(segs) == 0 {
		return front, nil
	}
	out, err := s.Provider.Translate(ctx, segs, src, dst, source.Title)
	if err != nil {
		return front, err
	}
	if err = ValidateBatch(segs, out); err != nil {
		return front, err
	}
	i := 0
	if source.Title != "" {
		front.Title = out[i]
		i++
	}
	if source.Description != "" {
		front.Description = out[i]
		i++
	}
	if source.SEO != nil && source.SEO.Title != "" {
		front.SEO.Title = out[i]
		i++
	}
	if source.SEO != nil && source.SEO.Description != "" {
		front.SEO.Description = out[i]
	}
	return front, nil
}

func (s *Service) fail(ctx context.Context, article *model.Article, payload jobPayload, cause error) error {
	now := time.Now().UTC()
	target := model.Locale(payload.Target)
	previous := article.Trans[target]
	article.Trans[target] = &model.TranslationState{Status: model.TSFailed, TranslatedFromRevision: article.SourceRev, Revision: revision(previous), Error: cause.Error(), FailedAt: &now, Attempts: revision(previous)}
	_ = s.Store.SaveMetadata(article)
	s.Index.UpsertArticle(article)
	_, _ = s.DB.Write().ExecContext(ctx, "UPDATE translation_tasks SET status='failed',error=?,finished_at=? WHERE id=?", cause.Error(), now.Format(time.RFC3339Nano), payload.TaskID)
	return cause
}

func (s *Service) List(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.DB.Read().QueryContext(ctx, "SELECT id,article_id,source_locale,target_locale,source_revision,status,COALESCE(job_id,0),segments_total,segments_done,tokens_in,tokens_out,COALESCE(provider,''),COALESCE(model,''),COALESCE(error,''),created_at,started_at,finished_at FROM translation_tasks ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		var created string
		var started, finished sql.NullString
		if err := rows.Scan(&task.ID, &task.ArticleID, &task.Source, &task.Target, &task.SourceRevision, &task.Status, &task.JobID, &task.SegmentsTotal, &task.SegmentsDone, &task.TokensIn, &task.TokensOut, &task.Provider, &task.Model, &task.Error, &created, &started, &finished); err != nil {
			return nil, err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, created); err == nil {
			task.CreatedAt = &parsed
		}
		if started.Valid {
			if parsed, err := time.Parse(time.RFC3339Nano, started.String); err == nil {
				task.StartedAt = &parsed
			}
		}
		if finished.Valid {
			if parsed, err := time.Parse(time.RFC3339Nano, finished.String); err == nil {
				task.FinishedAt = &parsed
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func revision(v *model.TranslationState) int {
	if v == nil {
		return 0
	}
	return v.Revision
}
func cloneSEO(v *model.SEO) *model.SEO {
	if v == nil {
		return nil
	}
	out := *v
	out.Keywords = append([]string(nil), v.Keywords...)
	return &out
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func providerModel(p Provider) string {
	if x, ok := p.(*OpenAICompatible); ok {
		return x.model
	}
	return p.Name()
}
func lenMustSegments(markdown string) int {
	segs, _ := Extract(markdown, ExtractOpts{})
	return len(segs)
}

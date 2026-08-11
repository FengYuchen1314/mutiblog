package server

import (
	"errors"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type firstPublishTranslationPlan struct {
	response     map[string]any
	launchTaskID string
}

// prepareFirstPublishTranslation records the translation receipt before the
// source-only static build. Only a child created by this request is launched
// later; reused work keeps its existing owner and preflight failures are
// already terminal durable tasks.
func (s *Server) prepareFirstPublishTranslation(firstPublish bool, entityKind, entityID string) firstPublishTranslationPlan {
	plan := firstPublishTranslationPlan{response: map[string]any{"status": "not-needed"}}
	if !firstPublish || s.translator == nil {
		return plan
	}
	task, created, err := s.translator.Prepare(translation.StartInput{
		EntityKind: entityKind, PostID: entityID, SkipManual: true, RecordPreflightFailure: true,
	})
	switch {
	case err == nil:
		plan.response = translationResponseState("queued", task.ID)
		if created {
			plan.launchTaskID = task.ID
		}
	case errors.Is(err, translation.ErrNoTargets):
		plan.response = map[string]any{"status": "not-needed"}
	case task.ID != "" && providerConfigurationError(err):
		plan.response = translationResponseState("not-configured", task.ID)
	default:
		if s.logger != nil {
			s.logger.Error("automatic first-publish translation could not be prepared", "kind", entityKind, "id", entityID, "error", err)
		}
		plan.response = translationResponseState("failed", task.ID)
	}
	return plan
}

func providerConfigurationError(err error) bool {
	return errors.Is(err, ai.ErrProviderNotFound) || errors.Is(err, ai.ErrKeyMissing) || errors.Is(err, ai.ErrInvalidProvider)
}

func (s *Server) launchFirstPublishTranslation(plan firstPublishTranslationPlan) {
	if plan.launchTaskID == "" || s.translator == nil {
		return
	}
	if !s.translator.LaunchPrepared(plan.launchTaskID) && s.logger != nil {
		s.logger.Warn("prepared first-publish translation remains durable for recovery", "task", plan.launchTaskID)
	}
}

func translationResponseState(status, taskID string) map[string]any {
	state := map[string]any{"status": status}
	if taskID != "" {
		state["taskId"] = taskID
	}
	return state
}

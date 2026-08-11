package server

import (
	"errors"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
)

type publishTranslationPlan struct {
	response     map[string]any
	launchTaskID string
	deferBuild   bool
	blockBuild   bool
}

// preparePublishTranslation records the automatic translation receipt after a
// content release is committed. A queued task owns the only public build: an
// earlier source-only build would either fail exact-locale validation or expose
// stale target copy. Reused work keeps its existing owner and durable provider
// preflight failures leave the previous generated release active.
func (s *Server) preparePublishTranslation(entityKind, entityID string) publishTranslationPlan {
	plan := publishTranslationPlan{response: map[string]any{"status": "not-needed"}}
	if s.translator == nil {
		return plan
	}
	task, created, err := s.translator.Prepare(translation.StartInput{
		EntityKind: entityKind, PostID: entityID, OverwriteManual: true,
		PublishedRelease: true, SkipCurrentAI: true, RecordPreflightFailure: true,
	})
	switch {
	case err == nil:
		// HTTP publish results use one stable non-terminal value. The task center
		// exposes the child's live queued/running state separately.
		plan.response = translationResponseState("queued", task.ID)
		plan.deferBuild = true
		if created {
			plan.launchTaskID = task.ID
		}
	case errors.Is(err, translation.ErrNoTargets):
		plan.response = map[string]any{"status": "not-needed"}
	case task.ID != "" && providerConfigurationError(err):
		plan.response = translationResponseState("not-configured", task.ID)
		plan.blockBuild = true
	default:
		if s.logger != nil {
			s.logger.Error("automatic publish translation could not be prepared", "kind", entityKind, "id", entityID, "error", err)
		}
		plan.response = translationResponseState("failed", task.ID)
		plan.blockBuild = true
	}
	return plan
}

func providerConfigurationError(err error) bool {
	return errors.Is(err, ai.ErrProviderNotFound) || errors.Is(err, ai.ErrKeyMissing) || errors.Is(err, ai.ErrInvalidProvider)
}

func (s *Server) launchPublishTranslation(plan publishTranslationPlan) {
	if plan.launchTaskID == "" || s.translator == nil {
		return
	}
	if !s.translator.LaunchPrepared(plan.launchTaskID) && s.logger != nil {
		s.logger.Warn("prepared publish translation remains durable for recovery", "task", plan.launchTaskID)
	}
}

func translationResponseState(status, taskID string) map[string]any {
	state := map[string]any{"status": status}
	if taskID != "" {
		state["taskId"] = taskID
	}
	return state
}

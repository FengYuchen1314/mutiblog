package ai

import (
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

var (
	ErrProviderNotFound = errors.New("AI provider not found")
	// ErrMaxOutputTokensExceeded prevents a caller from relying on a hidden
	// provider-side clamp. A clamp can turn a valid-looking 2xx answer into a
	// truncated translation, so callers must deliberately fit their request
	// within the configured limit.
	ErrMaxOutputTokensExceeded = errors.New("requested AI output budget exceeds provider limit")
)

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type ProviderView struct {
	domain.AIProviderConfig
	Default   bool   `json:"default"`
	HasKey    bool   `json:"hasKey"`
	MaskedKey string `json:"maskedKey,omitempty"`
}

type UpsertProviderInput struct {
	Name            string
	Kind            string
	BaseURL         string
	Model           string
	Enabled         bool
	TimeoutSeconds  int
	MaxOutputTokens int
	Default         bool
	APIKey          *string
}

type TestResult struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
	LatencyMS  int64  `json:"latencyMs"`
	Response   string `json:"response"`
}

type Service struct {
	repository *fsrepo.Repository
	client     Client
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository, client Client) *Service {
	return &Service{repository: repository, client: client}
}

func (s *Service) List() ([]ProviderView, error) {
	config, err := s.config()
	if err != nil {
		return nil, err
	}
	secrets, err := s.secrets()
	if err != nil {
		return nil, err
	}
	views := make([]ProviderView, 0, len(config.Providers))
	for _, provider := range config.Providers {
		key := strings.TrimSpace(secrets.Providers[provider.ID])
		views = append(views, ProviderView{AIProviderConfig: provider, Default: provider.ID == config.DefaultProvider, HasKey: key != "", MaskedKey: maskKey(key)})
	}
	return views, nil
}

func (s *Service) Upsert(id string, input UpsertProviderInput) (ProviderView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	provider := domain.AIProviderConfig{
		ID: id, Name: strings.TrimSpace(input.Name), Kind: strings.TrimSpace(input.Kind),
		BaseURL: strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"), Model: strings.TrimSpace(input.Model), Enabled: input.Enabled,
		TimeoutSeconds: input.TimeoutSeconds, MaxOutputTokens: input.MaxOutputTokens,
	}
	if provider.Kind == "" {
		provider.Kind = "openai-compatible"
	}
	if provider.TimeoutSeconds == 0 {
		provider.TimeoutSeconds = 45
	}
	if provider.MaxOutputTokens == 0 {
		provider.MaxOutputTokens = 8192
	}
	if !providerIDPattern.MatchString(provider.ID) || provider.Name == "" || len([]rune(provider.Name)) > 80 || provider.Kind != "openai-compatible" || provider.TimeoutSeconds < 5 || provider.TimeoutSeconds > 300 || provider.MaxOutputTokens < 256 || provider.MaxOutputTokens > 65536 {
		return ProviderView{}, ErrInvalidProvider
	}
	if _, err := chatEndpoint(provider); err != nil {
		return ProviderView{}, ErrInvalidProvider
	}
	config, err := s.config()
	if err != nil {
		return ProviderView{}, err
	}
	secrets, err := s.secrets()
	if err != nil {
		return ProviderView{}, err
	}
	previousSecrets := cloneSecrets(secrets)
	found := false
	for index := range config.Providers {
		if config.Providers[index].ID == id {
			config.Providers[index] = provider
			found = true
			break
		}
	}
	if !found {
		config.Providers = append(config.Providers, provider)
	}
	if input.Default || config.DefaultProvider == "" {
		config.DefaultProvider = id
	}
	secretsChanged := input.APIKey != nil
	if secretsChanged {
		key := strings.TrimSpace(*input.APIKey)
		if key == "" {
			delete(secrets.Providers, id)
		} else {
			secrets.Providers[id] = key
		}
		if err := s.repository.WriteYAML("config/secrets.yaml", secrets, true); err != nil {
			return ProviderView{}, err
		}
	}
	if err := s.repository.WriteYAML("config/providers.yaml", config, false); err != nil {
		if secretsChanged {
			if rollbackErr := s.repository.WriteYAML("config/secrets.yaml", previousSecrets, true); rollbackErr != nil {
				return ProviderView{}, errors.Join(err, rollbackErr)
			}
		}
		return ProviderView{}, err
	}
	key := strings.TrimSpace(secrets.Providers[id])
	return ProviderView{AIProviderConfig: provider, Default: config.DefaultProvider == id, HasKey: key != "", MaskedKey: maskKey(key)}, nil
}

func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, err := s.config()
	if err != nil {
		return err
	}
	secrets, err := s.secrets()
	if err != nil {
		return err
	}
	previousSecrets := cloneSecrets(secrets)
	providers := config.Providers[:0]
	found := false
	for _, provider := range config.Providers {
		if provider.ID == id {
			found = true
			continue
		}
		providers = append(providers, provider)
	}
	if !found {
		return ErrProviderNotFound
	}
	config.Providers = providers
	delete(secrets.Providers, id)
	if config.DefaultProvider == id {
		config.DefaultProvider = ""
		if len(providers) > 0 {
			config.DefaultProvider = providers[0].ID
		}
	}
	if err := s.repository.WriteYAML("config/secrets.yaml", secrets, true); err != nil {
		return err
	}
	if err := s.repository.WriteYAML("config/providers.yaml", config, false); err != nil {
		if rollbackErr := s.repository.WriteYAML("config/secrets.yaml", previousSecrets, true); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return nil
}

func (s *Service) Test(ctx context.Context, id string) (TestResult, error) {
	provider, key, err := s.credentials(id)
	if err != nil {
		return TestResult{}, err
	}
	started := time.Now()
	response, err := s.client.Chat(ctx, provider, key, []ChatMessage{
		{Role: "system", Content: "You are a connectivity check. Reply with only OK."},
		{Role: "user", Content: "OK"},
	}, 8)
	if err != nil {
		return TestResult{}, err
	}
	return TestResult{ProviderID: id, Model: provider.Model, LatencyMS: time.Since(started).Milliseconds(), Response: strings.TrimSpace(response)}, nil
}

func (s *Service) DefaultCredentials() (domain.AIProviderConfig, string, error) {
	config, err := s.config()
	if err != nil {
		return domain.AIProviderConfig{}, "", err
	}
	if config.DefaultProvider == "" {
		return domain.AIProviderConfig{}, "", ErrProviderNotFound
	}
	return s.credentials(config.DefaultProvider)
}

func (s *Service) ChatDefault(ctx context.Context, messages []ChatMessage, maxTokens int) (string, domain.AIProviderConfig, error) {
	provider, key, err := s.DefaultCredentials()
	if err != nil {
		return "", domain.AIProviderConfig{}, err
	}
	maxTokens, err = boundedOutputTokens(provider, maxTokens)
	if err != nil {
		return "", provider, err
	}
	response, err := s.client.Chat(ctx, provider, key, messages, maxTokens)
	return response, provider, err
}

func (s *Service) ChatProvider(ctx context.Context, id string, messages []ChatMessage, maxTokens int) (string, domain.AIProviderConfig, error) {
	provider, key, err := s.credentials(id)
	if err != nil {
		return "", domain.AIProviderConfig{}, err
	}
	maxTokens, err = boundedOutputTokens(provider, maxTokens)
	if err != nil {
		return "", provider, err
	}
	response, err := s.client.Chat(ctx, provider, key, messages, maxTokens)
	return response, provider, err
}

// ProviderConfig returns the effective, enabled provider configuration without
// exposing its secret. Translation uses it to size every request before any
// content is sent, rather than depending on ChatProvider to silently clamp.
func (s *Service) ProviderConfig(id string) (domain.AIProviderConfig, error) {
	provider, _, err := s.credentials(id)
	return provider, err
}

func boundedOutputTokens(provider domain.AIProviderConfig, requested int) (int, error) {
	if requested <= 0 {
		return provider.MaxOutputTokens, nil
	}
	if requested > provider.MaxOutputTokens {
		return 0, ErrMaxOutputTokensExceeded
	}
	return requested, nil
}

func (s *Service) credentials(id string) (domain.AIProviderConfig, string, error) {
	config, err := s.config()
	if err != nil {
		return domain.AIProviderConfig{}, "", err
	}
	secrets, err := s.secrets()
	if err != nil {
		return domain.AIProviderConfig{}, "", err
	}
	for _, provider := range config.Providers {
		if provider.ID == id {
			if !provider.Enabled {
				return domain.AIProviderConfig{}, "", ErrInvalidProvider
			}
			// Providers written before the explicit output-budget field existed are
			// still valid. Normalize their in-memory effective value so a legacy
			// configuration gets the same safe request sizing as a newly saved one.
			if provider.MaxOutputTokens == 0 {
				provider.MaxOutputTokens = 8192
			}
			key := strings.TrimSpace(secrets.Providers[id])
			if key == "" {
				return domain.AIProviderConfig{}, "", ErrKeyMissing
			}
			return provider, key, nil
		}
	}
	return domain.AIProviderConfig{}, "", ErrProviderNotFound
}

func (s *Service) config() (domain.AIProvidersConfig, error) {
	var config domain.AIProvidersConfig
	if err := s.repository.ReadYAML("config/providers.yaml", &config); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.AIProvidersConfig{SchemaVersion: domain.SchemaVersion, Providers: []domain.AIProviderConfig{}}, nil
		}
		return domain.AIProvidersConfig{}, err
	}
	sort.SliceStable(config.Providers, func(i, j int) bool { return config.Providers[i].Name < config.Providers[j].Name })
	return config, nil
}

func (s *Service) secrets() (domain.SecretsConfig, error) {
	var secrets domain.SecretsConfig
	if err := s.repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}}, nil
		}
		return domain.SecretsConfig{}, err
	}
	if secrets.Providers == nil {
		secrets.Providers = map[string]string{}
	}
	return secrets, nil
}

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= 4 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}

func cloneSecrets(source domain.SecretsConfig) domain.SecretsConfig {
	cloned := source
	cloned.Providers = make(map[string]string, len(source.Providers))
	for id, key := range source.Providers {
		cloned.Providers[id] = key
	}
	return cloned
}

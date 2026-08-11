package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestProviderPersistenceMaskingAndConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{HTTPClient: server.Client()})
	key := "temporary-test-secret"
	view, err := service.Upsert("test-provider", UpsertProviderInput{Name: "Test", BaseURL: server.URL + "/v1", Model: "test-model", Enabled: true, Default: true, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasKey || view.MaskedKey != "••••cret" {
		t.Fatalf("view = %#v", view)
	}
	info, err := os.Stat(filepath.Join(repository.Root(), "config", "secrets.yaml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %v, err = %v", info.Mode().Perm(), err)
	}
	result, err := service.Test(context.Background(), "test-provider")
	if err != nil || result.Response != "OK" {
		t.Fatalf("test result = %#v, err = %v", result, err)
	}
	views, err := service.List()
	if err != nil || len(views) != 1 || views[0].MaskedKey != "••••cret" {
		t.Fatalf("views = %#v, err = %v", views, err)
	}
	if err := service.Delete("test-provider"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.DefaultCredentials(); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("default credentials error = %v", err)
	}
}

func TestChatProviderRejectsOutputBudgetAboveConfiguredLimit(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{})
	key := "temporary-test-secret"
	if _, err := service.Upsert("test-provider", UpsertProviderInput{Name: "Test", BaseURL: "https://example.com/v1", Model: "test-model", Enabled: true, Default: true, MaxOutputTokens: 256, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ChatProvider(context.Background(), "test-provider", nil, 257); !errors.Is(err, ErrMaxOutputTokensExceeded) {
		t.Fatalf("ChatProvider() error = %v", err)
	}
}

func TestProviderCredentialsSeparateMissingKeyFromInvalidConfiguration(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{})
	key := "temporary-test-secret"
	if _, err := service.Upsert("test-provider", UpsertProviderInput{
		Name: "Test", BaseURL: "https://example.com/v1", Model: "test-model", Enabled: true, Default: true, APIKey: &key,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{
		SchemaVersion: domain.SchemaVersion,
		Providers:     map[string]string{"test-provider": " \t "},
	}, true); err != nil {
		t.Fatal(err)
	}
	views, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].HasKey || views[0].MaskedKey != "" {
		t.Fatalf("whitespace key views = %#v", views)
	}
	if _, err := service.Test(context.Background(), "test-provider"); !errors.Is(err, ErrKeyMissing) || errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("missing key error = %v", err)
	}
	view, err := service.Upsert("test-provider", UpsertProviderInput{
		Name: "Test", BaseURL: "https://example.com/v1", Model: "test-model", Enabled: false, Default: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.HasKey || view.MaskedKey != "" {
		t.Fatalf("whitespace key view = %#v", view)
	}
	if _, err := service.Test(context.Background(), "test-provider"); !errors.Is(err, ErrInvalidProvider) || errors.Is(err, ErrKeyMissing) {
		t.Fatalf("invalid provider error = %v", err)
	}
}

func TestGoogleFreeProviderUsesNativeTranslationWithoutAPIKey(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/translate_a/single" || strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("authorization = %q", authorization)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		source := r.Form.Get("q")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"translated:" + source, source}}})
	}))
	defer server.Close()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{HTTPClient: server.Client()})
	view, err := service.Upsert("google-free", UpsertProviderInput{
		Name: "Google Free Translate", Kind: ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single",
		Enabled: true, Default: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.HasKey || view.MaskedKey != "" || view.Model != GoogleFreeDefaultModel || view.TimeoutSeconds != 45 || view.MaxOutputTokens != 8192 {
		t.Fatalf("view = %#v", view)
	}
	provider, key, err := service.DefaultCredentials()
	if err != nil || provider.Kind != ProviderKindGoogleFree || key != "" {
		t.Fatalf("DefaultCredentials() = %#v, %q, %v", provider, key, err)
	}
	result, err := service.Test(context.Background(), "google-free")
	if err != nil || result.Response != "translated:连接测试" {
		t.Fatalf("Test() = %#v, %v", result, err)
	}
	translated, translatedProvider, err := service.TranslateProvider(context.Background(), "google-free", "zh-CN", "en", "你好")
	if err != nil || translated != "translated:你好" || translatedProvider.Kind != ProviderKindGoogleFree {
		t.Fatalf("TranslateProvider() = %q, %#v, %v", translated, translatedProvider, err)
	}
	if _, _, err := service.ChatProvider(context.Background(), "google-free", nil, 8); !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("ChatProvider() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("native translation requests = %d, want 2", requests)
	}
}

func TestSwitchingProviderToGoogleFreeDeletesStoredSecret(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{})
	key := "must-be-removed"
	if _, err := service.Upsert("provider", UpsertProviderInput{
		Name: "OpenAI", Kind: ProviderKindOpenAICompatible, BaseURL: "https://example.com/v1", Model: "model",
		Enabled: true, Default: true, APIKey: &key,
	}); err != nil {
		t.Fatal(err)
	}
	view, err := service.Upsert("provider", UpsertProviderInput{
		Name: "Google", Kind: ProviderKindGoogleFree, Enabled: true, Default: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.HasKey || view.BaseURL != GoogleFreeDefaultEndpoint || view.Model != GoogleFreeDefaultModel {
		t.Fatalf("view = %#v", view)
	}
	var secrets domain.SecretsConfig
	if err := repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		t.Fatal(err)
	}
	if _, exists := secrets.Providers["provider"]; exists {
		t.Fatalf("stale Google-free secret remains: %#v", secrets.Providers)
	}
}

func TestMigrateLegacyDefaultOnlyReplacesExactKeylessSeed(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacyProvider := legacyQwenSeedProvider()
	legacyProvider.Kind = ""
	legacyProvider.TimeoutSeconds = 0
	legacyProvider.MaxOutputTokens = 0
	legacy := domain.AIProvidersConfig{
		SchemaVersion: domain.SchemaVersion, DefaultProvider: "qwen-free", Providers: []domain.AIProviderConfig{legacyProvider},
	}
	if err := repository.WriteYAML("config/providers.yaml", legacy, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{
		SchemaVersion: domain.SchemaVersion, Providers: map[string]string{"qwen-free": "   "},
	}, true); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, Client{})
	migrated, err := service.MigrateLegacyDefault()
	if err != nil || !migrated {
		t.Fatalf("MigrateLegacyDefault() = %t, %v", migrated, err)
	}
	var config domain.AIProvidersConfig
	if err := repository.ReadYAML("config/providers.yaml", &config); err != nil {
		t.Fatal(err)
	}
	if config.DefaultProvider != "google-free" || len(config.Providers) != 1 || config.Providers[0] != defaultGoogleFreeProvider() {
		t.Fatalf("migrated config = %#v", config)
	}
	var secrets domain.SecretsConfig
	if err := repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		t.Fatal(err)
	}
	if _, exists := secrets.Providers["qwen-free"]; exists {
		t.Fatalf("legacy empty secret remains: %#v", secrets.Providers)
	}
	if migrated, err := service.MigrateLegacyDefault(); err != nil || migrated {
		t.Fatalf("second MigrateLegacyDefault() = %t, %v", migrated, err)
	}
}

func TestMigrateLegacyDefaultPreservesCustomizedOrCredentialedProviders(t *testing.T) {
	tests := []struct {
		name      string
		providers []domain.AIProviderConfig
		key       string
	}{
		{name: "stored-key", providers: []domain.AIProviderConfig{legacyQwenSeedProvider()}, key: "user-key"},
		{name: "customized", providers: []domain.AIProviderConfig{func() domain.AIProviderConfig {
			provider := legacyQwenSeedProvider()
			provider.Name = "My Qwen"
			return provider
		}()}},
		{name: "additional-provider", providers: []domain.AIProviderConfig{legacyQwenSeedProvider(), {ID: "other", Name: "Other", Kind: ProviderKindOpenAICompatible, BaseURL: "https://example.com/v1", Model: "model", Enabled: true, TimeoutSeconds: 45, MaxOutputTokens: 8192}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			original := domain.AIProvidersConfig{SchemaVersion: domain.SchemaVersion, DefaultProvider: "qwen-free", Providers: test.providers}
			if err := repository.WriteYAML("config/providers.yaml", original, false); err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{"qwen-free": test.key}}, true); err != nil {
				t.Fatal(err)
			}
			migrated, err := NewService(repository, Client{}).MigrateLegacyDefault()
			if err != nil || migrated {
				t.Fatalf("MigrateLegacyDefault() = %t, %v", migrated, err)
			}
			var after domain.AIProvidersConfig
			if err := repository.ReadYAML("config/providers.yaml", &after); err != nil {
				t.Fatal(err)
			}
			if after.DefaultProvider != original.DefaultProvider || len(after.Providers) != len(original.Providers) {
				t.Fatalf("configuration was changed: before=%#v after=%#v", original, after)
			}
		})
	}
}

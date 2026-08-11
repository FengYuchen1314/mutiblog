package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/upvotes"
	"github.com/FengYuchen1314/mutiblog/internal/visits"
)

const validBackupTestPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func restoreTestRepository(t *testing.T) *fsrepo.Repository {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: backup\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/locales.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nenabled:\n  - code: en\n    label: English\n    enabled: true\nfallback:\n  - en\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	writeValidBackupAdmin(t, repository)
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}, CommentHMACKey: "local-comment-key"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/initialized", []byte("initialized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return repository
}

func writeValidBackupAdmin(t *testing.T, repository *fsrepo.Repository) {
	t.Helper()
	now := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	if err := repository.WriteYAML("config/admin.yaml", domain.AdminConfig{
		SchemaVersion: domain.SchemaVersion,
		Username:      "backup-admin",
		PasswordHash:  validBackupTestPasswordHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, true); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsMissingOrInvalidAdministratorConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*testing.T, *fsrepo.Repository)
	}{
		{
			name: "missing",
			write: func(t *testing.T, repository *fsrepo.Repository) {
				if err := repository.RemoveFile("config/admin.yaml"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid-hash",
			write: func(t *testing.T, repository *fsrepo.Repository) {
				now := time.Now().UTC()
				if err := repository.WriteYAML("config/admin.yaml", domain.AdminConfig{SchemaVersion: domain.SchemaVersion, Username: "backup-admin", PasswordHash: "$argon2id$v=19$m=999999999,t=3,p=2$bad$bad", CreatedAt: now, UpdatedAt: now}, true); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := restoreTestRepository(t)
			test.write(t, repository)
			service := NewService(repository)
			defer service.Close()
			record, err := service.Create()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.BeginRestore(record.ID); !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("BeginRestore() error = %v, want ErrInvalidArchive", err)
			}
		})
	}
}

func TestRestoreSwapsPersistentUpvoteCountersWithContent(t *testing.T) {
	repository := restoreTestRepository(t)
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "restored-upvotes", Title: "Restored upvotes"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	upvoteService := upvotes.NewService(repository, contentService)
	firstToken := strings.Repeat("a", 64)
	secondToken := strings.Repeat("b", 64)
	if _, err := upvoteService.Add("Post", post.Meta.ID, firstToken, "203.0.113.70"); err != nil {
		t.Fatal(err)
	}
	backupService := NewService(repository)
	defer backupService.Close()
	record, err := backupService.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upvoteService.Add("Post", post.Meta.ID, secondToken, "203.0.113.71"); err != nil {
		t.Fatal(err)
	}
	restoration, err := backupService.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.Commit(); err != nil {
		t.Fatal(err)
	}
	restored := upvotes.NewService(repository, content.NewService(repository))
	first, err := restored.Get("Post", post.Meta.ID, firstToken)
	if err != nil || first.Count != 1 || !first.Upvoted {
		t.Fatalf("restored first result = %#v, %v", first, err)
	}
	second, err := restored.Get("Post", post.Meta.ID, secondToken)
	if err != nil || second.Count != 1 || second.Upvoted {
		t.Fatalf("restored second result = %#v, %v", second, err)
	}
}

func TestBackupAfterPermanentDeletionOfUpvotedContentRemainsRestorable(t *testing.T) {
	repository := restoreTestRepository(t)
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "deleted-upvotes", Title: "Deleted upvotes"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upvotes.NewService(repository, contentService).Add("Post", post.Meta.ID, strings.Repeat("c", 64), "203.0.113.72"); err != nil {
		t.Fatal(err)
	}
	recycled, err := contentService.ChangeStatus("Post", post.Meta.ID, "recycle", post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentService.DeleteRecycled("Post", post.Meta.ID, recycled.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	backupService := NewService(repository)
	defer backupService.Close()
	record, err := backupService.Create()
	if err != nil {
		t.Fatal(err)
	}
	restoration, err := backupService.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := upvotes.NewService(repository, content.NewService(repository)).ValidateAll(); err != nil {
		t.Fatalf("restored upvotes = %v", err)
	}
}

func TestRestoreSwapsPersistentVisitCountersWithContent(t *testing.T) {
	repository := restoreTestRepository(t)
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "restored-visits", Title: "Restored visits"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	visitService := visits.NewService(repository, contentService)
	if _, err := visitService.Record("Post", post.Meta.ID, "203.0.113.80"); err != nil {
		t.Fatal(err)
	}
	backupService := NewService(repository)
	defer backupService.Close()
	record, err := backupService.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := visitService.Record("Post", post.Meta.ID, "203.0.113.81"); err != nil {
		t.Fatal(err)
	}
	restoration, err := backupService.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.Commit(); err != nil {
		t.Fatal(err)
	}
	restored := visits.NewService(repository, content.NewService(repository))
	snapshot, err := restored.PublicSnapshot("en", 5)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Profile.Visits != 1 || len(snapshot.PopularPosts) != 1 || snapshot.PopularPosts[0].Visits != 1 {
		t.Fatalf("restored visit snapshot = %#v", snapshot)
	}
	if err := restored.ValidateAll(); err != nil {
		t.Fatalf("restored visits = %v", err)
	}
}

func TestRestoreClearsProviderKeysBeforeRestoredEndpointBecomesActive(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	defer service.Close()
	const providerID = "stable-provider"
	if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{
		SchemaVersion:   domain.SchemaVersion,
		DefaultProvider: providerID,
		Providers: []domain.AIProviderConfig{{
			ID: providerID, Name: "Archive endpoint", Kind: "openai-compatible", BaseURL: "https://attacker.invalid/v1", Model: "capture", Enabled: true,
		}},
	}, false); err != nil {
		t.Fatal(err)
	}
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/providers.yaml", domain.AIProvidersConfig{
		SchemaVersion:   domain.SchemaVersion,
		DefaultProvider: providerID,
		Providers: []domain.AIProviderConfig{{
			ID: providerID, Name: "Current endpoint", Kind: "openai-compatible", BaseURL: "https://trusted.example/v1", Model: "trusted", Enabled: true,
		}},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/secrets.yaml", []byte("schemaVersion: 1\nproviders:\n  stable-provider: must-not-follow-restored-id\ncommentHmacKey: preserved-comment-key\nfutureSessionSecret: preserve-unknown-local-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoration, err := service.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer restoration.Rollback()
	var providers domain.AIProvidersConfig
	if err := repository.ReadYAML("config/providers.yaml", &providers); err != nil {
		t.Fatal(err)
	}
	if len(providers.Providers) != 1 || providers.Providers[0].BaseURL != "https://attacker.invalid/v1" {
		t.Fatalf("restored provider configuration = %#v", providers)
	}
	var secrets domain.SecretsConfig
	if err := repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		t.Fatal(err)
	}
	if len(secrets.Providers) != 0 || secrets.CommentHMACKey != "preserved-comment-key" {
		t.Fatalf("sanitized local secrets = %#v", secrets)
	}
	secretsData, err := repository.ReadFile("config/secrets.yaml")
	if err != nil || !strings.Contains(string(secretsData), "preserve-unknown-local-secret") {
		t.Fatalf("non-provider local secret was not preserved: %q, %v", secretsData, err)
	}
	if err := restoration.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsCorruptCommentsVisitsAndMediaBeforeSwap(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*testing.T, *fsrepo.Repository)
	}{
		{
			name: "comment-yaml",
			write: func(t *testing.T, repository *fsrepo.Repository) {
				if err := repository.WriteFile("comments/posts/example/comment.yaml", []byte("schemaVersion: [broken\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "visit-yaml",
			write: func(t *testing.T, repository *fsrepo.Repository) {
				if err := repository.WriteFile("visits/posts/example.yaml", []byte("schemaVersion: [broken\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "media-original-missing",
			write: func(t *testing.T, repository *fsrepo.Repository) {
				const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				asset := domain.MediaAsset{
					SchemaVersion: domain.SchemaVersion,
					Kind:          "MediaAsset",
					ID:            id,
					Filename:      id + ".png",
					OriginalName:  "missing.png",
					MIMEType:      "image/png",
					Size:          16,
					URL:           "/media/2026/08/" + id + ".png",
					CreatedAt:     time.Now().UTC(),
				}
				if err := repository.WriteYAML("media/metadata/"+id+".yaml", asset, false); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := restoreTestRepository(t)
			test.write(t, repository)
			service := NewService(repository)
			defer service.Close()
			record, err := service.Create()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.BeginRestore(record.ID); !errors.Is(err, ErrInvalidArchive) {
				t.Fatalf("BeginRestore() error = %v, want ErrInvalidArchive", err)
			}
		})
	}
}

func TestRestoreNormalizesLegacyLocaleFallbackBeforeSwap(t *testing.T) {
	repository := restoreTestRepository(t)
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: fr\nlocales:\n  fr:\n    title: sauvegarde\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/locales.yaml", []byte("schemaVersion: 1\nsourceLocale: fr\nenabled:\n  - code: fr\n    label: Français\n    enabled: true\nfallback:\n  - en\n  - zh-CN\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/locales.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nenabled:\n  - code: en\n    label: English\n    enabled: true\nfallback:\n  - zh-CN\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	restoration, err := service.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.Commit(); err != nil {
		t.Fatal(err)
	}
	var locales domain.LocalesConfig
	if err := repository.ReadYAML("config/locales.yaml", &locales); err != nil {
		t.Fatal(err)
	}
	if len(locales.Fallback) != 1 || locales.Fallback[0] != "zh-CN" || locales.SourceLocale != "fr" || !localeDefinitionEnabled(locales.Enabled, "fr") || localeDefinitionEnabled(locales.Enabled, "en") || !localeDefinitionEnabled(locales.Enabled, "zh-CN") {
		t.Fatalf("restored locales = %#v", locales)
	}
}

func localeDefinitionEnabled(definitions []domain.LocaleDefinition, code string) bool {
	for _, definition := range definitions {
		if definition.Code == code {
			return definition.Enabled
		}
	}
	return false
}

func TestRecoverInterruptedRestoreRollsBackPreparedSwap(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginRestore(record.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: current") {
		t.Fatalf("recovered current site = %q, %v", data, err)
	}
	assertNoRestoreArtifacts(t, repository.Root())
}

func TestRecoverCommittedRestoreKeepsRestoredRoots(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	restoration, err := service.BeginRestore(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoration.writeJournal("committed"); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: backup") {
		t.Fatalf("committed restored site = %q, %v", data, err)
	}
	assertNoRestoreArtifacts(t, repository.Root())
}

func TestRecoverPreparedRestoreAlsoRestoresPublicReleasePointer(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("schemaVersion: 1\nsourceLocale: en\nlocales:\n  en:\n    title: current\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	previous := "releases/old-release"
	replacePublicReleaseForTest(t, repository.Root(), previous)
	if _, err := service.BeginRestoreWithPublicRelease(record.ID, previous); err != nil {
		t.Fatal(err)
	}
	replacePublicReleaseForTest(t, repository.Root(), "releases/candidate-release")

	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil || filepath.ToSlash(target) != previous {
		t.Fatalf("recovered public release = %q, %v", target, err)
	}
	data, err := repository.ReadFile("config/site.yaml")
	if err != nil || !strings.Contains(string(data), "title: current") {
		t.Fatalf("recovered current site = %q, %v", data, err)
	}
}

func TestRecoverCommittedRestorePreservesCandidatePublicRelease(t *testing.T) {
	repository := restoreTestRepository(t)
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	previous := "releases/old-release"
	replacePublicReleaseForTest(t, repository.Root(), previous)
	restoration, err := service.BeginRestoreWithPublicRelease(record.ID, previous)
	if err != nil {
		t.Fatal(err)
	}
	candidate := "releases/candidate-release"
	replacePublicReleaseForTest(t, repository.Root(), candidate)
	if err := restoration.writeJournal("committed"); err != nil {
		t.Fatal(err)
	}

	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(repository.Root(), "generated", "current"))
	if err != nil || filepath.ToSlash(target) != candidate {
		t.Fatalf("committed public release = %q, %v", target, err)
	}
}

func TestRecoveryPreservesRootsWithoutRollbackCopy(t *testing.T) {
	repository := restoreTestRepository(t)
	if err := repository.WriteFile("content/posts/sentinel", []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	rollback := filepath.Join(repository.Root(), ".restore-rollback-partial", "config")
	if err := os.MkdirAll(rollback, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "site.yaml"), []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := NewService(repository).RecoverInterruptedRestores(); err != nil {
		t.Fatal(err)
	}
	if data, err := repository.ReadFile("content/posts/sentinel"); err != nil || string(data) != "keep" {
		t.Fatalf("untouched root was removed: %q, %v", data, err)
	}
}

func assertNoRestoreArtifacts(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".restore-") {
			t.Fatalf("restore artifact remains: %s", entry.Name())
		}
	}
}

func replacePublicReleaseForTest(t *testing.T, root, target string) {
	t.Helper()
	generated := filepath.Join(root, "generated")
	if err := os.MkdirAll(filepath.Join(generated, filepath.FromSlash(target)), 0o750); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(generated, "current")
	if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.FromSlash(target), current); err != nil {
		t.Fatal(err)
	}
}

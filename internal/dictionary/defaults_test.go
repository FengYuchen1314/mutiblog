package dictionary

import (
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestEnsureAddsMissingDefaultsWithoutOverwritingMaintainedCopy(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("content/dictionaries/en.yaml", map[string]string{"home": "Custom home"}, false); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(repository); err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := repository.ReadYAML("content/dictionaries/en.yaml", &values); err != nil {
		t.Fatal(err)
	}
	if values["home"] != "Custom home" || values["commentsClosed"] == "" {
		t.Fatalf("merged dictionary = %#v", values)
	}
}

func TestWriteKeepsOnlyFrameworkKeysAndRestoresBuiltInSafetyNet(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(repository, "fr", map[string]string{"home": "Accueil", "unknown": "ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := Write(repository, "en", map[string]string{"home": "Custom home"}); err != nil {
		t.Fatal(err)
	}
	dictionaries, err := Read(repository)
	if err != nil {
		t.Fatal(err)
	}
	if dictionaries["fr"]["home"] != "Accueil" || dictionaries["fr"]["unknown"] != "" {
		t.Fatalf("French dictionary = %#v", dictionaries["fr"])
	}
	if dictionaries["en"]["home"] != "Custom home" || dictionaries["en"]["commentsUnavailable"] == "" {
		t.Fatalf("English safety dictionary = %#v", dictionaries["en"])
	}
}

func TestReadRejectsNonCanonicalDictionaryFilename(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("content/dictionaries/EN.yaml", map[string]string{"home": "Home"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(repository); err == nil {
		t.Fatal("expected non-canonical dictionary filename to be reported")
	}
}

package dictionary

import (
	"reflect"
	"strings"
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
		t.Fatalf("built-in English dictionary = %#v", dictionaries["en"])
	}
}

func TestBuiltInDictionariesMatchChineseRequiredKeys(t *testing.T) {
	required := RequiredKeys()
	if len(required) != len(defaults["zh-CN"]) || len(required) != len(defaults["en"]) {
		t.Fatalf("built-in dictionary key counts = required %d, zh-CN %d, en %d", len(required), len(defaults["zh-CN"]), len(defaults["en"]))
	}
	for _, key := range required {
		if defaults["zh-CN"][key] == "" || defaults["en"][key] == "" {
			t.Fatalf("built-in dictionary key %q is missing or empty", key)
		}
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

func TestSourceValuesReturnsCompleteDefensiveCopy(t *testing.T) {
	first := SourceValues()
	if !reflect.DeepEqual(first, defaults["zh-CN"]) {
		t.Fatalf("SourceValues() = %#v, want complete zh-CN defaults", first)
	}
	first["home"] = "changed"
	delete(first, "archives")
	second := SourceValues()
	if second["home"] != defaults["zh-CN"]["home"] || second["archives"] == "" || len(second) != len(defaults["zh-CN"]) {
		t.Fatalf("SourceValues() shared mutable state: %#v", second)
	}
}

func TestWriteCompleteValidatesWholeDictionaryBeforeAtomicWrite(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(repository); err != nil {
		t.Fatal(err)
	}
	complete := SourceValues()
	for key, value := range complete {
		complete[key] = "FR " + value
	}
	if err := WriteComplete(repository, "fr", complete); err != nil {
		t.Fatal(err)
	}
	var baseline map[string]string
	if err := repository.ReadYAML("content/dictionaries/fr.yaml", &baseline); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, complete) {
		t.Fatalf("complete dictionary = %#v, want %#v", baseline, complete)
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{name: "missing key", mutate: func(values map[string]string) { delete(values, "home") }},
		{name: "unknown key", mutate: func(values map[string]string) { values["unknown"] = "inconnu" }},
		{name: "empty value", mutate: func(values map[string]string) { values["home"] = " \t " }},
		{name: "value too long", mutate: func(values map[string]string) {
			values["home"] = strings.Repeat("界", frameworkDictionaryValueMaxRunes+1)
		}},
		{name: "placeholder mismatch", mutate: func(values map[string]string) { values["home"] = "Accueil {name}" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneDictionaryValues(complete)
			test.mutate(candidate)
			if err := WriteComplete(repository, "fr", candidate); err == nil {
				t.Fatal("WriteComplete() accepted an incomplete or invalid dictionary")
			}
			var after map[string]string
			if err := repository.ReadYAML("content/dictionaries/fr.yaml", &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, baseline) {
				t.Fatalf("failed complete write changed dictionary: before=%#v after=%#v", baseline, after)
			}
		})
	}
	if err := WriteComplete(repository, "zh-CN", SourceValues()); err == nil {
		t.Fatal("WriteComplete() replaced the source dictionary")
	}
}

func TestWriteCompleteUsesMaintainedChineseSource(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(repository); err != nil {
		t.Fatal(err)
	}
	source, err := ReadSource(repository)
	if err != nil {
		t.Fatal(err)
	}
	source["home"] = "自定义首页"
	source["customGreeting"] = "你好 {name}"
	if err := repository.WriteYAML("content/dictionaries/zh-CN.yaml", source, false); err != nil {
		t.Fatal(err)
	}
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = "FR " + value
	}
	if err := WriteComplete(repository, "fr", target); err != nil {
		t.Fatal(err)
	}
	var stored map[string]string
	if err := repository.ReadYAML("content/dictionaries/fr.yaml", &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, target) {
		t.Fatalf("stored maintained dictionary = %#v, want %#v", stored, target)
	}
}

func TestFrameworkPlaceholderComparisonUsesAnExactMultiset(t *testing.T) {
	source := "Hello {name}: %s / {{.Total}} / {name}"
	if !sameFrameworkPlaceholders(source, "{{.Total}}，{name}：%s；{name}") {
		t.Fatal("equivalent reordered placeholders were rejected")
	}
	if sameFrameworkPlaceholders(source, "{{.Total}}，{name}：%s") {
		t.Fatal("missing repeated placeholder was accepted")
	}
	if sameFrameworkPlaceholders(source, "{{.Total}}，{other}：%s；{name}") {
		t.Fatal("changed placeholder name was accepted")
	}
}

func cloneDictionaryValues(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

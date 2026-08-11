package localization

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

const sourceFingerprintSchemaVersion = 1

// sourceFingerprintState is stored at state/localization/<locale>.yaml. It is
// retry metadata, not public content: each non-empty entry certifies that the
// corresponding complete target was written from exactly that source digest.
type sourceFingerprintState struct {
	SchemaVersion int                      `yaml:"schemaVersion"`
	Locale        string                   `yaml:"locale"`
	Sources       sourceFingerprintSources `yaml:"sources"`
}

type sourceFingerprintSources struct {
	Site       string                  `yaml:"site,omitempty"`
	Dictionary string                  `yaml:"dictionary,omitempty"`
	Theme      *themeSourceFingerprint `yaml:"theme,omitempty"`
}

type themeSourceFingerprint struct {
	ID          string `yaml:"id"`
	Fingerprint string `yaml:"fingerprint"`
}

type fingerprintField struct {
	Name  string
	Value string
}

func (s *Service) readSourceFingerprints(locale string) (sourceFingerprintState, error) {
	state := sourceFingerprintState{SchemaVersion: sourceFingerprintSchemaVersion, Locale: locale}
	err := s.repository.ReadYAML(sourceFingerprintPath(locale), &state)
	if errors.Is(err, os.ErrNotExist) {
		return sourceFingerprintState{SchemaVersion: sourceFingerprintSchemaVersion, Locale: locale}, nil
	}
	if err != nil {
		return sourceFingerprintState{}, err
	}
	if state.SchemaVersion != sourceFingerprintSchemaVersion || state.Locale != locale {
		return sourceFingerprintState{}, errors.New("invalid localization source fingerprint state")
	}
	return state, nil
}

func (s *Service) writeSourceFingerprints(state sourceFingerprintState) error {
	if state.SchemaVersion != sourceFingerprintSchemaVersion || state.Locale == "" {
		return errors.New("invalid localization source fingerprint state")
	}
	return s.repository.WriteYAML(sourceFingerprintPath(state.Locale), state, false)
}

func sourceFingerprintPath(locale string) string {
	return filepath.Join("state", "localization", locale+".yaml")
}

func localizedSiteFingerprint(source domain.LocalizedSite) string {
	return fingerprintSource("site", "", []fingerprintField{
		{Name: "title", Value: source.Title},
		{Name: "subtitle", Value: source.Subtitle},
		{Name: "description", Value: source.Description},
	})
}

func dictionarySourceFingerprint(source map[string]string) string {
	return fingerprintStringMap("dictionary", "", source)
}

func themeSourceTextFingerprint(themeID string, source map[string]string) string {
	return fingerprintStringMap("theme", themeID, source)
}

func fingerprintStringMap(component, scope string, source map[string]string) string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]fingerprintField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, fingerprintField{Name: key, Value: source[key]})
	}
	return fingerprintSource(component, scope, fields)
}

func fingerprintSource(component, scope string, fields []fingerprintField) string {
	payload := binary.AppendUvarint(nil, sourceFingerprintSchemaVersion)
	appendPart := func(value string) {
		payload = binary.AppendUvarint(payload, uint64(len(value)))
		payload = append(payload, value...)
	}
	appendPart(component)
	appendPart(scope)
	payload = binary.AppendUvarint(payload, uint64(len(fields)))
	for _, field := range fields {
		appendPart(field.Name)
		appendPart(field.Value)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

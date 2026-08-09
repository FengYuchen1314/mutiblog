package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// ReadSettings returns the effective configuration for an authenticated
// settings view. It applies the same local-file, environment, and
// interpolation rules as Load, then replaces secrets before returning data.
func ReadSettings(root, file string) (map[string]any, []string, error) {
	path, err := FilePath(root, file)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return nil, nil, err
	}
	local := strings.TrimSuffix(path, ".yaml") + ".local.yaml"
	if data, readErr := os.ReadFile(local); readErr == nil {
		override := map[string]any{}
		if err := yaml.Unmarshal(data, &override); err != nil {
			return nil, nil, err
		}
		values = merge(values, override)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, readErr
	}
	applyEnvironment(values)
	warnings := make([]string, 0)
	values = interpolateMap(values, &warnings).(map[string]any)
	return redactMap(values), warnings, nil
}

func redactMap(values map[string]any) map[string]any {
	masked := make(map[string]any, len(values))
	for key, value := range values {
		if secretKey(key) {
			if text, ok := value.(string); ok && text != "" {
				masked[key] = "********"
				continue
			}
		}
		switch nested := value.(type) {
		case map[string]any:
			masked[key] = redactMap(nested)
		case []any:
			items := make([]any, len(nested))
			for i, item := range nested {
				if m, ok := item.(map[string]any); ok {
					items[i] = redactMap(m)
				} else {
					items[i] = item
				}
			}
			masked[key] = items
		default:
			masked[key] = value
		}
	}
	return masked
}

func secretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "apikey") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "token")
}

// FilePath resolves the primary config path without loading it. It is shared
// by offline CLI operations and the authenticated settings API.
func FilePath(root, file string) (string, error) {
	if !filepath.IsAbs(root) {
		var err error
		root, err = filepath.Abs(root)
		if err != nil {
			return "", err
		}
	}
	if file == "" {
		return filepath.Join(root, "config", "config.yaml"), nil
	}
	if filepath.IsAbs(file) {
		return file, nil
	}
	return filepath.Join(root, file), nil
}

// UpdateSection merges a top-level JSON/YAML-compatible object into the main
// YAML file. Existing nodes are changed in place so untouched comments and
// ordering survive. The candidate is validated before the live file changes.
func UpdateSection(root, file, section string, values map[string]any) (*Config, error) {
	if section == "" {
		return nil, errors.New("configuration section is required")
	}
	path, err := FilePath(root, file)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	target, err := topLevelNode(&document, section)
	if err != nil {
		return nil, err
	}
	if target.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("configuration section %q is not an object", section)
	}
	patch, err := mapNode(values)
	if err != nil {
		return nil, err
	}
	mergeMapping(target, patch)
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return nil, err
	}
	candidate, err := os.CreateTemp(filepath.Dir(path), ".config-validate-*.yaml")
	if err != nil {
		return nil, err
	}
	candidatePath := candidate.Name()
	if err := candidate.Close(); err != nil {
		_ = os.Remove(candidatePath)
		return nil, err
	}
	defer os.Remove(candidatePath)
	if err := fsutil.AtomicWrite(candidatePath, encoded, 0o600); err != nil {
		return nil, err
	}
	validated, _, err := Load(root, candidatePath)
	if err != nil {
		return nil, fmt.Errorf("configuration value is invalid: %w", err)
	}
	if err := fsutil.AtomicWrite(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return validated, nil
}

// UpdateSchemaVersion changes the content-format marker while preserving the
// remainder of config.yaml (including comments). It validates the candidate
// before atomically replacing the live file, exactly like settings edits.
func UpdateSchemaVersion(root, file string, version int) (*Config, error) {
	if version < 1 {
		return nil, errors.New("schema version must be at least 1")
	}
	path, err := FilePath(root, file)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 ||
		document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("invalid YAML configuration document")
	}
	mapping := document.Content[0]
	var target *yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "schemaVersion" {
			target = mapping.Content[i+1]
			break
		}
	}
	if target == nil {
		mapping.Content = append(
			[]*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "schemaVersion"},
				{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(version)},
			},
			mapping.Content...)
	} else {
		target.Kind, target.Tag, target.Value, target.Content = yaml.ScalarNode, "!!int", fmt.Sprint(version), nil
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return nil, err
	}
	candidate, err := os.CreateTemp(filepath.Dir(path), ".config-validate-*.yaml")
	if err != nil {
		return nil, err
	}
	candidatePath := candidate.Name()
	if err := candidate.Close(); err != nil {
		_ = os.Remove(candidatePath)
		return nil, err
	}
	defer os.Remove(candidatePath)
	if err := fsutil.AtomicWrite(candidatePath, encoded, 0o600); err != nil {
		return nil, err
	}
	validated, _, err := Load(root, candidatePath)
	if err != nil {
		return nil, fmt.Errorf("configuration value is invalid: %w", err)
	}
	if err := fsutil.AtomicWrite(path, encoded, 0o600); err != nil {
		return nil, err
	}
	return validated, nil
}

func topLevelNode(document *yaml.Node, key string) (*yaml.Node, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 ||
		document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("invalid YAML configuration document")
	}
	mapping := document.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], nil
		}
	}
	return nil, fmt.Errorf("configuration section %q was not found", key)
}

func mapNode(values map[string]any) (*yaml.Node, error) {
	encoded, err := yaml.Marshal(values)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("configuration update must be an object")
	}
	return document.Content[0], nil
}

func mergeMapping(target, patch *yaml.Node) {
	for i := 0; i+1 < len(patch.Content); i += 2 {
		key, incoming := patch.Content[i], patch.Content[i+1]
		for j := 0; j+1 < len(target.Content); j += 2 {
			if target.Content[j].Value != key.Value {
				continue
			}
			if target.Content[j+1].Kind == yaml.MappingNode && incoming.Kind == yaml.MappingNode {
				mergeMapping(target.Content[j+1], incoming)
			} else {
				next := target.Content[j+1]
				head, line, foot := next.HeadComment, next.LineComment, next.FootComment
				*target.Content[j+1] = *incoming
				target.Content[j+1].HeadComment, target.Content[j+1].LineComment, target.Content[j+1].FootComment = head, line, foot
			}
			goto next
		}
		target.Content = append(target.Content, key, incoming)
	next:
	}
}

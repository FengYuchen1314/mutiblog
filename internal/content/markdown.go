package content

import (
	"bytes"
	"errors"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"gopkg.in/yaml.v3"
)

func encodeMarkdown(value domain.LocalizedMarkdown) ([]byte, error) {
	frontMatter := struct {
		Title          string `yaml:"title"`
		Summary        string `yaml:"summary,omitempty"`
		SEOTitle       string `yaml:"seoTitle,omitempty"`
		SEODescription string `yaml:"seoDescription,omitempty"`
	}{value.Title, value.Summary, value.SEOTitle, value.SEODescription}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	err := encoder.Encode(frontMatter)
	if err != nil {
		return nil, err
	}
	body := strings.TrimLeft(value.Markdown, "\r\n")
	return []byte("---\n" + buffer.String() + "---\n\n" + body), nil
}

func decodeMarkdown(data []byte) (domain.LocalizedMarkdown, error) {
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return domain.LocalizedMarkdown{}, errors.New("markdown front matter is missing")
	}
	remainder := data[4:]
	separator := bytes.Index(remainder, []byte("\n---\n"))
	if separator < 0 {
		return domain.LocalizedMarkdown{}, errors.New("markdown front matter is not closed")
	}
	var value domain.LocalizedMarkdown
	if err := yaml.Unmarshal(remainder[:separator], &value); err != nil {
		return domain.LocalizedMarkdown{}, err
	}
	value.Markdown = strings.TrimPrefix(string(remainder[separator+5:]), "\n")
	return value, nil
}

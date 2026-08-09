// Package ai keeps Markdown structure under program control while a provider
// translates only the natural-language portions of an article.
package ai

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type SegKind string

const (
	SegHeading    SegKind = "heading"
	SegParagraph  SegKind = "paragraph"
	SegListItem   SegKind = "listItem"
	SegTableCell  SegKind = "tableCell"
	SegBlockquote SegKind = "blockquote"
	SegImageAlt   SegKind = "imageAlt"
)

type Placeholder struct{ Token, Content string }

type Segment struct {
	Index        int
	Kind         SegKind
	Text         string
	Start, End   int
	Placeholders []Placeholder
	Context      string
}

type ExtractOpts struct{}

var (
	fenceRE       = regexp.MustCompile(`^\s*(` + "```" + `|~~~)`)
	headingRE     = regexp.MustCompile(`^(\s{0,3}#{1,6}\s+)(.*)$`)
	listRE        = regexp.MustCompile(`^(\s*(?:[-+*]|\d+[.)])\s+(?:\[[ xX]\]\s+)?)(.*)$`)
	quoteRE       = regexp.MustCompile(`^(\s*>\s?)(.*)$`)
	footnoteRE    = regexp.MustCompile(`^(\s*\[\^[^\]]+\]:\s*)(.*)$`)
	pureImageRE   = regexp.MustCompile(`^\s*!\[([^\]]*)\]\([^)]*\)\s*$`)
	codeSpanRE    = regexp.MustCompile("`[^`]+`")
	inlineMathRE  = regexp.MustCompile(`\$[^$\n]+\$`)
	autolinkRE    = regexp.MustCompile(`<(?:(?:https?://)[^ >]+|[\w.+-]+@[\w.-]+)>`)
	rawHTMLRE     = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	htmlEntityRE  = regexp.MustCompile(`&(?:#\d+|#x[0-9A-Fa-f]+|[A-Za-z]+);`)
	linkTargetRE  = regexp.MustCompile(`\]\((?:\\.|[^)])+\)`)
	urlRE         = regexp.MustCompile(`https?://[^\s<>()\[\]{}"'，。；：！？]+`)
	ipv4RE        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`)
	ipv6RE        = regexp.MustCompile(`\b[0-9a-fA-F:]{2,}:[0-9a-fA-F:]+\b`)
	unixPathRE    = regexp.MustCompile(`(?:^|\s)(?:/[\w.\-]+){2,}/?`)
	windowsPathRE = regexp.MustCompile(`\b[A-Za-z]:\\[\w\\.\-]+`)
	domainRE      = regexp.MustCompile(`\b[\w.\-]+\.(?:com|org|net|io|dev|cn|jp|de|me|sh|app|xyz)\b`)
	emailRE       = regexp.MustCompile(`\b[\w.+\-]+@[\w\-]+\.[\w.\-]+\b`)
	templateRE    = regexp.MustCompile(`\{\{[^}]+\}\}|\$\{[^}]+\}|:[a-z0-9_+\-]+:`)
)

// Extract returns non-overlapping byte ranges in the Markdown body. It is
// deliberately line-oriented: Markdown block prefixes are left outside every
// range, so applying a translated range cannot change nesting or syntax.
func Extract(md string, _ ExtractOpts) ([]Segment, error) {
	var out []Segment
	inFence, inMath, inFrontMatter := false, false, false
	frontOpening := false
	if strings.HasPrefix(md, "---\n") {
		inFrontMatter = true
		frontOpening = true
	}
	for offset, line := 0, md; ; {
		if len(line) == 0 {
			break
		}
		end := strings.IndexByte(line, '\n')
		chunk, next := line, ""
		if end >= 0 {
			chunk, next = line[:end], line[end+1:]
		}
		if inFrontMatter {
			if chunk == "---" && !frontOpening {
				inFrontMatter = false
			}
			frontOpening = false
			offset += len(chunk)
			if end >= 0 {
				offset++
			}
			line = next
			continue
		}
		trimmed := strings.TrimSpace(chunk)
		if fenceRE.MatchString(chunk) {
			inFence = !inFence
			offset += len(chunk)
			if end >= 0 {
				offset++
			}
			line = next
			continue
		}
		if trimmed == "$$" || strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") {
			if trimmed == "$$" {
				inMath = !inMath
			}
			offset += len(chunk)
			if end >= 0 {
				offset++
			}
			line = next
			continue
		}
		if !inFence && !inMath && trimmed != "" && !isTableSeparator(chunk) && trimmed != "---" && trimmed != "***" {
			switch {
			case isTableRow(chunk):
				out = append(out, tableCells(chunk, offset)...)
			case pureImageRE.MatchString(chunk):
				match := pureImageRE.FindStringSubmatchIndex(chunk)
				out = append(out, makeSegment(md, offset+match[2], offset+match[3], SegImageAlt))
			case headingRE.MatchString(chunk):
				m := headingRE.FindStringSubmatchIndex(chunk)
				out = append(out, makeSegment(md, offset+m[4], offset+m[5], SegHeading))
			case listRE.MatchString(chunk):
				m := listRE.FindStringSubmatchIndex(chunk)
				out = append(out, makeSegment(md, offset+m[4], offset+m[5], SegListItem))
			case quoteRE.MatchString(chunk):
				m := quoteRE.FindStringSubmatchIndex(chunk)
				out = append(out, makeSegment(md, offset+m[4], offset+m[5], SegBlockquote))
			case footnoteRE.MatchString(chunk):
				m := footnoteRE.FindStringSubmatchIndex(chunk)
				out = append(out, makeSegment(md, offset+m[4], offset+m[5], SegParagraph))
			default:
				start, stop := trimRange(chunk)
				if start < stop {
					out = append(out, makeSegment(md, offset+start, offset+stop, SegParagraph))
				}
			}
		}
		offset += len(chunk)
		if end >= 0 {
			offset++
		}
		line = next
	}
	for i := range out {
		out[i].Index = i
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	for i := range out {
		out[i].Index = i
		if out[i].Start < 0 || out[i].End > len(md) || out[i].Start >= out[i].End || (i > 0 && out[i-1].End > out[i].Start) {
			return nil, fmt.Errorf("invalid or overlapping translation segment at %d", i)
		}
	}
	return out, nil
}

func makeSegment(md string, start, end int, kind SegKind) Segment {
	text, placeholders := protectInline(md[start:end])
	return Segment{Kind: kind, Text: text, Start: start, End: end, Placeholders: placeholders}
}

func trimRange(s string) (int, int) {
	start := len(s) - len(strings.TrimLeft(s, " \t"))
	end := len(strings.TrimRight(s, " \t"))
	return start, end
}
func isTableRow(s string) bool {
	return strings.Count(s, "|") >= 2 && strings.Contains(strings.TrimSpace(s), "|")
}
func isTableSeparator(s string) bool {
	if !isTableRow(s) {
		return false
	}
	for _, r := range s {
		if r != '|' && r != '-' && r != ':' && r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}
func tableCells(line string, offset int) []Segment {
	var out []Segment
	start := 0
	for i := 0; i <= len(line); i++ {
		if i != len(line) && line[i] != '|' {
			continue
		}
		a, b := trimRange(line[start:i])
		if a < b {
			out = append(out, makeSegment(line, start+a, start+b, SegTableCell))
		}
		start = i + 1
	}
	for i := range out {
		out[i].Start += offset
		out[i].End += offset
	}
	return out
}

func protectInline(s string) (string, []Placeholder) {
	var placeholders []Placeholder
	protect := func(re *regexp.Regexp, input string) string {
		return re.ReplaceAllStringFunc(input, func(match string) string {
			token := fmt.Sprintf("⟦P%d⟧", len(placeholders)+1)
			placeholders = append(placeholders, Placeholder{Token: token, Content: match})
			return token
		})
	}
	for _, re := range []*regexp.Regexp{codeSpanRE, inlineMathRE, autolinkRE, rawHTMLRE, htmlEntityRE} {
		s = protect(re, s)
	}
	// Keep link/image text available to the model while protecting its target.
	s = linkTargetRE.ReplaceAllStringFunc(s, func(match string) string {
		content := match[2 : len(match)-1]
		token := fmt.Sprintf("⟦P%d⟧", len(placeholders)+1)
		placeholders = append(placeholders, Placeholder{Token: token, Content: content})
		return "](" + token + ")"
	})
	for _, re := range []*regexp.Regexp{urlRE, ipv4RE, ipv6RE, unixPathRE, windowsPathRE, domainRE, emailRE, templateRE} {
		s = protect(re, s)
	}
	return s, placeholders
}

func Batch(segs []Segment, budgetChars int) [][]Segment {
	if budgetChars <= 0 {
		budgetChars = 2500
	}
	var batches [][]Segment
	var current []Segment
	used := 0
	for _, segment := range segs {
		size := utf8.RuneCountInString(segment.Text)
		if len(current) > 0 && used+size > budgetChars {
			batches, current, used = append(batches, current), nil, 0
		}
		current, used = append(current, segment), used+size
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

func Apply(md string, segs []Segment, translated []string) (string, error) {
	if len(segs) != len(translated) {
		return "", fmt.Errorf("segment count mismatch")
	}
	var out strings.Builder
	last := 0
	for i, segment := range segs {
		if segment.Start < last || segment.End > len(md) {
			return "", fmt.Errorf("invalid segment range")
		}
		out.WriteString(md[last:segment.Start])
		value := translated[i]
		for _, placeholder := range segment.Placeholders {
			value = strings.ReplaceAll(value, placeholder.Token, placeholder.Content)
		}
		out.WriteString(value)
		last = segment.End
	}
	out.WriteString(md[last:])
	return out.String(), nil
}

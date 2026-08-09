// Package media implements the local, file-backed media storage driver.
package media

import (
	"context"
	"errors"
	"fmt"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Entry struct {
	Path, Name, MIME string
	Size             int64
	IsDir            bool
	ModTime          time.Time
}
type Storage interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	Move(context.Context, string, string) error
	List(context.Context, string) ([]Entry, error)
	Stat(context.Context, string) (Entry, error)
	PublicURL(string) string
	LocalPath(string) string
}
type LocalStorage struct{ root, prefix string }

func NewLocal(root, prefix string) *LocalStorage {
	return &LocalStorage{root: root, prefix: strings.TrimRight(prefix, "/")}
}
func (s *LocalStorage) resolve(path string) (string, error) { return fsutil.SafeJoin(s.root, path) }
func (s *LocalStorage) Put(_ context.Context, path string, r io.Reader, size int64, kind string) error {
	target, err := s.resolve(path)
	if err != nil {
		return err
	}
	if size < 0 {
		return errors.New("unknown upload size")
	}
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > size {
		return errors.New("upload exceeds declared size")
	}
	if kind == "image/svg+xml" {
		data = sanitizeSVG(data)
	}
	return fsutil.AtomicWrite(target, data, 0o644)
}
func (s *LocalStorage) Get(_ context.Context, path string) (io.ReadCloser, error) {
	target, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}
func (s *LocalStorage) Delete(_ context.Context, path string) error {
	target, err := s.resolve(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}
func (s *LocalStorage) Move(_ context.Context, src, dst string) error {
	from, err := s.resolve(src)
	if err != nil {
		return err
	}
	to, err := s.resolve(dst)
	if err != nil {
		return err
	}
	if err := fsutil.EnsureDir(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}
func (s *LocalStorage) List(_ context.Context, dir string) ([]Entry, error) {
	path, err := s.resolve(dir)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".") {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(dir, item.Name()))
		out = append(
			out,
			Entry{
				Path:    rel,
				Name:    item.Name(),
				MIME:    mime.TypeByExtension(filepath.Ext(item.Name())),
				Size:    info.Size(),
				IsDir:   item.IsDir(),
				ModTime: info.ModTime(),
			},
		)
	}
	return out, nil
}
func (s *LocalStorage) Stat(_ context.Context, path string) (Entry, error) {
	target, err := s.resolve(path)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Path:    path,
		Name:    filepath.Base(path),
		MIME:    mime.TypeByExtension(filepath.Ext(path)),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}
func (s *LocalStorage) PublicURL(path string) string {
	return s.prefix + "/" + strings.TrimLeft(filepath.ToSlash(path), "/")
}
func (s *LocalStorage) LocalPath(path string) string {
	target, err := s.resolve(path)
	if err != nil {
		return ""
	}
	return target
}
func SanitizeName(value string) string {
	ext := strings.ToLower(filepath.Ext(value))
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(value)), filepath.Ext(value))
	var b strings.Builder
	dash := false
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "upload"
	}
	return out + ext
}
func ParseByteSize(value string) (int64, error) {
	text := strings.ToUpper(strings.TrimSpace(value))
	if text == "" {
		return 0, errors.New("size is required")
	}
	multipliers := []struct {
		suffix string
		value  int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}}
	for _, unit := range multipliers {
		if !strings.HasSuffix(text, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
		parsed, err := strconv.ParseInt(number, 10, 64)
		if err != nil || parsed < 1 || parsed > (1<<62)/unit.value {
			return 0, fmt.Errorf("invalid size %q", value)
		}
		return parsed * unit.value, nil
	}
	return 0, fmt.Errorf("invalid size %q", value)
}
func ValidateImage(header []byte, name string, allowed []string) (string, error) {
	detected := http.DetectContentType(header)
	ext := strings.ToLower(filepath.Ext(name))
	byExt := mime.TypeByExtension(ext)
	if byExt == "" {
		return "", fmt.Errorf("unsupported extension %s", ext)
	}
	allowedByExt := false
	allowedDetected := false
	for _, kind := range allowed {
		allowedByExt = allowedByExt || byExt == kind
		allowedDetected = allowedDetected || detected == kind
	}
	if !allowedByExt {
		return "", fmt.Errorf("unsupported extension %s", ext)
	}
	// The standard sniffer identifies many valid SVG files as text/xml. The
	// extension is accepted only for SVG and the payload is subsequently
	// sanitised before it reaches the public media directory.
	if ext == ".svg" && byExt == "image/svg+xml" &&
		(detected == "image/svg+xml" || strings.HasPrefix(detected, "text/")) {
		return "image/svg+xml", nil
	}
	if !allowedDetected || detected != byExt || !strings.HasPrefix(detected, "image/") {
		return "", fmt.Errorf("only images are accepted")
	}
	return detected, nil
}

var (
	svgDangerousElement = regexp.MustCompile(
		`(?is)<\s*script\b[^>]*>.*?<\s*/\s*script\s*>|<\s*foreignObject\b[^>]*>.*?<\s*/\s*foreignObject\s*>`,
	)
	svgEventAttribute = regexp.MustCompile(`(?is)\s+on[a-z0-9_-]+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	svgDangerousURL   = regexp.MustCompile(
		`(?is)\s+(?:href|xlink:href)\s*=\s*(?:` +
			`"\s*(?:javascript:|data:text/html)[^"]*"|` +
			`'\s*(?:javascript:|data:text/html)[^']*'|` +
			`(?:javascript:|data:text/html)[^\s>]+)`,
	)
)

func sanitizeSVG(data []byte) []byte {
	value := string(data)
	value = svgDangerousElement.ReplaceAllString(value, "")
	value = svgEventAttribute.ReplaceAllString(value, "")
	value = svgDangerousURL.ReplaceAllString(value, "")
	return []byte(value)
}

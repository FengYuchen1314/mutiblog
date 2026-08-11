package dictionary

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

var defaults = map[string]map[string]string{
	"en":    {"home": "Home", "archives": "Archives", "links": "Links", "search": "Search", "colorScheme": "Color scheme", "about": "About", "languages": "Languages", "poweredBy": "Powered by MutiBlog", "comments": "Comments", "loadingComments": "Loading comments…", "commentsUnavailable": "Comments are temporarily unavailable.", "commentsClosed": "Commenting is closed; existing comments remain visible.", "commentName": "Name", "commentEmail": "Email (optional)", "commentWebsite": "Website (optional)", "commentContent": "Comment", "commentSubmit": "Submit", "commentPending": "Your comment is awaiting moderation.", "commentEmpty": "No comments yet.", "notFound": "Not found", "redirecting": "Continue to the requested page"},
	"zh-CN": {"home": "首页", "archives": "归档", "links": "友链", "search": "搜索", "colorScheme": "明暗主题", "about": "关于", "languages": "语言", "poweredBy": "由 MutiBlog 驱动", "comments": "评论", "loadingComments": "正在加载评论…", "commentsUnavailable": "评论服务暂时不可用。", "commentsClosed": "评论已关闭，已有评论仍可查看。", "commentName": "姓名", "commentEmail": "邮箱（可选）", "commentWebsite": "网站（可选）", "commentContent": "评论内容", "commentSubmit": "提交评论", "commentPending": "评论已提交，正在等待审核。", "commentEmpty": "还没有评论。", "notFound": "页面不存在", "redirecting": "继续前往请求的页面"},
}

func RequiredKeys() []string {
	keys := make([]string, 0, len(defaults["en"]))
	for key := range defaults["en"] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func Write(repository *fsrepo.Repository, rawLocale string, values map[string]string) error {
	tag, err := language.Parse(strings.TrimSpace(rawLocale))
	if err != nil || strings.TrimSpace(rawLocale) == "" {
		return errors.New("invalid framework dictionary locale")
	}
	cleaned := make(map[string]string, len(values))
	for _, key := range RequiredKeys() {
		if value := strings.TrimSpace(values[key]); value != "" {
			cleaned[key] = value
		}
	}
	if err := repository.WriteYAML(filepath.Join("content", "dictionaries", tag.String()+".yaml"), cleaned, false); err != nil {
		return err
	}
	// Built-in dictionaries are always a final non-empty safety net. Existing
	// entries remain deliberate overrides; only omitted built-in keys return to
	// their shipped value.
	return Ensure(repository)
}

func Ensure(repository *fsrepo.Repository) error {
	for locale, values := range defaults {
		path := filepath.Join("content", "dictionaries", locale+".yaml")
		exists, err := repository.Exists(path)
		if err != nil {
			return err
		}
		if !exists {
			if err := repository.WriteYAML(path, values, false); err != nil {
				return err
			}
			continue
		}
		var current map[string]string
		if err := repository.ReadYAML(path, &current); err != nil {
			return err
		}
		changed := false
		if current == nil {
			current = make(map[string]string, len(values))
		}
		for key, value := range values {
			if _, present := current[key]; !present {
				current[key] = value
				changed = true
			}
		}
		if changed {
			if err := repository.WriteYAML(path, current, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func Read(repository *fsrepo.Repository) (map[string]map[string]string, error) {
	entries, err := repository.ReadDir(filepath.Join("content", "dictionaries"))
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		tag, err := language.Parse(strings.TrimSuffix(entry.Name(), ".yaml"))
		expectedLocale := strings.TrimSuffix(entry.Name(), ".yaml")
		if err != nil || tag.String() != expectedLocale {
			return nil, fmt.Errorf("framework dictionary %q has a non-canonical locale filename", entry.Name())
		}
		var values map[string]string
		if err := repository.ReadYAML(filepath.Join("content", "dictionaries", entry.Name()), &values); err != nil {
			return nil, err
		}
		result[expectedLocale] = values
	}
	return result, nil
}

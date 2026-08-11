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
	"en": {
		"home":                  "Home",
		"archives":              "Archives",
		"links":                 "Links",
		"search":                "Search",
		"menu":                  "Menu",
		"colorScheme":           "Color scheme",
		"about":                 "About",
		"languages":             "Languages",
		"poweredBy":             "Powered by MutiBlog",
		"posts":                 "Posts",
		"categories":            "Categories",
		"tags":                  "Tags",
		"visits":                "Visits",
		"popularPosts":          "Popular posts",
		"recentPosts":           "Recent posts",
		"all":                   "All",
		"noPosts":               "No posts",
		"morePosts":             "More posts",
		"undated":               "Undated",
		"pagination":            "Pagination",
		"page":                  "Page",
		"previousPage":          "Previous",
		"nextPage":              "Next",
		"tableOfContents":       "Table of contents",
		"postNavigation":        "Post navigation",
		"previousPost":          "Previous post",
		"nextPost":              "Next post",
		"share":                 "Share",
		"nativeShare":           "Share",
		"wechatCopyLink":        "WeChat (copy link)",
		"copyLink":              "Copy link",
		"copied":                "Copied",
		"close":                 "Close",
		"wechatScan":            "Scan with WeChat",
		"shareUnavailable":      "The QR code is temporarily unavailable. Copy the link instead.",
		"upvote":                "Upvote",
		"upvoted":               "Upvoted",
		"upvoteCount":           "Upvote count",
		"upvotesUnavailable":    "Upvotes are temporarily unavailable.",
		"scrollTop":             "Scroll to top",
		"statisticsUnavailable": "Statistics are temporarily unavailable.",
		"comments":              "Comments",
		"loadingComments":       "Loading comments…",
		"commentsUnavailable":   "Comments are temporarily unavailable.",
		"commentsClosed":        "Commenting is closed; existing comments remain visible.",
		"commentsMore":          "Load more comments",
		"commentName":           "Name",
		"commentEmail":          "Email (optional)",
		"commentWebsite":        "Website (optional)",
		"commentContent":        "Comment",
		"commentSubmit":         "Submit",
		"commentPending":        "Your comment is awaiting moderation.",
		"commentEmpty":          "No comments yet.",
		"notFound":              "Not found",
		"redirecting":           "Continue to the requested page",
	},
	"zh-CN": {
		"home":                  "首页",
		"archives":              "归档",
		"links":                 "友链",
		"search":                "搜索",
		"menu":                  "菜单",
		"colorScheme":           "明暗主题",
		"about":                 "关于",
		"languages":             "语言",
		"poweredBy":             "由 MutiBlog 驱动",
		"posts":                 "文章",
		"categories":            "分类",
		"tags":                  "标签",
		"visits":                "访问",
		"popularPosts":          "热门文章",
		"recentPosts":           "最新文章",
		"all":                   "全部",
		"noPosts":               "暂无文章",
		"morePosts":             "更多文章",
		"undated":               "未注明日期",
		"pagination":            "分页",
		"page":                  "页码",
		"previousPage":          "上一页",
		"nextPage":              "下一页",
		"tableOfContents":       "目录",
		"postNavigation":        "文章导航",
		"previousPost":          "上一篇",
		"nextPost":              "下一篇",
		"share":                 "分享",
		"nativeShare":           "系统分享",
		"wechatCopyLink":        "微信（复制链接）",
		"copyLink":              "复制链接",
		"copied":                "已复制",
		"close":                 "关闭",
		"wechatScan":            "使用微信扫码",
		"shareUnavailable":      "二维码暂时不可用，请复制链接分享。",
		"upvote":                "点赞",
		"upvoted":               "已点赞",
		"upvoteCount":           "点赞数",
		"upvotesUnavailable":    "点赞暂时不可用。",
		"scrollTop":             "返回顶部",
		"statisticsUnavailable": "统计数据暂时不可用。",
		"comments":              "评论",
		"loadingComments":       "正在加载评论…",
		"commentsUnavailable":   "评论服务暂时不可用。",
		"commentsClosed":        "评论已关闭，已有评论仍可查看。",
		"commentsMore":          "加载更多评论",
		"commentName":           "姓名",
		"commentEmail":          "邮箱（可选）",
		"commentWebsite":        "网站（可选）",
		"commentContent":        "评论内容",
		"commentSubmit":         "提交评论",
		"commentPending":        "评论已提交，正在等待审核。",
		"commentEmpty":          "还没有评论。",
		"notFound":              "页面不存在",
		"redirecting":           "继续前往请求的页面",
	},
}

func RequiredKeys() []string {
	keys := make([]string, 0, len(defaults["zh-CN"]))
	for key := range defaults["zh-CN"] {
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

// Package sourcedomain 提供 URL→域名提取与域名分类的公共工具。
//
// 独立成包的原因：brand（审计引擎）、topsource（Top Source 归因）、
// sourcestudy（引擎来源偏好研究）三处都需要按域名聚合引用来源，
// 放公共包避免 brand ↔ topsource/sourcestudy 之间的循环依赖。
package sourcedomain

import (
	"net/url"
	"strings"
)

// domainCategories 已知域名 → 类别映射。
// 匹配时同时命中域名本身与其子域名（如 blog.g2.com → review_site）。
var domainCategories = map[string]string{
	// 评测站点
	"g2.com":             "review_site",
	"capterra.com":       "review_site",
	"gartner.com":        "review_site",
	"trustpilot.com":     "review_site",
	"softwareadvice.com": "review_site",
	"getapp.com":         "review_site",
	// 技术文档 / 代码托管
	"github.com":        "docs",
	"gitlab.com":        "docs",
	"readthedocs.io":    "docs",
	"stackoverflow.com": "docs",
	// 社交 / 问答社区
	"reddit.com":  "social",
	"twitter.com": "social",
	"x.com":       "social",
	"weibo.com":   "social",
	"zhihu.com":   "social",
	// 新闻媒体
	"techcrunch.com": "news",
	"theverge.com":   "news",
	"36kr.com":       "news",
	"pingwest.com":   "news",
	// 博客 / 内容平台
	"medium.com": "blog",
	"dev.to":     "blog",
	"juejin.cn":  "blog",
	// 视频平台
	"youtube.com":  "video",
	"bilibili.com": "video",
}

// CategorizeDomain 根据域名启发式归类。
//
// 类别：review_site / docs / social / news / blog / video / other。
// 子域名继承主域名类别（例如 reviews.g2.com → review_site）。
// 未命中任何已知域名时返回 "other"。
func CategorizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "other"
	}
	// 精确匹配
	if cat, ok := domainCategories[domain]; ok {
		return cat
	}
	// 子域名匹配：逐级去掉最左侧标签，避免短后缀误命中
	// 例如 "blog.g2.com" → "g2.com" → review_site
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if cat, ok := domainCategories[suffix]; ok {
			return cat
		}
	}
	return "other"
}

// ExtractDomain 从 URL 中提取规范化的域名。
//
// 处理逻辑：
//   - 空字符串或解析失败返回 ""
//   - 无 scheme 时补 https:// 让 url.Parse 正确解析 host
//   - 去掉端口、转为小写、去掉 "www." 前缀
func ExtractDomain(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	// 无 scheme 时补一个，让 url.Parse 能正确解析出 Host
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	// 去掉端口
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	// 去掉 www. 前缀
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return ""
	}
	return host
}

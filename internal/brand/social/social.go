// Package social 提供社媒情感监控能力，作为 AI 搜索引擎可见度之外的补充维度。
//
// 监控品牌在社交媒体平台上的提及量与情感倾向，回答"品牌在社媒上口碑如何"这一
// 与 AI 可见度互补的问题：AI 可见度衡量"被 AI 引用"，社媒情感衡量"被人讨论"。
//
// 设计采用**可插拔适配器**架构（参考 readiness 包风格）：
//   - 定义统一的 PlatformAdapter 接口
//   - 内置 Reddit / 微博 / YouTube 三个免鉴权适配器（公开 JSON / RSS 端点）
//   - Twitter / 小红书 预留适配器接口，默认返回"需配置 API Key"
//
// 仅依赖 net/http / encoding/json / encoding/xml 标准库，请求超时 15 秒/平台。
// 情感分析用规则引擎（正面词库 + 负面词库），不引入 NLP 库。
package social

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Mention 单条社媒提及。
type Mention struct {
	// 平台标识：reddit / weibo / youtube / twitter / xiaohongshu
	Platform string `json:"platform"`
	// 帖子/视频/微博标题。
	Title string `json:"title"`
	// 正文内容（已清洗 HTML）。
	Content string `json:"content"`
	// 作者昵称。
	Author string `json:"author"`
	// 原始链接。
	URL string `json:"url"`
	// 发布时间。
	PublishedAt time.Time `json:"published_at"`
	// 情感倾向：positive / neutral / negative。
	Sentiment string `json:"sentiment"`
	// 点赞数（如可获取）。
	Likes int `json:"likes,omitempty"`
}

// PlatformStat 单平台统计。
type PlatformStat struct {
	Platform string `json:"platform"`
	Count    int    `json:"count"`
	Positive int    `json:"positive"`
	Negative int    `json:"negative"`
}

// MonitorResult 一次社媒监控的完整结果。
type MonitorResult struct {
	BrandName      string         `json:"brand_name"`
	TotalMentions  int            `json:"total_mentions"`
	Positive       int            `json:"positive"`
	Neutral        int            `json:"neutral"`
	Negative       int            `json:"negative"`
	SentimentScore float64        `json:"sentiment_score"` // -100 ~ 100
	Mentions       []Mention      `json:"mentions"`
	Platforms      []PlatformStat `json:"platforms"`
	// 各平台错误信息（便于前端展示降级原因）。
	Errors map[string]string `json:"errors,omitempty"`
}

// PlatformAdapter 社媒平台适配器接口。
//
// 实现方负责：构造请求、解析响应、清洗文本，返回统一的 Mention 列表。
// 情感分析由调用方（Monitor 函数）统一执行，适配器无需关注。
type PlatformAdapter interface {
	// Name 返回平台标识（reddit/weibo/youtube/twitter/xiaohongshu）。
	Name() string
	// Search 在该平台搜索 query，返回最多 limit 条提及。
	Search(ctx context.Context, query string, limit int) ([]Mention, error)
}

// perPlatformTimeout 单平台请求超时上限。
const perPlatformTimeout = 15 * time.Second

// sharedHTTPClient 跨适配器共享的 HTTP 客户端（15 秒超时）。
var sharedHTTPClient = &http.Client{Timeout: perPlatformTimeout}

// userAgent 统一 User-Agent（Reddit 强制要求，其他平台也建议设置）。
const userAgent = "geo-social-monitor/1.0 (+https://github.com/my-geo)"

// ---------- 核心调度 ----------

// Monitor 在多个社媒平台并行监控品牌提及与情感。
//
// platforms 指定要查询的平台标识列表（reddit/weibo/youtube/twitter/xiaohongshu），
// limit 为每个平台的提及上限。各平台并行执行，互不阻塞。
// 单平台失败不影响其他平台，错误信息写入 result.Errors。
func Monitor(ctx context.Context, brandName string, platforms []string, limit int) (*MonitorResult, error) {
	if strings.TrimSpace(brandName) == "" {
		return nil, fmt.Errorf("social: brand_name 不能为空")
	}
	if len(platforms) == 0 {
		return nil, fmt.Errorf("social: platforms 不能为空")
	}
	if limit <= 0 {
		limit = 20
	}

	adapters := make([]PlatformAdapter, 0, len(platforms))
	for _, p := range platforms {
		ad := adapterFor(p)
		if ad == nil {
			continue
		}
		adapters = append(adapters, ad)
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("social: 没有可用的平台适配器（platforms=%v）", platforms)
	}

	// 各平台并行查询
	type platformResult struct {
		name     string
		mentions []Mention
		err      error
	}
	results := make([]platformResult, len(adapters))
	var wg sync.WaitGroup
	for i, ad := range adapters {
		wg.Add(1)
		go func(idx int, adapter PlatformAdapter) {
			defer wg.Done()
			// 每个平台独立设置超时，避免一个慢平台拖累整体
			pctx, cancel := context.WithTimeout(ctx, perPlatformTimeout)
			defer cancel()
			mentions, err := adapter.Search(pctx, brandName, limit)
			results[idx] = platformResult{
				name:     adapter.Name(),
				mentions: mentions,
				err:      err,
			}
		}(i, ad)
	}
	wg.Wait()

	// 聚合
	res := &MonitorResult{
		BrandName: brandName,
		Errors:    map[string]string{},
	}
	for _, pr := range results {
		if pr.err != nil {
			res.Errors[pr.name] = pr.err.Error()
			continue
		}
		// 对每条提及执行情感分析
		for i := range pr.mentions {
			pr.mentions[i].Sentiment = analyzeSentiment(pr.mentions[i].Title + " " + pr.mentions[i].Content)
		}
		stat := PlatformStat{Platform: pr.name, Count: len(pr.mentions)}
		for _, m := range pr.mentions {
			switch m.Sentiment {
			case "positive":
				stat.Positive++
			case "negative":
				stat.Negative++
			}
		}
		res.Platforms = append(res.Platforms, stat)
		res.Mentions = append(res.Mentions, pr.mentions...)
		res.TotalMentions += len(pr.mentions)
	}

	// 全局情感统计
	for _, m := range res.Mentions {
		switch m.Sentiment {
		case "positive":
			res.Positive++
		case "neutral":
			res.Neutral++
		case "negative":
			res.Negative++
		}
	}
	// 情感评分：(-100 ~ 100) = (正面数 - 负面数) / 总提及数 * 100
	if res.TotalMentions > 0 {
		res.SentimentScore = float64(res.Positive-res.Negative) / float64(res.TotalMentions) * 100
	}
	// 保留两位小数（避免浮点尾数）
	res.SentimentScore = float64(int(res.SentimentScore*100)) / 100
	return res, nil
}

// adapterFor 按平台标识返回对应适配器实例（未识别返回 nil）。
func adapterFor(platform string) PlatformAdapter {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "reddit":
		return &RedditAdapter{}
	case "weibo":
		return &WeiboAdapter{}
	case "youtube":
		return &YouTubeAdapter{}
	case "twitter", "x":
		return &TwitterAdapter{}
	case "xiaohongshu", "xhs":
		return &XiaohongshuAdapter{}
	}
	return nil
}

// ---------- 情感分析（规则引擎） ----------

// positiveWords 正面词库（中英文常见表达）。
var positiveWords = []string{
	// 中文
	"好", "棒", "优秀", "推荐", "喜欢", "牛", "赞", "不错", "厉害", "支持",
	"期待", "真香", "爱了", "惊艳", "领先", "最佳", "最好", "首选", "知名",
	"值得信赖", "强大", "创新", "受欢迎", "杰出", "权威", "出色", "靠谱",
	"实用", "良心", "宝藏", "宝藏品牌", "良心品牌", "回购", "种草", "安利",
	// 英文
	"best", "top", "leading", "recommend", "recommended", "excellent", "great",
	"popular", "trusted", "reliable", "powerful", "innovative", "favorite",
	"prefer", "preferred", "standout", "notable", "award", "leader", "love",
	"amazing", "awesome", "fantastic", "wonderful",
}

// negativeWords 负面词库（中英文常见表达）。
var negativeWords = []string{
	// 中文
	"差", "垃圾", "坑", "骗", "烂", "失望", "退", "投诉", "崩溃", "恶心",
	"愤怒", "后悔", "糟糕", "避免", "问题", "过时", "昂贵", "局限", "负面",
	"避雷", "踩雷", "劝退", "吐槽", "黑心", "虚假", "跑路", "暴雷", "翻车",
	"漏水", "卡顿", "bug", "BUG", "Bug", "假货", "山寨", "劣质",
	// 英文
	"worst", "avoid", "poor", "bad", "expensive", "limited", "outdated",
	"buggy", "slow", "complaint", "issue", "problem", "controversy",
	"scam", "fraud", "terrible", "horrible", "awful", "disappointed",
}

// analyzeSentiment 用规则引擎对文本做情感分析。
//
// 简单计分：正面词数 - 负面词数 > 0 = positive, < 0 = negative, = 0 = neutral。
// 不引入 NLP 库，零依赖、零延迟，适合社媒监控这种粗粒度场景。
func analyzeSentiment(text string) string {
	if strings.TrimSpace(text) == "" {
		return "neutral"
	}
	lower := strings.ToLower(text)
	posScore, negScore := 0, 0
	for _, w := range positiveWords {
		// 英文词做小写匹配；中文词本身就是小写无关
		posScore += strings.Count(lower, strings.ToLower(w))
	}
	for _, w := range negativeWords {
		negScore += strings.Count(lower, strings.ToLower(w))
	}
	switch {
	case posScore > negScore:
		return "positive"
	case negScore > posScore:
		return "negative"
	default:
		return "neutral"
	}
}

// ---------- 工具函数 ----------

// fetchText 发起 GET 请求并返回响应体文本与状态码（限制 2MB）。
func fetchText(ctx context.Context, target string) (body string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 限制 2MB
	return string(b), resp.StatusCode, err
}

// stripHTMLTags 简单去除 HTML 标签（<...>），保留纯文本。
// 微博正文里嵌套大量 <a>/<span>/<br> 等标签，需要清洗后再做情感分析。
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	// 折叠多余空白
	out := strings.Join(strings.Fields(b.String()), " ")
	return out
}

// ---------- Reddit 适配器 ----------

// RedditAdapter 通过 Reddit 公开 JSON API 搜索提及（免鉴权）。
//
// 端点：https://www.reddit.com/search.json?q={query}&sort=new&limit={limit}
// 响应结构：data.children[].data.{title,author,created,score,selftext,permalink,url}
type RedditAdapter struct{}

// Name 返回平台标识。
func (a *RedditAdapter) Name() string { return "reddit" }

// Search 在 Reddit 搜索品牌提及。
func (a *RedditAdapter) Search(ctx context.Context, query string, limit int) ([]Mention, error) {
	if limit <= 0 {
		limit = 20
	}
	target := fmt.Sprintf("https://www.reddit.com/search.json?q=%s&sort=new&limit=%d",
		url.QueryEscape(query), limit)
	body, status, err := fetchText(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("reddit: 请求失败: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("reddit: HTTP %d", status)
	}

	// 嵌套结构：{data:{children:[{data:{...}}]}}
	var resp struct {
		Data struct {
			Children []struct {
				Data struct {
					Title     string  `json:"title"`
					Author    string  `json:"author"`
					Created   float64 `json:"created_utc"`
					Score     int     `json:"score"`
					Selftext  string  `json:"selftext"`
					Permalink string  `json:"permalink"`
					URL       string  `json:"url"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("reddit: JSON 解析失败: %w", err)
	}

	mentions := make([]Mention, 0, len(resp.Data.Children))
	for _, c := range resp.Data.Children {
		d := c.Data
		link := d.URL
		if link == "" && d.Permalink != "" {
			link = "https://www.reddit.com" + d.Permalink
		}
		// 截断过长正文（避免传输与渲染负担）
		content := d.Selftext
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		mentions = append(mentions, Mention{
			Platform:    a.Name(),
			Title:       d.Title,
			Content:     content,
			Author:      d.Author,
			URL:         link,
			PublishedAt: time.Unix(int64(d.Created), 0).UTC(),
			Likes:       d.Score,
		})
	}
	return mentions, nil
}

// ---------- 微博适配器 ----------

// WeiboAdapter 通过微博移动端公开 API 搜索提及（免鉴权）。
//
// 端点：https://m.weibo.cn/api/container/getIndex?containerid=100103type=1&q={query}
// 响应结构：data.cards[].mblog.{text,user.screen_name,created_at,attitudes_count,id,bid}
// 正文 text 含 HTML 标签，需 stripHTMLTags 清洗。
type WeiboAdapter struct{}

// Name 返回平台标识。
func (a *WeiboAdapter) Name() string { return "weibo" }

// Search 在微博搜索品牌提及。
func (a *WeiboAdapter) Search(ctx context.Context, query string, limit int) ([]Mention, error) {
	if limit <= 0 {
		limit = 20
	}
	// containerid=100103type=1 表示按关键词搜索微博
	target := fmt.Sprintf("https://m.weibo.cn/api/container/getIndex?containerid=100103type%%3D1%%26q%%3D%s",
		url.QueryEscape(query))
	body, status, err := fetchText(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("weibo: 请求失败: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("weibo: HTTP %d", status)
	}

	var resp struct {
		Ok   bool `json:"ok"`
		Data struct {
			Cards []struct {
				CardType int `json:"card_type"`
				Mblog    *struct {
					ID             int64  `json:"id"`
					Text           string `json:"text"`
					CreatedAt      string `json:"created_at"`
					AttitudesCount int    `json:"attitudes_count"`
					User           struct {
						ScreenName string `json:"screen_name"`
					} `json:"user"`
				} `json:"mblog"`
			} `json:"cards"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("weibo: JSON 解析失败: %w", err)
	}

	mentions := make([]Mention, 0, limit)
	for _, c := range resp.Data.Cards {
		// card_type=9 是微博正文卡片，其他类型（如 11=热门话题、17=分组）跳过
		if c.CardType != 9 || c.Mblog == nil {
			continue
		}
		mb := c.Mblog
		// 微博时间格式："Sun Jan 02 15:04:05 +0800 2006"
		pubAt := parseWeiboTime(mb.CreatedAt)
		content := stripHTMLTags(mb.Text)
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		mentions = append(mentions, Mention{
			Platform:    a.Name(),
			Title:       truncateTitle(content, 60),
			Content:     content,
			Author:      mb.User.ScreenName,
			URL:         fmt.Sprintf("https://m.weibo.cn/detail/%d", mb.ID),
			PublishedAt: pubAt,
			Likes:       mb.AttitudesCount,
		})
		if len(mentions) >= limit {
			break
		}
	}
	return mentions, nil
}

// parseWeiboTime 解析微博时间字符串（"Mon Jan 02 15:04:05 +0800 2006"）。
// 失败时返回零值，不阻塞流程。
func parseWeiboTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// truncateTitle 截取字符串前 n 字符作为标题。
func truncateTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// ---------- YouTube 适配器 ----------

// YouTubeAdapter 通过 YouTube 公开 RSS 搜索提及（免鉴权）。
//
// 端点：https://www.youtube.com/feeds/videos.xml?search_query={query}
// 响应结构：Atom feed，entry.{title,author.name,published,link[href],id(yt:video:xxx)}
type YouTubeAdapter struct{}

// Name 返回平台标识。
func (a *YouTubeAdapter) Name() string { return "youtube" }

// Search 在 YouTube 搜索品牌相关的最新视频。
func (a *YouTubeAdapter) Search(ctx context.Context, query string, limit int) ([]Mention, error) {
	if limit <= 0 {
		limit = 20
	}
	target := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?search_query=%s",
		url.QueryEscape(query))
	body, status, err := fetchText(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("youtube: 请求失败: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("youtube: HTTP %d", status)
	}

	// YouTube RSS 是标准 Atom feed，命名空间：
	//   默认 http://www.w3.org/2005/Atom
	//   yt    http://www.youtube.com/xml/schemas/2015
	type atomEntry struct {
		ID        string `xml:"id"` // 形如 "yt:video:VIDEO_ID"
		Title     string `xml:"title"`
		Published string `xml:"published"` // RFC3339
		Link      struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Author struct {
			Name string `xml:"name"`
		} `xml:"author"`
	}
	type atomFeed struct {
		XMLName xml.Name    `xml:"feed"`
		Entries []atomEntry `xml:"entry"`
	}

	var feed atomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		return nil, fmt.Errorf("youtube: XML 解析失败: %w", err)
	}

	mentions := make([]Mention, 0, limit)
	for _, e := range feed.Entries {
		if len(mentions) >= limit {
			break
		}
		// 解析发布时间（RFC3339）
		pubAt := time.Time{}
		if e.Published != "" {
			if t, err := time.Parse(time.RFC3339, e.Published); err == nil {
				pubAt = t
			}
		}
		// 视频链接：优先取 entry.link.href，否则从 id 提取 videoId 构造
		link := e.Link.Href
		if link == "" {
			if vid := extractYouTubeVideoID(e.ID); vid != "" {
				link = "https://www.youtube.com/watch?v=" + vid
			}
		}
		mentions = append(mentions, Mention{
			Platform:    a.Name(),
			Title:       e.Title,
			Content:     "", // RSS 不含视频描述，留空
			Author:      e.Author.Name,
			URL:         link,
			PublishedAt: pubAt,
			Likes:       0, // RSS 不含点赞数
		})
	}
	return mentions, nil
}

// extractYouTubeVideoID 从 "yt:video:VIDEO_ID" 格式中提取 videoId。
func extractYouTubeVideoID(id string) string {
	const prefix = "yt:video:"
	idx := strings.Index(id, prefix)
	if idx < 0 {
		return ""
	}
	return id[idx+len(prefix):]
}

// ---------- Twitter / 小红书 预留适配器 ----------

// TwitterAdapter Twitter 适配器（预留接口）。
//
// Twitter/X 官方 API 自 2023 年起强制付费鉴权，无免鉴权公开端点可用。
// 这里预留接口与错误提示，待配置 API Key 后可在此实现真实查询。
type TwitterAdapter struct{}

// Name 返回平台标识。
func (a *TwitterAdapter) Name() string { return "twitter" }

// Search 返回提示信息（需配置 API Key）。
func (a *TwitterAdapter) Search(ctx context.Context, query string, limit int) ([]Mention, error) {
	return nil, fmt.Errorf("twitter: 需配置 API Key（Twitter/X API 自 2023 起强制付费鉴权，未配置时不返回数据）")
}

// XiaohongshuAdapter 小红书适配器（预留接口）。
//
// 小红书无公开免鉴权搜索端点，需配置 App Key / Secret。
type XiaohongshuAdapter struct{}

// Name 返回平台标识。
func (a *XiaohongshuAdapter) Name() string { return "xiaohongshu" }

// Search 返回提示信息（需配置 API Key）。
func (a *XiaohongshuAdapter) Search(ctx context.Context, query string, limit int) ([]Mention, error) {
	return nil, fmt.Errorf("xiaohongshu: 需配置 API Key（小红书无公开免鉴权端点，未配置时不返回数据）")
}

// Package template 提供 20+ 平台的内容生成模板。
//
// 不同平台（生成式引擎、社交媒体、内容社区）对内容格式、风格、长度、
// 品牌植入方式有差异化要求。本包为每个平台预置内容模板，指导 GEO 内容生成：
//   - 写作风格（正式/活泼/技术/简洁）
//   - 内容结构建议（开头/主体/结尾的组织方式）
//   - 品牌植入策略（自然提及/对比植入/权威背书）
//   - 字数/长度建议
//   - 平台特有关键注意事项
//
// 覆盖平台（23 个）：
//   - 生成式引擎（14）：ChatGPT/Perplexity/Gemini/Claude/Grok/Qwen/GLM/DeepSeek/Kimi/Wenxin/Doubao/Xiaomi/Xunfei/Yuanbao
//   - 社交媒体（5）：Reddit/微博/Twitter/小红书/YouTube
//   - 内容社区（4）：知乎/CSDN/微信公众号/掘金
package template

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// PlatformType 平台类型。
type PlatformType string

const (
	PlatformTypeAIEngine PlatformType = "ai_engine" // 生成式引擎
	PlatformTypeSocial   PlatformType = "social"    // 社交媒体
	PlatformTypeContent  PlatformType = "content"   // 内容社区
)

// WritingStyle 写作风格。
type WritingStyle string

const (
	StyleFormal    WritingStyle = "formal"    // 正式严谨
	StyleLively    WritingStyle = "lively"    // 活泼亲切
	StyleTechnical WritingStyle = "technical" // 技术深度
	StyleConcise   WritingStyle = "concise"   // 简洁直接
	StyleNarrative WritingStyle = "narrative" // 叙事故事
)

// BrandPlacement 品牌植入策略。
type BrandPlacement string

const (
	PlacementNatural     BrandPlacement = "natural"      // 自然提及
	PlacementComparison  BrandPlacement = "comparison"   // 对比植入
	PlacementAuthority   BrandPlacement = "authority"    // 权威背书
	PlacementFirstPerson BrandPlacement = "first_person" // 第一人称体验
	PlacementEducational BrandPlacement = "educational"  // 教育式植入
)

// Template 平台内容模板。
type Template struct {
	Platform      string         `json:"platform"`       // 平台标识
	Name          string         `json:"name"`           // 平台中文名
	Type          PlatformType   `json:"type"`           // 平台类型
	Style         WritingStyle   `json:"style"`          // 推荐写作风格
	MinWords      int            `json:"min_words"`      // 建议最小字数
	MaxWords      int            `json:"max_words"`      // 建议最大字数
	Structure     []string       `json:"structure"`      // 内容结构建议（按段落）
	BrandStrategy BrandPlacement `json:"brand_strategy"` // 品牌植入策略
	Tips          []string       `json:"tips"`           // 平台特有注意事项
	PromptSuffix  string         `json:"prompt_suffix"`  // 生成内容时追加到 LLM prompt 的平台指令
}

// registry 平台模板注册表。
var registry = map[string]Template{}

func init() {
	// ===== 生成式引擎（14）=====
	registerAIEngines()
	// ===== 社交媒体（5）=====
	registerSocialPlatforms()
	// ===== 内容社区（4）=====
	registerContentPlatforms()
}

// registerAIEngines 注册生成式引擎模板。
func registerAIEngines() {
	engines := []struct {
		id    string
		name  string
		style WritingStyle
		place BrandPlacement
	}{
		{"chatgpt", "ChatGPT", StyleFormal, PlacementNatural},
		{"perplexity", "Perplexity", StyleFormal, PlacementAuthority},
		{"gemini", "Gemini", StyleTechnical, PlacementEducational},
		{"claude", "Claude", StyleNarrative, PlacementEducational},
		{"grok", "Grok", StyleLively, PlacementComparison},
		{"qwen", "通义千问", StyleFormal, PlacementNatural},
		{"glm", "智谱GLM", StyleTechnical, PlacementAuthority},
		{"deepseek", "DeepSeek", StyleTechnical, PlacementComparison},
		{"kimi", "Kimi", StyleConcise, PlacementNatural},
		{"wenxin", "文心一言", StyleFormal, PlacementEducational},
		{"doubao", "豆包", StyleLively, PlacementFirstPerson},
		{"xiaomi", "小米大模型", StyleConcise, PlacementNatural},
		{"xunfei", "讯飞星火", StyleFormal, PlacementEducational},
		{"yuanbao", "元宝/混元", StyleFormal, PlacementNatural},
	}
	for _, e := range engines {
		register(Template{
			Platform:      e.id,
			Name:          e.name,
			Type:          PlatformTypeAIEngine,
			Style:         e.style,
			MinWords:      800,
			MaxWords:      2500,
			Structure:     []string{"结论前置（首段直接给出核心观点）", "分点展开（每点配数据/案例支撑）", "引用权威来源标注", "结尾总结与行动建议"},
			BrandStrategy: e.place,
			Tips: []string{
				"AI 引擎偏好结构化、引用丰富的内容",
				"首段 100 字内必须出现品牌名与核心品类",
				"数据与统计数字能显著提升被引用概率",
			},
			PromptSuffix: fmt.Sprintf("请以%s的风格生成内容，结论前置，引用来源丰富，确保首段出现品牌名。", styleLabel(e.style)),
		})
	}
}

// registerSocialPlatforms 注册社交媒体模板。
func registerSocialPlatforms() {
	// Reddit
	register(Template{
		Platform: "reddit", Name: "Reddit", Type: PlatformTypeSocial, Style: StyleNarrative,
		MinWords: 300, MaxWords: 1000,
		Structure:     []string{"个人体验故事开头", "具体使用场景描述", "优缺点客观对比", "社区互动引导"},
		BrandStrategy: PlacementFirstPerson,
		Tips:          []string{"Reddit 用户反感硬广，必须以真实体验口吻", "标题用问题式或分享式", "积极参与评论区互动"},
		PromptSuffix:  "以 Reddit 用户第一人称分享真实体验，语气自然不做作，客观陈述优缺点。",
	})
	// 微博
	register(Template{
		Platform: "weibo", Name: "微博", Type: PlatformTypeSocial, Style: StyleLively,
		MinWords: 100, MaxWords: 500,
		Structure:     []string{"热点话题切入", "品牌自然植入", "互动话题/投票", "@相关账号扩散"},
		BrandStrategy: PlacementNatural,
		Tips:          []string{"140 字内点明核心", "配图/视频提升曝光", "带上热门话题标签"},
		PromptSuffix:  "微博风格，简洁活泼，140字内点明核心，带2-3个话题标签。",
	})
	// Twitter/X
	register(Template{
		Platform: "twitter", Name: "Twitter/X", Type: PlatformTypeSocial, Style: StyleConcise,
		MinWords: 50, MaxWords: 280,
		Structure:     []string{"观点/金句开头", "品牌自然提及", "数据/链接支撑", "互动引导"},
		BrandStrategy: PlacementNatural,
		Tips:          []string{"280字符限制", "线程(Thread)展开深度内容", "配图提升30%互动率"},
		PromptSuffix:  "Twitter/X 风格，280字符以内，观点鲜明，配数据支撑。",
	})
	// 小红书
	register(Template{
		Platform: "xiaohongshu", Name: "小红书", Type: PlatformTypeSocial, Style: StyleLively,
		MinWords: 200, MaxWords: 800,
		Structure:     []string{"吸睛标题+封面", "个人体验故事", "干货要点(分点)", "互动引导+标签"},
		BrandStrategy: PlacementFirstPerson,
		Tips:          []string{"标题决定点击率", "emoji 增加亲和力", "5-10个标签覆盖搜索", "首图必须精美"},
		PromptSuffix:  "小红书种草风格，第一人称体验分享，标题吸睛，正文分点+emoji，结尾带标签。",
	})
	// YouTube
	register(Template{
		Platform: "youtube", Name: "YouTube", Type: PlatformTypeSocial, Style: StyleNarrative,
		MinWords: 500, MaxWords: 2000,
		Structure:     []string{"视频脚本：开场hook", "主体内容分段", "品牌产品演示", "结尾CTA+订阅引导"},
		BrandStrategy: PlacementFirstPerson,
		Tips:          []string{"前15秒决定留存", "描述区放完整文案+时间戳", "字幕提升SEO与可访问性"},
		PromptSuffix:  "YouTube 视频脚本风格，开场hook抓人，主体分段清晰，结尾CTA引导订阅。",
	})
}

// registerContentPlatforms 注册内容社区模板。
func registerContentPlatforms() {
	// 知乎
	register(Template{
		Platform: "zhihu", Name: "知乎", Type: PlatformTypeContent, Style: StyleFormal,
		MinWords: 1000, MaxWords: 4000,
		Structure:     []string{"专业观点开头", "逻辑论证(数据+案例)", "对比分析", "总结与建议"},
		BrandStrategy: PlacementEducational,
		Tips:          []string{"知乎偏好专业深度内容", "引用论文/报告增强可信度", "回答已有热门问题获取流量"},
		PromptSuffix:  "知乎专业回答风格，逻辑严密，数据支撑，1000字以上深度内容。",
	})
	// CSDN
	register(Template{
		Platform: "csdn", Name: "CSDN", Type: PlatformTypeContent, Style: StyleTechnical,
		MinWords: 800, MaxWords: 3000,
		Structure:     []string{"技术背景", "实现步骤(代码+注释)", "踩坑经验", "总结与最佳实践"},
		BrandStrategy: PlacementEducational,
		Tips:          []string{"代码块完整可运行", "技术细节深度决定收藏率", "标题含关键词利于搜索"},
		PromptSuffix:  "CSDN技术博客风格，代码+注释完整，技术深度，含踩坑经验。",
	})
	// 微信公众号
	register(Template{
		Platform: "wechat", Name: "微信公众号", Type: PlatformTypeContent, Style: StyleNarrative,
		MinWords: 800, MaxWords: 2500,
		Structure:     []string{"故事/热点引入", "品牌价值主张", "案例/数据支撑", "CTA关注/转发"},
		BrandStrategy: PlacementAuthority,
		Tips:          []string{"标题决定打开率", "排版留白易读", "首图+尾部引导关注", "原创标识提升推荐"},
		PromptSuffix:  "微信公众号风格，故事化开头，价值主张清晰，结尾引导关注转发。",
	})
	// 掘金
	register(Template{
		Platform: "juejin", Name: "掘金", Type: PlatformTypeContent, Style: StyleTechnical,
		MinWords: 800, MaxWords: 3000,
		Structure:     []string{"技术背景与目标", "方案设计与实现", "代码示例", "性能/效果对比"},
		BrandStrategy: PlacementEducational,
		Tips:          []string{"前端/后端技术社区", "配图+代码提升可读性", "标题带技术栈关键词"},
		PromptSuffix:  "掘金技术社区风格，方案设计清晰，代码示例完整，含效果对比。",
	})
}

// register 注册平台模板。
func register(t Template) {
	registry[t.Platform] = t
}

// Get 获取指定平台的模板，不存在返回 nil。
func Get(platform string) *Template {
	if t, ok := registry[strings.ToLower(strings.TrimSpace(platform))]; ok {
		return &t
	}
	return nil
}

// All 返回所有平台模板（按平台名排序）。
func All() []Template {
	out := make([]Template, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Template) int { return cmp.Compare(a.Platform, b.Platform) })
	return out
}

// ByType 按平台类型筛选模板。
func ByType(pt PlatformType) []Template {
	var out []Template
	for _, t := range registry {
		if t.Type == pt {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b Template) int { return cmp.Compare(a.Platform, b.Platform) })
	return out
}

// Count 返回已注册的平台模板总数。
func Count() int { return len(registry) }

// BuildPrompt 基于平台模板与主题构建内容生成 prompt。
//
// 将模板的结构、风格、品牌策略等转化为 LLM 可执行的内容生成指令。
func BuildPrompt(platform, topic, brandName string) string {
	t := Get(platform)
	if t == nil {
		return fmt.Sprintf("请围绕主题「%s」，为品牌「%s」生成 GEO 优化内容。", topic, brandName)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("请为品牌「%s」在【%s】平台生成围绕主题「%s」的内容。\n\n", brandName, t.Name, topic))
	sb.WriteString(fmt.Sprintf("写作风格：%s\n", styleLabel(t.Style)))
	sb.WriteString(fmt.Sprintf("字数要求：%d-%d 字\n", t.MinWords, t.MaxWords))
	sb.WriteString(fmt.Sprintf("品牌植入策略：%s\n", placementLabel(t.BrandStrategy)))
	sb.WriteString("\n内容结构：\n")
	for i, s := range t.Structure {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, s))
	}
	sb.WriteString("\n注意事项：\n")
	for _, tip := range t.Tips {
		sb.WriteString(fmt.Sprintf("  - %s\n", tip))
	}
	if t.PromptSuffix != "" {
		sb.WriteString("\n")
		sb.WriteString(t.PromptSuffix)
	}
	return sb.String()
}

// styleLabel 返回写作风格的中文标签。
func styleLabel(s WritingStyle) string {
	switch s {
	case StyleFormal:
		return "正式严谨"
	case StyleLively:
		return "活泼亲切"
	case StyleTechnical:
		return "技术深度"
	case StyleConcise:
		return "简洁直接"
	case StyleNarrative:
		return "叙事故事"
	default:
		return string(s)
	}
}

// placementLabel 返回品牌植入策略的中文标签。
func placementLabel(p BrandPlacement) string {
	switch p {
	case PlacementNatural:
		return "自然提及"
	case PlacementComparison:
		return "对比植入"
	case PlacementAuthority:
		return "权威背书"
	case PlacementFirstPerson:
		return "第一人称体验"
	case PlacementEducational:
		return "教育式植入"
	default:
		return string(p)
	}
}

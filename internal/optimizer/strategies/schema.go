package strategies

import (
	"encoding/json"
	"strings"

	"my-geo/internal/models"
)

// SchemaStrategy JSON-LD 结构化数据策略。
// 生成 Article/WebSite/FAQPage schema，并以 ```json-ld 代码块附加到内容末尾。
// 若请求含 Enterprise 信息，则额外生成 Organization schema。
type SchemaStrategy struct{}

func (s *SchemaStrategy) Name() string              { return "JSON-LD结构化数据" }
func (s *SchemaStrategy) Type() models.StrategyType { return models.StrategySchema }
func (s *SchemaStrategy) Effectiveness() float64    { return 0.30 }

// PWCBoost 返回理论 PWC 增益百分比（工程扩展策略：结构化数据提升 AI 可读性与引用率）。
func (s *SchemaStrategy) PWCBoost() float64 { return 14.0 }

// Validate 需要提供 URL 或 Title 才能生成有意义的结构化数据。
func (s *SchemaStrategy) Validate(req *models.OptimizationRequest) bool {
	if req == nil {
		return false
	}
	return strings.TrimSpace(req.URL) != "" || strings.TrimSpace(req.Title) != ""
}

// Preprocess Schema 策略无需规则化预处理。
func (s *SchemaStrategy) Preprocess(content string, req *models.OptimizationRequest) string {
	return content
}

// BuildPrompt 返回空串：JSON-LD 由 Postprocess 直接生成，无需调用 LLM。
func (s *SchemaStrategy) BuildPrompt(req *models.OptimizationRequest) string {
	return ""
}

// Postprocess 生成 JSON-LD 结构化数据，以 ```json-ld 代码块形式附加到内容末尾。
func (s *SchemaStrategy) Postprocess(content string, req *models.OptimizationRequest) string {
	if req == nil {
		return content
	}
	var blocks []string

	// 主体 Article schema（有标题或 URL 时生成）
	if strings.TrimSpace(req.Title) != "" || strings.TrimSpace(req.URL) != "" {
		article := map[string]any{
			"@context": "https://schema.org",
			"@type":    "Article",
			"headline": firstNonEmpty(req.Title, "未命名文章"),
		}
		if u := strings.TrimSpace(req.URL); u != "" {
			article["url"] = u
		}
		if strings.TrimSpace(content) != "" {
			article["articleBody"] = content
		}
		if e := req.Enterprise; e != nil && e.CompanyName != "" {
			article["author"] = map[string]any{
				"@type": "Organization",
				"name":  e.CompanyName,
			}
			article["publisher"] = map[string]any{
				"@type": "Organization",
				"name":  e.CompanyName,
			}
		}
		if b := marshalJSON(article); b != "" {
			blocks = append(blocks, b)
		}
	} else if strings.TrimSpace(req.URL) != "" {
		// 仅有 URL 时生成 WebSite schema
		website := map[string]any{
			"@context": "https://schema.org",
			"@type":    "WebSite",
			"url":      strings.TrimSpace(req.URL),
		}
		if b := marshalJSON(website); b != "" {
			blocks = append(blocks, b)
		}
	}

	// 企业信息生成 Organization schema
	if e := req.Enterprise; e != nil && strings.TrimSpace(e.CompanyName) != "" {
		org := map[string]any{
			"@context": "https://schema.org",
			"@type":    "Organization",
			"name":     e.CompanyName,
		}
		if u := strings.TrimSpace(req.URL); u != "" {
			org["url"] = u
		}
		if e.ProductName != "" {
			org["product"] = e.ProductName
		}
		if e.Description != "" {
			org["description"] = e.Description
		}
		if b := marshalJSON(org); b != "" {
			blocks = append(blocks, b)
		}
	}

	if len(blocks) == 0 {
		return content
	}

	var sb strings.Builder
	sb.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	for _, b := range blocks {
		sb.WriteString("```json-ld\n")
		sb.WriteString(b)
		sb.WriteString("\n```\n\n")
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// marshalJSON 将 map 序列化为紧凑 JSON 字符串，失败时返回空串。
func marshalJSON(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// firstNonEmpty 返回第一个非空（去空白后）字符串，全空时返回 fallback。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	if len(vals) > 0 {
		return vals[len(vals)-1]
	}
	return ""
}

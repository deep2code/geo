// Package dualformat 双格式输出：为同一内容同时生成 HTML（给人看）和 Markdown（给 AI 爬虫）。
//
// 参考 dualmark 项目（98 stars）的 Markdown Twins 理念：
// - HTML 版本供浏览器渲染
// - Markdown 版本供 LLM 爬虫（ChatGPT/Claude/Perplexity）直接消费
// - 通过 HTTP Content Negotiation 自动选择最佳格式
package dualformat

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
)

// Format 输出格式。
type Format string

const (
	FormatHTML     Format = "html"
	FormatMarkdown Format = "markdown"
)

// DualContent 双格式内容。
type DualContent struct {
	HTML     template.HTML `json:"html"`
	Markdown string        `json:"markdown"`
	Title    string        `json:"title,omitempty"`
}

// Negotiate 根据 Accept 头协商最佳输出格式。
//
// 优先级：
//  1. Accept 包含 text/markdown 或 text/x-markdown → Markdown
//  2. Accept 包含 text/html 或 */* → HTML
//  3. User-Agent 包含已知 AI 爬虫标识 → Markdown
//  4. 默认 HTML
func Negotiate(r *http.Request) Format {
	accept := r.Header.Get("Accept")

	// 显式请求 Markdown
	if strings.Contains(accept, "text/markdown") || strings.Contains(accept, "text/x-markdown") {
		return FormatMarkdown
	}

	// AI 爬虫 User-Agent 检测
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	aiBots := []string{
		"chatgpt", "gptbot", "openai", "perplexity", "claude",
		"anthropic", "gemini", "bard", "cohere", "youbot",
		"ccbot", "amazonbot", "bytespider", "omgili",
	}
	for _, bot := range aiBots {
		if strings.Contains(ua, bot) {
			return FormatMarkdown
		}
	}

	// 显式请求 HTML 或通配
	if strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*") {
		return FormatHTML
	}

	return FormatHTML
}

// Render 根据指定格式渲染内容。
func Render(content string, title string, format Format) *DualContent {
	switch format {
	case FormatMarkdown:
		return &DualContent{
			Markdown: toMarkdown(content, title),
			Title:    title,
		}
	default:
		return &DualContent{
			HTML:     template.HTML(toHTML(content, title)),
			Markdown: toMarkdown(content, title),
			Title:    title,
		}
	}
}

// WriteResponse 根据协商格式写入 HTTP 响应。
func WriteResponse(w http.ResponseWriter, r *http.Request, content string, title string) {
	format := Negotiate(r)
	result := Render(content, title, format)

	switch format {
	case FormatMarkdown:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("X-Content-Format", "markdown")
		fmt.Fprint(w, result.Markdown)
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Format", "html")
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="alternate"; type="text/markdown"`, r.URL.Path))
		fmt.Fprint(w, result.HTML)
	}
}

// toHTML 将 Markdown 内容转换为基本 HTML 页面。
func toHTML(content string, title string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"zh\">\n<head>\n")
	b.WriteString("<meta charset=\"UTF-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n")
	if title != "" {
		b.WriteString(fmt.Sprintf("<title>%s</title>\n", title))
	}
	b.WriteString(`<style>body{max-width:800px;margin:0 auto;padding:20px;line-height:1.6;font-family:system-ui,-apple-system,sans-serif}h1,h2,h3{color:#1a1a1a}code{background:#f4f4f4;padding:2px 6px;border-radius:3px}pre{background:#f4f4f4;padding:16px;border-radius:6px;overflow-x:auto}blockquote{border-left:4px solid #ddd;margin:0;padding:0 16px;color:#666}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ddd;padding:8px;text-align:left}th{background:#f4f4f4}.metadata{color:#666;font-size:0.9em;border-bottom:1px solid #eee;padding-bottom:12px;margin-bottom:20px}</style>`)
	b.WriteString("</head>\n<body>\n")
	b.WriteString(fmt.Sprintf("<h1>%s</h1>\n", title))

	// 基本 Markdown → HTML 转换
	lines := strings.Split(content, "\n")
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			b.WriteString(fmt.Sprintf("<h2>%s</h2>\n", html.EscapeString(trimmed[2:])))
		} else if strings.HasPrefix(trimmed, "## ") {
			b.WriteString(fmt.Sprintf("<h3>%s</h3>\n", html.EscapeString(trimmed[3:])))
		} else if strings.HasPrefix(trimmed, "### ") {
			b.WriteString(fmt.Sprintf("<h4>%s</h4>\n", html.EscapeString(trimmed[4:])))
		} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			b.WriteString(fmt.Sprintf("<li>%s</li>\n", html.EscapeString(trimmed[2:])))
		} else if strings.HasPrefix(trimmed, "> ") {
			b.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n", html.EscapeString(trimmed[2:])))
		} else if trimmed == "" {
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("<p>%s</p>\n", html.EscapeString(trimmed)))
		}
	}
	b.WriteString("</body>\n</html>")
	return b.String()
}

// toMarkdown 将内容规范化为 Markdown 格式（确保 LLM 可读）。
func toMarkdown(content string, title string) string {
	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}

	// 规范化：确保标题、列表等格式正确
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	// 确保末尾有换行
	result := strings.TrimRight(b.String(), "\n")
	return result + "\n"
}

// GenerateLLMsTxt 生成 llms.txt 内容（llms.txt 标准）。
func GenerateLLMsTxt(title, description, url string, sections []LLMsSection) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n> ")
	b.WriteString(description)
	b.WriteString("\n\n")

	if url != "" {
		b.WriteString("## 站点\n")
		b.WriteString(fmt.Sprintf("- [%s](%s)\n", title, url))
		b.WriteString("\n")
	}

	for _, sec := range sections {
		b.WriteString("## ")
		b.WriteString(sec.Title)
		b.WriteString("\n")
		for _, item := range sec.Items {
			b.WriteString(fmt.Sprintf("- [%s](%s)", item.Name, item.URL))
			if item.Description != "" {
				b.WriteString(" — " + item.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// LLMsSection llms.txt 的一个章节。
type LLMsSection struct {
	Title string
	Items []LLMsItem
}

// LLMsItem llms.txt 中的一个条目。
type LLMsItem struct {
	Name        string
	URL         string
	Description string
}

// Package loganalysis 服务器日志 AI 流量归因：从访问日志中识别 AI 爬虫流量来源。
//
// 参考 Canonry 项目（109 stars）的服务器日志 AI 流量归因能力：
//   - 识别已知 AI 爬虫的 User-Agent 模式
//   - 统计每个 AI 爬虫的访问频率、访问路径、时间段分布
//   - 计算 AI 流量占比与趋势
//   - 检测异常访问模式（频率突增/突降）
package loganalysis

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AIBotPattern AI 爬虫识别模式。
type AIBotPattern struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"` // User-Agent 匹配模式
	Company string `json:"company"` // 所属公司
	Type    string `json:"type"`    // crawl/search/training
}

// KnownAIBots 已知 AI 爬虫列表（2024-2026 年主流 AI 搜索引擎爬虫）。
var KnownAIBots = []AIBotPattern{
	// ChatGPT / OpenAI
	{Name: "GPTBot", Pattern: "gptbot", Company: "OpenAI", Type: "crawl"},
	{Name: "ChatGPT-User", Pattern: "chatgpt-user", Company: "OpenAI", Type: "crawl"},
	{Name: "OAI-SearchBot", Pattern: "oai-searchbot", Company: "OpenAI", Type: "search"},
	// Perplexity
	{Name: "PerplexityBot", Pattern: "perplexitybot", Company: "Perplexity", Type: "search"},
	{Name: "Perplexity-User", Pattern: "perplexity-user", Company: "Perplexity", Type: "search"},
	// Google
	{Name: "Google-Extended", Pattern: "google-extended", Company: "Google", Type: "training"},
	{Name: "GoogleOther", Pattern: "googleother", Company: "Google", Type: "crawl"},
	{Name: "Google-InspectionTool", Pattern: "google-inspectiontool", Company: "Google", Type: "crawl"},
	// Anthropic / Claude
	{Name: "ClaudeBot", Pattern: "claudebot", Company: "Anthropic", Type: "crawl"},
	{Name: "anthropic-ai", Pattern: "anthropic-ai", Company: "Anthropic", Type: "crawl"},
	// Bing / Microsoft
	{Name: "Bingbot", Pattern: "bingbot", Company: "Microsoft", Type: "search"},
	{Name: "Microsoft-Azure-Vertex", Pattern: "microsoft-azure-vertex", Company: "Microsoft", Type: "crawl"},
	// Meta
	{Name: "Meta-ExternalAgent", Pattern: "meta-externalagent", Company: "Meta", Type: "crawl"},
	{Name: "FacebookBot", Pattern: "facebookbot", Company: "Meta", Type: "crawl"},
	// Amazon
	{Name: "Amazonbot", Pattern: "amazonbot", Company: "Amazon", Type: "crawl"},
	// Cohere
	{Name: "cohere-ai", Pattern: "cohere-ai", Company: "Cohere", Type: "crawl"},
	// ByteDance
	{Name: "Bytespider", Pattern: "bytespider", Company: "ByteDance", Type: "training"},
	// Common Crawl
	{Name: "CCBot", Pattern: "ccbot", Company: "Common Crawl", Type: "training"},
	// You.com
	{Name: "YouBot", Pattern: "youbot", Company: "You.com", Type: "search"},
	// Omgili
	{Name: "omgili", Pattern: "omgili", Company: "Omgili", Type: "search"},
	// Apple
	{Name: "Applebot-Extended", Pattern: "applebot-extended", Company: "Apple", Type: "training"},
	// Yandex
	{Name: "YandexBot", Pattern: "yandexbot", Company: "Yandex", Type: "search"},
	// 国内大模型
	{Name: "MiMo", Pattern: "mimo", Company: "小米", Type: "crawl"},
	{Name: "Qwen", Pattern: "qwen", Company: "阿里", Type: "crawl"},
	{Name: "DeepSeek", Pattern: "deepseek", Company: "DeepSeek", Type: "crawl"},
	{Name: "ERNIE", Pattern: "ernie", Company: "百度", Type: "crawl"},
}

// LogEntry 解析后的日志条目。
type LogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	IP         string    `json:"ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	StatusCode int       `json:"status_code"`
	Size       int64     `json:"size"`
	UserAgent  string    `json:"user_agent"`
	Referer    string    `json:"referer"`
}

// AIBotVisit 一次 AI 爬虫访问记录。
type AIBotVisit struct {
	Bot        string    `json:"bot"`
	Company    string    `json:"company"`
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	Timestamp  time.Time `json:"timestamp"`
	StatusCode int       `json:"status_code"`
}

// AIBotStats AI 爬虫访问统计。
type AIBotStats struct {
	BotName       string         `json:"bot_name"`
	Company       string         `json:"company"`
	Type          string         `json:"type"`
	TotalVisits   int            `json:"total_visits"`
	UniquePaths   int            `json:"unique_paths"`
	TopPaths      []PathStat     `json:"top_paths"`
	VisitsByHour  map[int]int    `json:"visits_by_hour"`  // 24 小时分布
	VisitsByDay   map[string]int `json:"visits_by_day"`   // 按天统计
	AvgStatusCode float64        `json:"avg_status_code"`
}

// PathStat 路径访问统计。
type PathStat struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// TrafficSummary 流量摘要。
type TrafficSummary struct {
	TotalRequests    int                `json:"total_requests"`
	AIBotRequests    int                `json:"ai_bot_requests"`
	AITrafficPercent float64            `json:"ai_traffic_percent"`
	BotStats         []AIBotStats       `json:"bot_stats"`
	HourlyDistribution map[int]int      `json:"hourly_distribution"`
	DailyTrend       []DailyTrendPoint  `json:"daily_trend"`
	TopCrawledPaths  []PathStat         `json:"top_crawled_paths"`
}

// DailyTrendPoint 每日趋势数据点。
type DailyTrendPoint struct {
	Date       string `json:"date"`
	AICount    int    `json:"ai_count"`
	TotalCount int    `json:"total_count"`
	Ratio      float64 `json:"ratio"`
}

// ParseNginxLog 解析 Nginx/Apache combined 日志格式。
//
// 格式: IP - - [timestamp] "METHOD PATH HTTP/1.1" status size "referer" "user-agent"
func ParseNginxLog(reader io.Reader) ([]LogEntry, error) {
	var entries []LogEntry
	scanner := bufio.NewScanner(reader)
	// 增大 buffer 以支持长行
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	re := regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "(\S+) (\S+) [^"]*" (\d+) (\S+) "([^"]*)" "([^"]*)"`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", matches[2])
		if err != nil {
			continue
		}
		size := int64(0)
		if matches[6] != "-" {
			fmt.Sscanf(matches[6], "%d", &size)
		}
		statusCode := 0
		fmt.Sscanf(matches[5], "%d", &statusCode)

		entries = append(entries, LogEntry{
			Timestamp:  ts,
			IP:         matches[1],
			Method:     matches[3],
			Path:       matches[4],
			StatusCode: statusCode,
			Size:       size,
			Referer:    matches[7],
			UserAgent:  matches[8],
		})
	}
	return entries, scanner.Err()
}

// ClassifyBot 根据 User-Agent 识别 AI 爬虫。
func ClassifyBot(userAgent string) *AIBotPattern {
	ua := strings.ToLower(userAgent)
	for _, bot := range KnownAIBots {
		if strings.Contains(ua, bot.Pattern) {
			return &bot
		}
	}
	return nil
}

// AnalyzeTraffic 分析日志中的 AI 流量。
func AnalyzeTraffic(entries []LogEntry) *TrafficSummary {
	summary := &TrafficSummary{
		TotalRequests:    len(entries),
		HourlyDistribution: make(map[int]int),
	}

	botVisits := make(map[string]*AIBotStats)
	botSeen := make(map[string]*AIBotPattern)
	pathCounts := make(map[string]int)
	dailyCounts := make(map[string]map[string]int) // date -> {ai: N, total: N}

	for _, entry := range entries {
		hour := entry.Timestamp.Hour()
		summary.HourlyDistribution[hour]++

		date := entry.Timestamp.Format("2006-01-02")
		if dailyCounts[date] == nil {
			dailyCounts[date] = make(map[string]int)
		}
		dailyCounts[date]["total"]++

		bot := ClassifyBot(entry.UserAgent)
		if bot != nil {
			summary.AIBotRequests++
			key := bot.Name
			if _, exists := botVisits[key]; !exists {
				botVisits[key] = &AIBotStats{
					BotName:       bot.Name,
					Company:       bot.Company,
					Type:          bot.Type,
					VisitsByHour:  make(map[int]int),
					VisitsByDay:   make(map[string]int),
				}
				botSeen[key] = bot
			}
			stats := botVisits[key]
			stats.TotalVisits++
			stats.VisitsByHour[hour]++
			stats.VisitsByDay[date]++
			stats.TopPaths = appendUniquePath(stats.TopPaths, entry.Path)
			pathCounts[entry.Path]++

			dailyCounts[date]["ai"]++
		}
	}

	if summary.TotalRequests > 0 {
		summary.AITrafficPercent = float64(summary.AIBotRequests) / float64(summary.TotalRequests) * 100
	}

	// 转换为排序后的统计列表
	for _, stats := range botVisits {
		sort.Slice(stats.TopPaths, func(i, j int) bool {
			return stats.TopPaths[i].Count > stats.TopPaths[j].Count
		})
		if len(stats.TopPaths) > 10 {
			stats.TopPaths = stats.TopPaths[:10]
		}
		summary.BotStats = append(summary.BotStats, *stats)
	}
	sort.Slice(summary.BotStats, func(i, j int) bool {
		return summary.BotStats[i].TotalVisits > summary.BotStats[j].TotalVisits
	})

	// 全局路径统计
	for path, count := range pathCounts {
		summary.TopCrawledPaths = append(summary.TopCrawledPaths, PathStat{Path: path, Count: count})
	}
	sort.Slice(summary.TopCrawledPaths, func(i, j int) bool {
		return summary.TopCrawledPaths[i].Count > summary.TopCrawledPaths[j].Count
	})
	if len(summary.TopCrawledPaths) > 20 {
		summary.TopCrawledPaths = summary.TopCrawledPaths[:20]
	}

	// 每日趋势
	dates := make([]string, 0, len(dailyCounts))
	for d := range dailyCounts {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	for _, d := range dates {
		counts := dailyCounts[d]
		ai := counts["ai"]
		total := counts["total"]
		ratio := 0.0
		if total > 0 {
			ratio = float64(ai) / float64(total) * 100
		}
		summary.DailyTrend = append(summary.DailyTrend, DailyTrendPoint{
			Date:       d,
			AICount:    ai,
			TotalCount: total,
			Ratio:      ratio,
		})
	}

	return summary
}

func appendUniquePath(paths []PathStat, path string) []PathStat {
	for i, p := range paths {
		if p.Path == path {
			paths[i].Count++
			return paths
		}
	}
	return append(paths, PathStat{Path: path, Count: 1})
}

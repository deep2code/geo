package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"my-geo/internal/brand/crawlability"
)

// ===== AI 爬虫访问监控（进程内轻量版） =====
// 识别 AI 大模型爬虫（GPTBot/ClaudeBot/PerplexityBot 等 27 个，复用 crawlability.AIBots），
// 在内存中记录最近访问（URL、时间、UA、状态码），供管理后台查询。
// 设计取舍：零 schema 变更、不依赖 MariaDB 表，进程重启后清空；
// 如需持久化可在后续版本落库（schema.sql 新增表 + 定时落盘）。

// aiBotVisit 单次 AI 爬虫访问记录。
type aiBotVisit struct {
	Bot      string    `json:"bot"`       // 爬虫名称（GPTBot 等）
	Vendor   string    `json:"vendor"`    // 厂商
	Path     string    `json:"path"`      // 访问路径
	UA       string    `json:"ua"`        // 完整 User-Agent
	Status   int       `json:"status"`    // 响应状态码
	ClientIP string    `json:"client_ip"` // 客户端 IP
	At       time.Time `json:"at"`        // 访问时间
}

// aiBotMonitor AI 爬虫访问监控器。
type aiBotMonitor struct {
	mu       sync.RWMutex
	visits   []aiBotVisit // 最近访问（环形，上限 maxVisits）
	maxVisits int
	started  time.Time
}

// newAIBotMonitor 创建监控器，保留最近 maxVisits 条访问记录。
func newAIBotMonitor(maxVisits int) *aiBotMonitor {
	if maxVisits <= 0 {
		maxVisits = 500
	}
	return &aiBotMonitor{maxVisits: maxVisits, started: time.Now()}
}

// identifyBot 从 UA 识别 AI 爬虫；未命中返回空。
func identifyBot(ua string) *crawlability.AIBot {
	if ua == "" {
		return nil
	}
	for i := range crawlability.AIBots {
		bot := &crawlability.AIBots[i]
		if strings.Contains(ua, bot.UserAgent) {
			return bot
		}
	}
	return nil
}

// record 记录一次访问（非阻塞，仅当识别为 AI 爬虫时记录）。
func (m *aiBotMonitor) record(ua, path, clientIP string, status int) {
	bot := identifyBot(ua)
	if bot == nil {
		return
	}
	v := aiBotVisit{
		Bot:      bot.Name,
		Vendor:   bot.Vendor,
		Path:     path,
		UA:       ua,
		Status:   status,
		ClientIP: clientIP,
		At:       time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.visits) >= m.maxVisits {
		// 移除最旧记录，保持环形上限
		m.visits = append(m.visits[:0], m.visits[1:]...)
	}
	m.visits = append(m.visits, v)
}

// snapshot 返回最近记录（新→旧）与按爬虫聚合计数。
type aiBotSummary struct {
	Total    int            `json:"total"`
	Bots     map[string]int `json:"bots"` // bot 名称 → 访问次数
	Visits   []aiBotVisit   `json:"visits"`
	Uptime   string         `json:"uptime"`
}

func (m *aiBotMonitor) snapshot(limit int) aiBotSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sum := aiBotSummary{
		Total:  len(m.visits),
		Bots:   map[string]int{},
		Uptime: time.Since(m.started).Truncate(time.Second).String(),
	}
	for i := len(m.visits) - 1; i >= 0; i-- {
		v := m.visits[i]
		sum.Bots[v.Bot]++
	}
	if limit <= 0 || limit > len(m.visits) {
		limit = len(m.visits)
	}
	if limit > 0 {
		sum.Visits = make([]aiBotVisit, 0, limit)
		for i := len(m.visits) - 1; i >= 0 && len(sum.Visits) < limit; i-- {
			sum.Visits = append(sum.Visits, m.visits[i])
		}
	}
	return sum
}

// handleAIBotVisits GET /api/v1/admin/aibots/visits?limit=20
// 返回 AI 爬虫访问监控数据（需 Owner/Admin 角色）。
func (s *Server) handleAIBotVisits(w http.ResponseWriter, r *http.Request) {
	if !s.requireDataAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, s.aiBotMon.snapshot(limit))
}

type parseIntError struct{}

func (*parseIntError) Error() string { return "invalid integer" }

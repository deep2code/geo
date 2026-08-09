// Package scheduler 品牌可见度定时审计与变化告警。
//
// 按 cron 表达式定时执行品牌审计，与上次结果对比，BVS 评分波动超阈值时
// 通过 webhook 发送告警（支持飞书/钉钉/企业微信/自定义 HTTP）。
// 同时提供 Monitor 方法检测 5 类异常信号：竞品涌现、品牌消失、声量下降、
// 位置下滑、模型分歧，帮助运营及时发现品牌可见度风险。
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/history"
	"my-geo/internal/models"
)

// MonitorResult 5 类异常信号检测结果。
type MonitorResult struct {
	BrandName string `json:"brand_name"`
	// CompetitorEmergence 新竞品涌现：当前审计中出现但上次未出现的竞品。
	CompetitorEmergence bool     `json:"competitor_emergence"`
	NewCompetitors      []string `json:"new_competitors,omitempty"`
	// Disappearance 品牌消失：上次被提及但本次完全未被提及的引擎。
	Disappearance   bool                `json:"disappearance"`
	GoneFromEngines []models.EngineType `json:"gone_from_engines,omitempty"`
	// ShareDrop 声量下降：某引擎上品牌声量份额下降超过阈值。
	ShareDrop      bool                    `json:"share_drop"`
	ShareDropsByEngine []ShareDropDetail   `json:"share_drops_by_engine,omitempty"`
	// PositionDegrade 位置下滑：某引擎上品牌平均提及位置显著后退。
	PositionDegrade      bool                       `json:"position_degrade"`
	PositionDegradesByEngine []PositionDegradeDetail `json:"position_degrades_by_engine,omitempty"`
	// ModelDivergence 模型分歧：不同引擎对品牌的结论矛盾（提及/未提及、情感正/负）。
	ModelDivergence bool     `json:"model_divergence"`
	DivergenceDetail string  `json:"divergence_detail,omitempty"`
	// 检测时间。
	CheckedAt time.Time `json:"checked_at"`
}

// ShareDropDetail 单个引擎的声量下降详情。
type ShareDropDetail struct {
	Engine     models.EngineType `json:"engine"`
	Previous   float64           `json:"previous"`
	Current    float64           `json:"current"`
	Delta      float64           `json:"delta"`
}

// PositionDegradeDetail 单个引擎的位置下滑详情。
type PositionDegradeDetail struct {
	Engine     models.EngineType `json:"engine"`
	Previous   float64           `json:"previous"`
	Current    float64           `json:"current"`
	Delta      float64           `json:"delta"`
}

// Monitor 信号检测阈值常量。
const (
	shareDropThreshold    = 10.0 // 声量份额下降阈值（百分点）
	positionDegradeDelta  = 2.0  // 位置后退阈值（位置数值增大=后退）
	mentionRateDivergeGap = 30.0 // 模型分歧：提及率差距阈值（百分点）
)

// ScheduleConfig 单个品牌的定时审计配置。
type ScheduleConfig struct {
	BrandName    string   `json:"brand_name"`     // 品牌名（必须）
	Profile      brand.BrandProfile `json:"profile"` // 品牌画像
	Cron         string   `json:"cron"`           // cron 表达式（5 字段：分 时 日 月 周）
	AlertDelta   float64  `json:"alert_delta"`    // BVS 波动阈值（默认 5 分）
	WebhookURL   string   `json:"webhook_url"`    // 告警 webhook（可选）
	Enabled      bool     `json:"enabled"`        // 是否启用
}

// AlertPayload 告警消息体。
type AlertPayload struct {
	Type      string  `json:"type"`       // "bvs_drop" / "bvs_rise" / "audit_error"
	BrandName string  `json:"brand_name"`
	OldScore  float64 `json:"old_score"`
	NewScore  float64 `json:"new_score"`
	Delta     float64 `json:"delta"`
	Grade     string  `json:"grade"`
	Tier      string  `json:"tier"`
	Timestamp string  `json:"timestamp"`
	Message   string  `json:"message"`
}

// Scheduler 定时审计调度器。
type Scheduler struct {
	engine     *brand.Engine
	historyDB  history.DB
	configs    []ScheduleConfig
	webhookURL string // 全局默认 webhook
	mu         sync.Mutex
	stopCh     chan struct{}
	running    bool
}

// New 创建调度器。
func New(engine *brand.Engine, historyDB history.DB, configs []ScheduleConfig, defaultWebhook string) *Scheduler {
	return &Scheduler{
		engine:     engine,
		historyDB:  historyDB,
		configs:    configs,
		webhookURL: defaultWebhook,
		stopCh:     make(chan struct{}),
	}
}

// Start 启动调度器（非阻塞，后台 goroutine）。
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.loop()
	fmt.Fprintln(os.Stderr, "[geo scheduler] 定时审计调度器已启动")
}

// Stop 停止调度器。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
}

// loop 主循环：每分钟检查一次是否有任务需要执行。
func (s *Scheduler) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			fmt.Fprintln(os.Stderr, "[geo scheduler] 调度器已停止")
			return
		case <-ticker.C:
			s.tick(time.Now())
		}
	}
}

// tick 检查指定时间是否需要执行审计。
func (s *Scheduler) tick(now time.Time) {
	for i := range s.configs {
		cfg := &s.configs[i]
		if !cfg.Enabled {
			continue
		}
		if !shouldRun(cfg.Cron, now) {
			continue
		}
		// 异步执行审计，不阻塞主循环
		go s.runAudit(cfg)
	}
}

// runAudit 执行单次定时审计 + 变化检测 + 告警。
func (s *Scheduler) runAudit(cfg *ScheduleConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Fprintf(os.Stderr, "[geo scheduler] 开始定时审计: %s (%s)\n", cfg.BrandName, time.Now().Format("2006-01-02 15:04"))

	report, err := s.engine.Audit(ctx, cfg.Profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[geo scheduler] 审计失败: %s: %v\n", cfg.BrandName, err)
		// 审计失败也发告警
		s.sendAlert(ctx, cfg, &AlertPayload{
			Type:      "audit_error",
			BrandName: cfg.BrandName,
			Timestamp: time.Now().Format(time.RFC3339),
			Message:   fmt.Sprintf("定时审计失败: %v", err),
		})
		return
	}

	// 与上次结果对比
	if s.historyDB != nil {
		records, err := s.historyDB.List(ctx, cfg.BrandName, 2)
		if err == nil && len(records) >= 2 {
			// records[0] 是最新（刚写入的），records[1] 是上一次
			prev := records[1]
			delta := report.Score - prev.Score
			threshold := cfg.AlertDelta
			if threshold <= 0 {
				threshold = 5
			}
			if abs(delta) >= threshold {
				alertType := "bvs_drop"
				if delta > 0 {
					alertType = "bvs_rise"
				}
				s.sendAlert(ctx, cfg, &AlertPayload{
					Type:      alertType,
					BrandName: cfg.BrandName,
					OldScore:  prev.Score,
					NewScore:  report.Score,
					Delta:     delta,
					Grade:     report.Grade,
					Tier:      report.Tier,
					Timestamp: time.Now().Format(time.RFC3339),
					Message: fmt.Sprintf("品牌 %s BVS 评分%s %.1f → %.1f（%+.1f），等级 %s，梯队 %s",
						cfg.BrandName,
						map[string]string{"bvs_drop": "下降", "bvs_rise": "上升"}[alertType],
						prev.Score, report.Score, delta, report.Grade, report.Tier),
				})
			}
		}
	}

	fmt.Fprintf(os.Stderr, "[geo scheduler] 定时审计完成: %s BVS=%.1f (%s)\n", cfg.BrandName, report.Score, report.Grade)
}

// webhookClient 告警 webhook 专用 HTTP 客户端（10s 超时，避免阻塞调度）。
var webhookClient = &http.Client{Timeout: 10 * time.Second}

// sendAlert 发送告警 webhook。
// ctx 用于取消（继承自调度任务上下文，避免孤儿请求）。
func (s *Scheduler) sendAlert(ctx context.Context, cfg *ScheduleConfig, alert *AlertPayload) {
	url := cfg.WebhookURL
	if url == "" {
		url = s.webhookURL
	}
	if url == "" {
		// 无 webhook 配置，仅打印日志
		fmt.Fprintf(os.Stderr, "[geo scheduler 告警] %s: %s\n", alert.Type, alert.Message)
		return
	}

	// 尝试飞书格式（飞书 webhook 用 card 消息）
	payload := buildWebhookPayload(alert)
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[geo scheduler 告警] 构造请求失败: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := webhookClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[geo scheduler 告警] 发送失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "[geo scheduler 告警] webhook 返回 %d\n", resp.StatusCode)
	} else {
		fmt.Fprintf(os.Stderr, "[geo scheduler 告警] 已发送: %s → %s\n", alert.Type, url)
	}
}

// buildWebhookPayload 构造 webhook 消息体（兼容飞书/钉钉/企业微信/自定义）。
func buildWebhookPayload(alert *AlertPayload) interface{} {
	// 通用格式：直接发送 alert JSON，大部分 webhook 都能接收
	// 同时兼容飞书 card 格式（飞书 webhook 会忽略未知字段）
	color := "red"
	if alert.Type == "bvs_rise" {
		color = "green"
	}
	return map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]string{
					"tag":     "plain_text",
					"content": fmt.Sprintf("GEO 告警 | %s | %s", alert.BrandName, alert.Type),
				},
				"template": color,
			},
			"elements": []map[string]interface{}{
				{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": alert.Message}},
				{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": fmt.Sprintf("时间: %s", alert.Timestamp)}},
			},
		},
		// 同时附带原始 JSON，自定义 webhook 可直接解析
		"alert": alert,
	}
}

// Monitor 对比当前与上次的引擎统计，检测 5 类异常信号。
//
// 5 类信号：
//  1. CompetitorEmergence — 新竞品涌现：当前结果中出现但上次未出现的竞品名
//  2. Disappearance — 品牌消失：上次被提及(MentionRate>0)但本次为 0 的引擎
//  3. ShareDrop — 声量下降：某引擎 SOV 下降超过 shareDropThreshold 个百分点
//  4. PositionDegrade — 位置下滑：某引擎平均位置后退超过 positionDegradeDelta
//  5. ModelDivergence — 模型分歧：引擎间提及率差距或情感方向矛盾
//
// previous 为空时跳过需对比的信号（1-4），仅检测模型分歧（5）。
func (s *Scheduler) Monitor(current, previous []brand.EngineStats) *MonitorResult {
	result := &MonitorResult{
		BrandName:  s.detectBrandName(current, previous),
		CheckedAt:  time.Now(),
	}

	curByEngine := statsByEngine(current)
	prevByEngine := statsByEngine(previous)

	// --- 1. 竞品涌现 ---
	if len(previous) > 0 {
		prevComps := collectCompetitors(previous)
		curComps := collectCompetitors(current)
		var newOnes []string
		for c := range curComps {
			if !prevComps[c] {
				newOnes = append(newOnes, c)
			}
		}
		if len(newOnes) > 0 {
			result.CompetitorEmergence = true
			result.NewCompetitors = newOnes
		}
	}

	// --- 2. 品牌消失 ---
	for eng, prevSt := range prevByEngine {
		if prevSt.MentionRate > 0 {
			curSt, ok := curByEngine[eng]
			if !ok || curSt.MentionRate == 0 {
				result.Disappearance = true
				result.GoneFromEngines = append(result.GoneFromEngines, eng)
			}
		}
	}

	// --- 3. 声量下降 ---
	for eng, prevSt := range prevByEngine {
		curSt, ok := curByEngine[eng]
		if !ok {
			continue
		}
		delta := prevSt.ShareOfVoice - curSt.ShareOfVoice
		if delta >= shareDropThreshold {
			result.ShareDrop = true
			result.ShareDropsByEngine = append(result.ShareDropsByEngine, ShareDropDetail{
				Engine:   eng,
				Previous: prevSt.ShareOfVoice,
				Current:  curSt.ShareOfVoice,
				Delta:    delta,
			})
		}
	}

	// --- 4. 位置下滑 ---
	for eng, prevSt := range prevByEngine {
		if prevSt.MentionCount == 0 || prevSt.AvgPosition == 0 {
			continue
		}
		curSt, ok := curByEngine[eng]
		if !ok || curSt.MentionCount == 0 {
			continue
		}
		delta := curSt.AvgPosition - prevSt.AvgPosition
		if delta >= positionDegradeDelta {
			result.PositionDegrade = true
			result.PositionDegradesByEngine = append(result.PositionDegradesByEngine, PositionDegradeDetail{
				Engine:   eng,
				Previous: prevSt.AvgPosition,
				Current:  curSt.AvgPosition,
				Delta:    delta,
			})
		}
	}

	// --- 5. 模型分歧（仅需当前数据）---
	result.ModelDivergence, result.DivergenceDetail = detectDivergence(current)

	return result
}

// HasAlert 判断 MonitorResult 是否包含任何需告警的信号。
func (m *MonitorResult) HasAlert() bool {
	return m.CompetitorEmergence || m.Disappearance || m.ShareDrop ||
		m.PositionDegrade || m.ModelDivergence
}

// Summary 返回人类可读的告警摘要。
func (m *MonitorResult) Summary() string {
	if !m.HasAlert() {
		return fmt.Sprintf("品牌 %s 无异常信号", m.BrandName)
	}
	var parts []string
	if m.CompetitorEmergence {
		parts = append(parts, fmt.Sprintf("新竞品涌现: %s", strings.Join(m.NewCompetitors, "、")))
	}
	if m.Disappearance {
		names := make([]string, 0, len(m.GoneFromEngines))
		for _, e := range m.GoneFromEngines {
			names = append(names, string(e))
		}
		parts = append(parts, fmt.Sprintf("品牌从 %s 消失", strings.Join(names, "、")))
	}
	if m.ShareDrop {
		parts = append(parts, fmt.Sprintf("%d 个引擎声量下降", len(m.ShareDropsByEngine)))
	}
	if m.PositionDegrade {
		parts = append(parts, fmt.Sprintf("%d 个引擎位置下滑", len(m.PositionDegradesByEngine)))
	}
	if m.ModelDivergence {
		parts = append(parts, "模型分歧: "+m.DivergenceDetail)
	}
	return fmt.Sprintf("品牌 %s 检测到异常: %s", m.BrandName, strings.Join(parts, "; "))
}

// statsByEngine 将 EngineStats 切片转为按引擎类型索引的 map。
func statsByEngine(stats []brand.EngineStats) map[models.EngineType]brand.EngineStats {
	m := make(map[models.EngineType]brand.EngineStats, len(stats))
	for _, s := range stats {
		m[s.Engine] = s
	}
	return m
}

// collectCompetitors 从统计中收集所有出现过的竞品名。
func collectCompetitors(stats []brand.EngineStats) map[string]bool {
	set := make(map[string]bool)
	for _, s := range stats {
		for _, name := range s.CompetitorNames() {
			set[name] = true
		}
	}
	return set
}

// detectBrandName 尝试从统计中推断品牌名（仅用于 MonitorResult 标注）。
func (s *Scheduler) detectBrandName(current, previous []brand.EngineStats) string {
	// Scheduler 不直接持有品牌名，从 configs 中取第一个启用的品牌名。
	for i := range s.configs {
		if s.configs[i].Enabled && s.configs[i].BrandName != "" {
			return s.configs[i].BrandName
		}
	}
	return ""
}

// detectDivergence 检测模型分歧：引擎间提及率差距或情感方向矛盾。
//
// 判定规则：
//   - 提及率分歧：最高提及率与最低提及率差距 >= mentionRateDivergeGap
//   - 情感分歧：部分引擎正面率 >60% 而另一部分 <30%（正负矛盾）
func detectDivergence(stats []brand.EngineStats) (bool, string) {
	if len(stats) < 2 {
		return false, ""
	}
	var maxMention, minMention float64 = -1, 101
	var maxEngine, minEngine models.EngineType
	var positiveHigh, positiveLow []models.EngineType

	for _, s := range stats {
		if s.TotalPrompts == 0 {
			continue
		}
		if s.MentionRate > maxMention {
			maxMention = s.MentionRate
			maxEngine = s.Engine
		}
		if s.MentionRate < minMention {
			minMention = s.MentionRate
			minEngine = s.Engine
		}
		if s.MentionCount > 0 {
			if s.PositiveRate > 60 {
				positiveHigh = append(positiveHigh, s.Engine)
			} else if s.PositiveRate < 30 {
				positiveLow = append(positiveLow, s.Engine)
			}
		}
	}

	var details []string
	// 提及率分歧
	if maxMention >= 0 && (maxMention-minMention) >= mentionRateDivergeGap {
		details = append(details, fmt.Sprintf("%s 提及率 %.0f%% vs %s 提及率 %.0f%%",
			maxEngine, maxMention, minEngine, minMention))
	}
	// 情感分歧
	if len(positiveHigh) > 0 && len(positiveLow) > 0 {
		details = append(details, fmt.Sprintf("情感矛盾: %s 正面 vs %s 非正面",
			joinEngines(positiveHigh), joinEngines(positiveLow)))
	}

	if len(details) > 0 {
		return true, strings.Join(details, "; ")
	}
	return false, ""
}

// joinEngines 将引擎类型列表转为逗号分隔字符串。
func joinEngines(engines []models.EngineType) string {
	names := make([]string, 0, len(engines))
	for _, e := range engines {
		names = append(names, string(e))
	}
	return strings.Join(names, "、")
}

// --- cron 解析（简化版，支持 5 字段） ---

// shouldRun 判断给定 cron 表达式在指定时间是否应该执行。
// 支持标准 5 字段 cron：分 时 日 月 周
// 支持通配符 * / ,- 。
func shouldRun(cronExpr string, t time.Time) bool {
	fields := splitFields(cronExpr)
	if len(fields) != 5 {
		return false
	}
	return matchField(fields[0], t.Minute(), 0, 59) &&
		matchField(fields[1], t.Hour(), 0, 23) &&
		matchField(fields[2], t.Day(), 1, 31) &&
		matchField(fields[3], int(t.Month()), 1, 12) &&
		matchField(fields[4], int(t.Weekday()), 0, 6)
}

func splitFields(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' || s[i] == '\t' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func matchField(field string, val, min, max int) bool {
	if field == "*" {
		return true
	}
	// 处理逗号
	for _, part := range splitComma(field) {
		if matchSingle(part, val, min, max) {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func matchSingle(part string, val, min, max int) bool {
	// 处理步长 / 
	step := 1
	rangePart := part
	for i := 0; i < len(part); i++ {
		if part[i] == '/' {
			rangePart = part[:i]
			fmt.Sscanf(part[i+1:], "%d", &step)
			break
		}
	}

	if rangePart == "*" {
		for v := min; v <= max; v += step {
			if v == val {
				return true
			}
		}
		return false
	}

	// 处理范围 -
	start, end := min, max
	for i := 0; i < len(rangePart); i++ {
		if rangePart[i] == '-' {
			fmt.Sscanf(rangePart[:i], "%d", &start)
			fmt.Sscanf(rangePart[i+1:], "%d", &end)
			break
		}
	}
	// 单个数字
	if start == min && end == max {
		var n int
		if cnt, _ := fmt.Sscanf(rangePart, "%d", &n); cnt == 1 {
			start, end = n, n
		}
	}

	for v := start; v <= end; v += step {
		if v == val {
			return true
		}
	}
	return false
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

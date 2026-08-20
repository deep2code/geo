package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"my-geo/internal/brand"
	"my-geo/internal/brand/attribution"
	"my-geo/internal/brand/persona"
	"my-geo/internal/brand/promptversion"
	"my-geo/internal/brand/topsource"
	"my-geo/internal/config"
)

// ---------- P1-d：开放测量 API（只读，供 agency/BI 接入） ----------

// handleBrandMeasure 开放测量 API：对品牌做一次审计，返回"对外可消费"的测量快照
// （可见度评分、加权竞品声量、准确性标记、源缺口），不含原始逐条回答。
//
// POST /api/v1/brand/measure
// 需要请求头 X-GEO-API-Key 匹配 GEO_OPENAPI_KEY（未配置则该端点返回 503）。
// 请求体为 brand.BrandProfile。
func (s *Server) handleBrandMeasure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	if !s.checkOpenAPIKey(r) {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Error: "开放测量 API 未启用（请配置 GEO_OPENAPI_KEY 并在请求头携带 X-GEO-API-Key）",
		})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	var profile brand.BrandProfile
	if err := readJSON(r, &profile); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if profile.Name == "" || len(profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name 与 prompts 不能为空"})
		return
	}
	rep, err := s.brandEngine.Audit(r.Context(), profile)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	// 源缺口（基于已审计结果）
	var missingSources []string
	if ts := topsource.Analyze(profile.Name, rep.Results, profile.Domain); ts != nil {
		for _, m := range ts.MissingSources {
			missingSources = append(missingSources, m.Domain)
		}
	}
	// 对外测量快照：只包含聚合指标，剥离原始回答
	measure := map[string]interface{}{
		"brand":                rep.BrandName,
		"generated_at":         rep.GeneratedAt,
		"score":                rep.Score,
		"grade":                rep.Grade,
		"tier":                 rep.Tier,
		"score_breakdown":      rep.ScoreBreakdown,
		"competitor_sov":       rep.CompetitorSOV,
		"weighted_competitor_sov": rep.WeightedCompetitorSOV,
		"persona_breakdown":    rep.PersonaBreakdown,
		"accuracy_flags":       rep.AccuracyFlags,
		"missing_sources":      missingSources,
		"content_gaps_count":   len(rep.ContentGaps),
		"negative_count":       len(rep.NegativeMentions),
	}
	writeJSON(w, http.StatusOK, measure)
}

// checkOpenAPIKey 校验开放 API Key（X-GEO-API-Key 头）。
func (s *Server) checkOpenAPIKey(r *http.Request) bool {
	key := config.Env("GEO_OPENAPI_KEY", "")
	if key == "" {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-GEO-API-Key"), key)
}

// ---------- P0-2：AI 引荐流量 / ROI 归因 ----------

// handleBrandAttribution 计算 AI 引荐流量与 ROI 归因。
//
// POST /api/v1/brand/attribution
// 请求体：
//
//	{ "brand_id":"...", "from":"2026-08-01", "to":"2026-08-31",
//	  "traffic":[ {"date":"...","source":"ga4","sessions":100,"conversions":5,"revenue":2000,"ai_sourced":true} ],
//	  "visibility":{ "2026-08-01": 42.0, ... } }
//
// 返回 attribution.AttributionReport（AI 引荐会话/转化/收入，及可归因于 GEO 的增量）。
func (s *Server) handleBrandAttribution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		BrandID    string                    `json:"brand_id"`
		From       string                    `json:"from"`
		To         string                    `json:"to"`
		Traffic    []attribution.TrafficPoint `json:"traffic"`
		Visibility  map[string]float64       `json:"visibility"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "from 日期格式应为 YYYY-MM-DD"})
		return
	}
	to, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "to 日期格式应为 YYYY-MM-DD"})
		return
	}
	if req.BrandID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand_id 不能为空"})
		return
	}
	tracker := attribution.NewTracker(nil) // 流量由请求体提供，无需额外源
	rep, err := tracker.Compute(r.Context(), req.BrandID, from, to, req.Visibility)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// ---------- P1-c：买家人设分群测量 ----------

// handleBrandPersona 按买家人设分群测量可见度。
//
// POST /api/v1/brand/persona
// 请求体：{ "profile": brand.BrandProfile, "personas": [ persona.Persona ... ] }
// 返回 []persona.Segment（各人设的提及率/情感/位置/内容缺口）。
func (s *Server) handleBrandPersona(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}
	var req struct {
		Profile  brand.BrandProfile `json:"profile"`
		Personas []persona.Persona  `json:"personas"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Profile.Name == "" || len(req.Profile.Prompts) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "profile.name 与 profile.prompts 不能为空"})
		return
	}
	if len(req.Personas) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "personas 不能为空"})
		return
	}
	segs, err := s.brandEngine.PersonaBreakdown(r.Context(), req.Profile, req.Personas)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"segments": segs,
		"count":    len(segs),
	})
}

// ---------- P1-e：Prompt 版本管理 + 实验归因 ----------

// handleBrandPrompt 处理 Prompt 版本/实验归因（内存存储）。
//
// 路由（均 POST/GET，按 path 第二段区分）：
//
//	POST /api/v1/brand/prompt             创建被追踪 prompt
//	POST /api/v1/brand/prompt/version     追加版本
//	GET  /api/v1/brand/prompt/versions    列出版本（?prompt_id=）
//	POST /api/v1/brand/prompt/experiment  保存实验（因果对比）
//	GET  /api/v1/brand/prompt/experiments 列出实验（?prompt_id=）
func (s *Server) handleBrandPrompt(w http.ResponseWriter, r *http.Request) {
	if s.promptStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Prompt 版本存储未初始化"})
		return
	}
	// /versions 或 /experiments 子路由
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/brand/prompt")
	switch {
	case rest == "/versions" && r.Method == http.MethodGet:
		s.handlePromptVersions(w, r)
	case rest == "/experiments" && r.Method == http.MethodGet:
		s.handlePromptExperiments(w, r)
	case rest == "/version" && r.Method == http.MethodPost:
		s.handlePromptAddVersion(w, r)
	case rest == "/experiment" && r.Method == http.MethodPost:
		s.handlePromptAddExperiment(w, r)
	case rest == "" && r.Method == http.MethodPost:
		s.handlePromptCreate(w, r)
	default:
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未知的子路由"})
	}
}

func (s *Server) handlePromptCreate(w http.ResponseWriter, r *http.Request) {
	var p promptversion.TrackedPrompt
	if err := readJSON(r, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if p.ID == "" || p.BrandID == "" || p.Text == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id/brand_id/text 不能为空"})
		return
	}
	if err := s.promptStore.CreatePrompt(r.Context(), &p); err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handlePromptAddVersion(w http.ResponseWriter, r *http.Request) {
	var v promptversion.PromptVersion
	if err := readJSON(r, &v); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if v.PromptID == "" || v.Content == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "prompt_id/content 不能为空"})
		return
	}
	if err := s.promptStore.AddVersion(r.Context(), &v); err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) handlePromptVersions(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("prompt_id")
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 prompt_id"})
		return
	}
	vs, err := s.promptStore.ListVersions(r.Context(), pid)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": vs, "count": len(vs)})
}

func (s *Server) handlePromptAddExperiment(w http.ResponseWriter, r *http.Request) {
	var e promptversion.Experiment
	if err := readJSON(r, &e); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if e.PromptID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "prompt_id 不能为空"})
		return
	}
	if e.StartAt.IsZero() {
		e.StartAt = time.Now()
	}
	if err := s.promptStore.SaveExperiment(r.Context(), &e); err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handlePromptExperiments(w http.ResponseWriter, r *http.Request) {
	pid := r.URL.Query().Get("prompt_id")
	if pid == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 prompt_id"})
		return
	}
	es, err := s.promptStore.ListExperiments(r.Context(), pid)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"experiments": es, "count": len(es)})
}

// ---------- P2-c：预测性策略生成（基于历史趋势外推） ----------

// handleBrandPredict 基于历史审计分数做简单趋势外推，生成未来 N 期预测与策略建议。
//
// GET /api/v1/brand/predict?brand=xxx&horizon=4
// 读取历史库最近记录，对 score 做最小二乘线性拟合，外推 horizon 期（默认 4），
// 并给出"若维持当前斜率，预计何时突破梯队阈值"的建议。
func (s *Server) handleBrandPredict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	brandName := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brand 参数"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	horizon := 4
	if h := r.URL.Query().Get("horizon"); h != "" {
		if n, err := parseIntSafe(h); err == nil && n > 0 && n <= 24 {
			horizon = n
		}
	}
	recs, err := s.brandEngine.HistoryDB().List(r.Context(), brandName, 60, 0)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if len(recs) < 2 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"brand":    brandName,
			"message":  "历史数据不足（少于 2 条），无法预测。请先积累多次审计。",
			"forecast": []float64{},
		})
		return
	}
	// 按时间升序
	sort.Slice(recs, func(i, j int) bool { return recs[i].Generated < recs[j].Generated })
	n := len(recs)
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i, rc := range recs {
		xs[i] = float64(i)
		ys[i] = rc.Score
	}
	slope, intercept := linreg(xs, ys)
	last := ys[n-1]
	forecast := make([]float64, horizon)
	for k := 1; k <= horizon; k++ {
		v := slope*float64(n-1+k) + intercept
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		forecast[k-1] = round1(v)
	}
	// 建议：若斜率为正且当前分 < 70，预计突破 70 的期数
	advice := "维持现状：当前趋势平稳，建议保持现有 GEO 投入节奏。"
	if slope > 0.2 {
		advice = "上升趋势良好：当前斜率约 +" + formatFloat(slope) + "/期，建议加码内容产出以加速爬坡。"
	} else if slope < -0.2 {
		advice = "下降趋势预警：当前斜率约 " + formatFloat(slope) + "/期，建议排查负面源并补强内容。"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"brand":         brandName,
		"history_count": n,
		"last_score":    round1(last),
		"slope":         round2(slope),
		"forecast":      forecast,
		"horizon":       horizon,
		"advice":        advice,
	})
}

// ---------- 小工具 ----------

func linreg(xs, ys []float64) (slope, intercept float64) {
	n := float64(len(xs))
	var sx, sy, sxy, sxx float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxy += xs[i] * ys[i]
		sxx += xs[i] * xs[i]
	}
	denom := n*sxx - sx*sx
	if denom == 0 {
		return 0, sy / n
	}
	slope = (n*sxy - sx*sy) / denom
	intercept = (sy - slope*sx) / n
	return slope, intercept
}

func round1(v float64) float64  { return float64(int(v*10+0.5)) / 10 }
func round2(v float64) float64  { return float64(int(v*100+0.5)) / 100 }

func formatFloat(v float64) string {
	b, _ := json.Marshal(round2(v))
	return string(b)
}

func parseIntSafe(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

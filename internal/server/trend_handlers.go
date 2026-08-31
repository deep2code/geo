package server

import (
	"my-geo/internal/brand/trend"
	"my-geo/internal/models"
	"net/http"
	"sync"
	"time"
)

// 全局趋势追踪器存储（按品牌名索引）。
var (
	trendTrackersMu sync.RWMutex
	trendTrackers   = make(map[string]*trend.Tracker)
)

// getOrCreateTracker 获取或创建品牌趋势追踪器。
func getOrCreateTracker(brandName string) *trend.Tracker {
	trendTrackersMu.Lock()
	defer trendTrackersMu.Unlock()
	if t, ok := trendTrackers[brandName]; ok {
		return t
	}
	t := trend.NewTracker(brandName, 365)
	trendTrackers[brandName] = t
	return t
}

// handleTrendData 获取品牌可见度趋势数据。
//
// GET /api/v1/brand/trend?brand=xxx&days=30
func (s *Server) handleTrendData(w http.ResponseWriter, r *http.Request) {
	brand := r.URL.Query().Get("brand")
	if brand == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 参数不能为空"})
		return
	}
	tracker := getOrCreateTracker(brand)
	series := tracker.GetSeries()
	writeJSON(w, http.StatusOK, series)
}

// handleTrendChart 获取品牌可见度趋势图表数据。
//
// GET /api/v1/brand/trend/chart?brand=xxx
func (s *Server) handleTrendChart(w http.ResponseWriter, r *http.Request) {
	brand := r.URL.Query().Get("brand")
	if brand == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 参数不能为空"})
		return
	}
	tracker := getOrCreateTracker(brand)
	chartData := tracker.ChartData()
	writeJSON(w, http.StatusOK, chartData)
}

// handleTrendRecord 记录一次审计结果到趋势追踪器。
//
// POST /api/v1/brand/trend/record
// 请求体: {"brand": "xxx", "results": [...PromptResult...]}
func (s *Server) handleTrendRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		Brand   string                   `json:"brand"`
		Results []trend.PromptResult     `json:"results"`
		BVSScore float64                 `json:"bvs_score"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Brand == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 不能为空"})
		return
	}
	tracker := getOrCreateTracker(req.Brand)

	// 构建趋势点
	point := trend.TrendPoint{
		Timestamp: time.Now(),
		BVSScore:  req.BVSScore,
	}
	mentioned, cited := 0, 0
	for _, pr := range req.Results {
		if pr.BrandMentioned {
			mentioned++
		}
		if pr.BrandCited {
			cited++
		}
	}
	n := float64(len(req.Results))
	if n > 0 {
		point.MentionRate = float64(mentioned) / n
		point.CitationRate = float64(cited) / n
	}
	tracker.AddPoint(point)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "recorded",
		"brand":   req.Brand,
		"total":   len(tracker.GetSeries().Points),
	})
}

// handleTrendAudit 执行品牌审计并自动记录到趋势追踪器。
//
// POST /api/v1/brand/trend/audit
// 请求体: BrandProfile（与 brand/audit 相同）
func (s *Server) handleTrendAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	// 复用现有审计逻辑
	s.handleBrandAudit(w, r)
}

// handleTrendAlerts 获取品牌趋势异常告警。
//
// GET /api/v1/brand/trend/alerts?brand=xxx
func (s *Server) handleTrendAlerts(w http.ResponseWriter, r *http.Request) {
	brand := r.URL.Query().Get("brand")
	if brand == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 参数不能为空"})
		return
	}
	tracker := getOrCreateTracker(brand)
	series := tracker.GetSeries()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"brand":  brand,
		"alerts": series.Summary.Alerts,
		"total":  len(series.Summary.Alerts),
	})
}

// ensureTrendRoutes 在 server 启动时注册趋势追踪路由（由 registerRoutes 调用）。
func ensureTrendRoutes(s *Server) {
	s.mux.HandleFunc("/api/v1/brand/trend", s.handleTrendData)
	s.mux.HandleFunc("/api/v1/brand/trend/chart", s.handleTrendChart)
	s.mux.HandleFunc("/api/v1/brand/trend/record", s.handleTrendRecord)
	s.mux.HandleFunc("/api/v1/brand/trend/audit", s.handleTrendAudit)
	s.mux.HandleFunc("/api/v1/brand/trend/alerts", s.handleTrendAlerts)
}

// 添加 models 导入支持
var _ = models.EngineType("")

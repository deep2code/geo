package server

import (
	"net/http"
	"strconv"

	"my-geo/internal/brand/sourcestudy"
)

// sourceStudyFromRequest 从请求解析研究过滤条件（query 参数）。
// 支持：engine / brand / category / domain / days / limit。
func sourceStudyFromRequest(r *http.Request) sourcestudy.StudyFilter {
	q := r.URL.Query()
	f := sourcestudy.StudyFilter{
		Engine:    q.Get("engine"),
		BrandName: q.Get("brand"),
		Category:  q.Get("category"),
		Domain:    q.Get("domain"),
	}
	if v := q.Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Days = n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = n
		}
	}
	return f
}

// handleEngineSourcesTop 来源排行：某引擎/品牌下被引用最多的来源域名。
//
// GET /api/v1/admin/engine-sources/top?engine=chatgpt&brand=Acme&days=90&limit=10
func (s *Server) handleEngineSourcesTop(w http.ResponseWriter, r *http.Request) {
	if !s.requireDataAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.SourceStudy() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "引擎来源偏好研究库未启用"})
		return
	}
	f := sourceStudyFromRequest(r)
	stats, err := s.brandEngine.SourceStudy().TopSources(r.Context(), f)
	if err != nil {
		writeInternalError(w, err, "查询来源排行")
		return
	}
	if stats == nil {
		stats = []sourcestudy.SourceStat{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sources": stats})
}

// handleEngineSourcesTrend 来源引用时间趋势：某引擎（可再按来源/品牌）逐日引用次数。
//
// GET /api/v1/admin/engine-sources/trend?engine=chatgpt&domain=zhihu.com&brand=Acme&days=90
func (s *Server) handleEngineSourcesTrend(w http.ResponseWriter, r *http.Request) {
	if !s.requireDataAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.SourceStudy() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "引擎来源偏好研究库未启用"})
		return
	}
	f := sourceStudyFromRequest(r)
	points, err := s.brandEngine.SourceStudy().Trend(r.Context(), f)
	if err != nil {
		writeInternalError(w, err, "查询来源趋势")
		return
	}
	if points == nil {
		points = []sourcestudy.TrendPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"trend": points})
}

// handleEngineSourcesCompare 引擎间来源偏好对比：各引擎 Top 来源并排。
//
// GET /api/v1/admin/engine-sources/compare?brand=Acme&days=90&limit=5
func (s *Server) handleEngineSourcesCompare(w http.ResponseWriter, r *http.Request) {
	if !s.requireDataAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.SourceStudy() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "引擎来源偏好研究库未启用"})
		return
	}
	f := sourceStudyFromRequest(r)
	compare, err := s.brandEngine.SourceStudy().EngineCompare(r.Context(), f)
	if err != nil {
		writeInternalError(w, err, "查询引擎对比")
		return
	}
	if compare == nil {
		compare = []sourcestudy.EngineSource{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"engines": compare})
}

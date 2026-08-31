package server

import (
	"my-geo/internal/brand/competitive"
	"net/http"
	"time"
)

// handleCompetitiveOverview 获取竞品总览分析。
//
// POST /api/v1/brand/competitive/overview
// 请求体: {"brand": "xxx", "results": [...PromptCompetitorResult...]}
func (s *Server) handleCompetitiveOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		Brand   string                               `json:"brand"`
		Results []competitive.PromptCompetitorResult  `json:"results"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if req.Brand == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brand 不能为空"})
		return
	}

	entries := competitive.AggregateSOV(req.Results)
	overview := competitive.CompetitorOverview{
		BrandName:    req.Brand,
		Competitors:  entries,
		TotalPrompts: len(req.Results),
		AnalysisTime: time.Now(),
	}
	writeJSON(w, http.StatusOK, overview)
}

// handleCompetitiveMatrix 获取竞品对比矩阵。
//
// POST /api/v1/brand/competitive/matrix
// 请求体: {"brand": {...CompetitorEntry...}, "competitors": [...CompetitorEntry...]}
func (s *Server) handleCompetitiveMatrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		Brand       competitive.CompetitorEntry   `json:"brand"`
		Competitors []competitive.CompetitorEntry `json:"competitors"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	matrix := competitive.BuildComparisonMatrix(req.Brand, req.Competitors)
	writeJSON(w, http.StatusOK, matrix)
}

// handleCompetitiveEmergence 检测竞品涌现与消失。
//
// POST /api/v1/brand/competitive/emergence
// 请求体: {"current": ["竞品A", "竞品B"], "previous": ["竞品A", "竞品C"]}
func (s *Server) handleCompetitiveEmergence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		Current  []string `json:"current"`
		Previous []string `json:"previous"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	emerged := competitive.DetectEmergence(req.Current, req.Previous)
	gone := competitive.DetectDisappearance(req.Current, req.Previous)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"emerged":     emerged,
		"disappeared": gone,
		"emerged_count": len(emerged),
		"disappeared_count": len(gone),
	})
}

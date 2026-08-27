package server

import (
	"my-geo/internal/models"
	"my-geo/internal/util"
	"net/http"
	"strings"
	"sync"
)

func (s *Server) handleStrategies(w http.ResponseWriter, r *http.Request) {
	infos := s.engine.StrategyInfos()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"strategies": infos,
		"count":      len(infos),
	})
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req analyzeRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
		return
	}
	analysis := s.engine.Analyze(req.Content)
	writeJSON(w, http.StatusOK, analysis)
}

func (s *Server) handleScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req analyzeRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
		return
	}
	score, breakdowns := s.engine.Score(req.Content)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"score":      score,
		"breakdowns": breakdowns,
		"grade":      util.ScoreToGrade(score),
	})
}

func (s *Server) handleOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req models.OptimizationRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "content 不能为空"})
		return
	}
	resp, err := s.engine.Optimize(r.Context(), &req)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── 内容 diff 对比 ──

// diffRequest diff 对比请求。
type diffRequest struct {
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
}

// handleDiff 对比两段文本的差异（优化前后对比）。
//
// POST /api/v1/diff
// 请求体: {"old_content": "...", "new_content": "..."}
// 返回逐行 diff 结果（added/removed/unchanged）+ 统计。
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req diffRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.OldContent) == "" && strings.TrimSpace(req.NewContent) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "old_content 和 new_content 不能同时为空"})
		return
	}
	result := util.ComputeDiff(req.OldContent, req.NewContent)
	writeJSON(w, http.StatusOK, result)
}

// ── 批量优化 ──

// batchOptimizeRequest 批量优化请求。
type batchOptimizeRequest struct {
	Items         []models.OptimizationRequest `json:"items"`
	MaxConcurrent int                          `json:"max_concurrent,omitempty"` // 默认 3
}

// batchOptimizeItem 单条优化结果。
type batchOptimizeItem struct {
	Index   int                          `json:"index"`
	Request models.OptimizationRequest  `json:"-"`
	Result  *models.OptimizationResponse `json:"result,omitempty"`
	Error   string                       `json:"error,omitempty"`
}

// handleBatchOptimize 批量优化多个内容。
//
// POST /api/v1/batch-optimize
// 请求体: {"items": [{content, target_engines, ...}, ...], "max_concurrent": 3}
// 并发执行，返回每条结果（含成功/失败状态）。
func (s *Server) handleBatchOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req batchOptimizeRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if len(req.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "items 不能为空"})
		return
	}
	if len(req.Items) > 20 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "单次最多 20 条"})
		return
	}
	maxConc := req.MaxConcurrent
	if maxConc <= 0 || maxConc > 10 {
		maxConc = 3
	}

	results := make([]batchOptimizeItem, len(req.Items))
	for i := range req.Items {
		results[i] = batchOptimizeItem{Index: i + 1, Request: req.Items[i]}
	}

	// 并发执行优化
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item := &results[idx]
			if strings.TrimSpace(item.Request.Content) == "" {
				item.Error = "content 不能为空"
				return
			}
			resp, err := s.engine.Optimize(r.Context(), &item.Request)
			if err != nil {
				item.Error = err.Error()
				return
			}
			item.Result = resp
		}(i)
	}
	wg.Wait()

	succeeded, failed := 0, 0
	for _, item := range results {
		if item.Error != "" {
			failed++
		} else {
			succeeded++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":     len(results),
		"succeeded": succeeded,
		"failed":    failed,
		"items":     results,
	})
}

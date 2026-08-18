package server

import (
	"my-geo/internal/models"
	"my-geo/internal/util"
	"net/http"
	"strings"
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

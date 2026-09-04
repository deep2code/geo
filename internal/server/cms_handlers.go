package server

import (
	"my-geo/internal/util"
	"net/http"
	"strings"
)

func (s *Server) handleCMSCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req cmsCheckRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(req.HTML) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "html 不能为空"})
		return
	}
	plain := cmsStripHTMLTags(req.HTML)
	if req.Title != "" {
		plain = req.Title + "\n\n" + plain
	}
	analysis := s.engine.Analyze(plain)
	analysis.URL = req.URL
	score, breakdowns := s.engine.Score(plain)
	grade := util.ScoreToGrade(score)
	suggestions := cmsGenerateSuggestions(analysis, score)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"signals": map[string]interface{}{
			"citability_signals": analysis.CitabilitySignals,
			"structure_signals":  analysis.StructureSignals,
			"negative_signals":   analysis.NegativeSignals,
			"evergreen_score":    analysis.EvergreenScore,
			"word_count":         analysis.WordCount,
		},
		"score":       score,
		"grade":       grade,
		"breakdowns":  breakdowns,
		"suggestions": suggestions,
		"ok":          score >= 60,
	})
}

func (s *Server) handleCMSInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":    geoVersion,
		"api_prefix": "/api/v1",
		"endpoints": map[string]string{
			"check": "/api/v1/cms/check",
			"info":  "/api/v1/cms/info",
		},
		"whitelabel": s.whitelabel,
	})
}

// handleSecurityAudit 安全审计接口，返回当前安全中间件的配置与状态。
//
// GET /api/v1/security/audit
// 用于运维快速确认限流、WAF、CSRF、安全头等防护是否生效。
func (s *Server) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireDataAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rate_limit": map[string]interface{}{
			"global_per_sec":    rlConfig.globalPerSec,
			"expensive_per_sec": rlConfig.expensivePerSec,
			"expensive_paths":   expensivePathPatterns,
		},
		"waf": map[string]interface{}{
			"max_body_bytes":     defaultMaxBodyBytes,
			"max_body_expensive": 20 * 1024 * 1024,
			"checks":             []string{"sqli", "xss", "path_traversal", "null_byte"},
			"security_headers":   []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "X-XSS-Protection", "Permissions-Policy", "Content-Security-Policy"},
		},
		"csrf": map[string]interface{}{
			"enabled":       corsOrigins() != nil,
			"write_methods": []string{"POST", "PUT", "PATCH", "DELETE"},
		},
		"auth": map[string]interface{}{
			"auth_enabled": s.authSvc != nil && s.authSvc.Enabled(),
		},
		"recovery": map[string]interface{}{
			"panic_recovery": true,
		},
		"fallback_cache": map[string]interface{}{
			"enabled":  s.brandEngine != nil,
			"ttl":      "1h",
			"max_size": 1000,
		},
	})
}

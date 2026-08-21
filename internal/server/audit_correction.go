package server

import (
	"net/http"

	"my-geo/internal/auth"
	"my-geo/internal/brand"
)

// handleAuditCorrection 人工修正单条判定并原地重算报告（管理后台）。
//
// POST /api/v1/admin/audit/correction
// 请求体: brand.CorrectResultInput（record_id / brand_name / index / 待修正字段 / reason）
//
// 权限：账号体系启用时要求 PermManageData（Owner/Admin，见 requireDataAdmin）；
// legacy GEO_API_KEY 模式中 API Key 鉴权已通过即为全权。
//
// 返回：重算后的完整 VisibilityReport（前端直接替换展示）。
func (s *Server) handleAuditCorrection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	if !s.requireDataAdmin(w, r) {
		return
	}
	if s.brandEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "品牌审计引擎未初始化"})
		return
	}

	var in brand.CorrectResultInput
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "解析失败: " + err.Error()})
		return
	}
	// 修正人优先取 JWT 用户邮箱（审计留痕更可信）；无账号体系时用请求体值或默认。
	if u := auth.UserFromContext(r.Context()); u != nil && u.Email != "" {
		in.CorrectedBy = u.Email
	}

	report, err := s.brandEngine.CorrectResult(r.Context(), in)
	if err != nil {
		// 业务校验错误（品牌不匹配/下标越界/参数非法）返回 400，系统错误返回 500。
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

package server

import (
	"net/http"
	"sort"
	"strings"

	"my-geo/internal/config"
)

// 系统设置管理接口（管理后台「系统设置」页）。
//
// 鉴权：与其它 /api/admin/* 一致，要求 X-Admin-Key 匹配 GEO_ADMIN_KEY
// （DB 覆盖优先，见 config.Env）。
//
//	GET  /api/v1/admin/settings?category=&q=   列出全部可管理配置（secret 脱敏）
//	PUT  /api/v1/admin/settings                更新单个配置（body: {"key","value"}）
//	POST /api/v1/admin/settings/reset          恢复默认（body: {"key"}）

// handleAdminSettingsList 列出配置项（支持分类过滤与关键字搜索）。
func (s *Server) handleAdminSettingsList(w http.ResponseWriter, r *http.Request) {
	// /api/v1/admin/settings 同时承载「GET 列出」与「PUT 更新」（文件头注释承诺）。
	// PUT 委托给 handleAdminSettingsUpdate，避免重复实现；该处理函数此前漏接路由，
	// 导致管理后台「保存设置」实际返回 405。此处补上接线，消除死代码并修复更新功能。
	if r.Method == http.MethodPut {
		s.handleAdminSettingsUpdate(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET / PUT"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	all := config.ListSettings()
	catCount := map[string]int{}
	items := make([]config.Setting, 0, len(all))
	for _, st := range all {
		catCount[st.Category]++
		if category != "" && st.Category != category {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(st.Key), q) &&
			!strings.Contains(strings.ToLower(st.Description), q) {
			continue
		}
		// secret 脱敏：只返回掩码，真实值仅通过运行日志/DB 可见
		if st.IsSecret {
			st.Value = maskSecret(st.Value)
		}
		items = append(items, st)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Key < items[j].Key
	})
	cats := make([]map[string]any, 0, len(catCount))
	for name, n := range catCount {
		cats = append(cats, map[string]any{"name": name, "count": n})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i]["name"].(string) < cats[j]["name"].(string) })
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":   items,
		"categories": cats,
		"total":      len(items),
	})
}

// handleAdminSettingsUpdate 更新单个配置项。
//
// body: {"key":"GEO_LLM_BUDGET_USD","value":"50"}
// secret 类配置传空串或 "********" 表示"保持不变"。
// 返回是否需重启生效（requires_restart）。
func (s *Server) handleAdminSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 PUT"})
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "key 不能为空"})
		return
	}
	// secret 占位符 = 不修改（保持当前值）
	if strings.TrimSpace(req.Value) == "" || strings.TrimSpace(req.Value) == "********" {
		writeJSON(w, http.StatusOK, map[string]any{"key": req.Key, "unchanged": true})
		return
	}
	restart, err := config.UpdateSetting(r.Context(), req.Key, req.Value)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":               req.Key,
		"updated":           true,
		"restart_required":  restart,
	})
}

// handleAdminSettingsReset 恢复某配置项为默认值。
//
// body: {"key":"GEO_LLM_BUDGET_USD"}
func (s *Server) handleAdminSettingsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "key 不能为空"})
		return
	}
	if err := config.ResetSetting(r.Context(), req.Key); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": req.Key, "reset": true})
}

// maskSecret 掩码敏感值：保留首 2 与末 4 字符，其余用 • 填充。
func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	runes := []rune(v)
	n := len(runes)
	if n <= 8 {
		return "••••"
	}
	return string(runes[:2]) + "••••••" + string(runes[n-4:])
}

package server

import (
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"my-geo/internal/httputil"
)

// serverStartTime 服务启动时间（包初始化时记录，用于系统信息展示）。
var serverStartTime = time.Now()

// TenantInfo 租户信息。
type TenantInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Plan        string    `json:"plan"`         // free/pro/enterprise
	Status      string    `json:"status"`       // active/suspended/expired
	Brands      int       `json:"brands"`       // 品牌数
	Audits      int       `json:"audits"`       // 审计次数
	Emails      int       `json:"emails"`       // 邮件发送量
	StorageUsed int64     `json:"storage_used"` // 存储使用量 bytes
	CreatedAt   time.Time `json:"created_at"`
	LastActive  time.Time `json:"last_active"`
}

// Announcement 公告。
type Announcement struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Type      string     `json:"type"` // info/warning/maintenance
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// 公告内存存储（进程重启清空，可接受）。
var (
	announcementsMu sync.Mutex
	announcements   []Announcement
	announcementSeq int64
	tenantStatusMu  sync.Mutex
	tenantStatusMap = map[string]string{} // 租户 ID → 状态（覆盖模拟默认值）
)

// checkAdminKey 校验管理员请求头。
// ⚠️ 未配置 GEO_ADMIN_KEY 时拒绝访问（生产默认安全）。
// 开发/本地 Demo 需要管理员接口时，务必显式配置：
//
//	export GEO_ADMIN_KEY="$(openssl rand -hex 16)"
func (s *Server) checkAdminKey(r *http.Request) bool {
	key := strings.TrimSpace(os.Getenv("GEO_ADMIN_KEY"))
	if key == "" {
		// 只在启动阶段打一次 warning 即可，这里避免每请求刷日志
		return false
	}
	// 恒定时间比较（避免时序攻击）
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Key")), []byte(key)) == 1
}

// adminForbidden 返回未授权错误。
func (s *Server) adminForbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, ErrorResponse{
		Error: "管理员鉴权失败：X-Admin-Key 不匹配",
		Code:  "ADMIN_FORBIDDEN",
	})
}

// hashToInt 将字符串确定性映射为非负整数（用于生成稳定的模拟数据）。
func hashToInt(s string, mod int) int {
	h := sha1.Sum([]byte(s))
	v := binary.BigEndian.Uint64(h[:8])
	if mod <= 0 {
		return int(v)
	}
	return int(v % uint64(mod))
}

// buildMockTenants 基于 HistoryDB 中的品牌数据生成模拟租户信息。
func (s *Server) buildMockTenants(ctx context.Context) []TenantInfo {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		return []TenantInfo{}
	}
	brands, _ := s.brandEngine.HistoryDB().Brands(ctx)
	slices.Sort(brands)
	plans := []string{"free", "pro", "enterprise"}
	statuses := []string{"active", "active", "active", "suspended", "expired"}
	now := time.Now()
	tenants := make([]TenantInfo, 0, len(brands))
	for _, name := range brands {
		plan := plans[hashToInt(name+"plan", len(plans))]
		// 默认状态：大部分 active，少数 suspended/expired
		status := statuses[hashToInt(name+"status", len(statuses))]
		// 允许被管理员覆盖
		tenantStatusMu.Lock()
		if override, ok := tenantStatusMap[name]; ok {
			status = override
		}
		tenantStatusMu.Unlock()
		audits := hashToInt(name+"audits", 50) + 1
		emails := hashToInt(name+"emails", 200)
		storage := int64(hashToInt(name+"storage", 500)+1) * 1024 * 1024
		createdDays := hashToInt(name+"created", 180)
		activeMins := hashToInt(name+"active", 60*24*7)
		tenants = append(tenants, TenantInfo{
			ID:          "tnt-" + name,
			Name:        name,
			Plan:        plan,
			Status:      status,
			Brands:      hashToInt(name+"brands", 5) + 1,
			Audits:      audits,
			Emails:      emails,
			StorageUsed: storage,
			CreatedAt:   now.AddDate(0, 0, -createdDays),
			LastActive:  now.Add(-time.Duration(activeMins) * time.Minute),
		})
	}
	return tenants
}

// nilCtx 返回一个 background context（用于无 request 关联的内部调用）。
func nilCtx() context.Context {
	return context.Background()
}

// handleAdminTenants 租户列表（支持 ?status=&plan= 过滤，?page=&limit= 分页）。
// GET /api/v1/admin/tenants
func (s *Server) handleAdminTenants(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	planFilter := strings.TrimSpace(r.URL.Query().Get("plan"))
	page, limit := httputil.PageLimit(r, 20, 100)

	tenants := s.buildMockTenants(r.Context())
	// 过滤
	filtered := tenants[:0]
	for _, t := range tenants {
		if statusFilter != "" && t.Status != statusFilter {
			continue
		}
		if planFilter != "" && t.Plan != planFilter {
			continue
		}
		filtered = append(filtered, t)
	}
	total := len(filtered)
	// 分页
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	var pageItems []TenantInfo
	if start < end {
		pageItems = filtered[start:end]
	} else {
		pageItems = []TenantInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total":   total,
		"page":    page,
		"limit":   limit,
		"tenants": pageItems,
	})
}

// handleAdminTenantDetail 租户详情 / 更新状态。
// GET    /api/v1/admin/tenants/{id}
// PUT    /api/v1/admin/tenants/{id}/status
func (s *Server) handleAdminTenantDetail(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/tenants/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少租户 ID"})
		return
	}
	// 区分 /{id} 与 /{id}/status
	parts := strings.SplitN(rest, "/", 2)
	tenantID := parts[0]
	isStatus := len(parts) == 2 && parts[1] == "status"

	if isStatus {
		// 更新租户状态
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 PUT"})
			return
		}
		var body struct {
			Status string `json:"status"` // suspend/activate
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		var newStatus string
		switch strings.ToLower(strings.TrimSpace(body.Status)) {
		case "suspend":
			newStatus = "suspended"
		case "activate":
			newStatus = "active"
		default:
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: "status 仅支持 suspend/activate",
				Code:  "INVALID_STATUS",
			})
			return
		}
		// 提取租户名（去掉 tnt- 前缀）
		tenantName := strings.TrimPrefix(tenantID, "tnt-")
		tenantStatusMu.Lock()
		tenantStatusMap[tenantName] = newStatus
		tenantStatusMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":     true,
			"id":     tenantID,
			"status": newStatus,
		})
		return
	}

	// 租户详情
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	tenants := s.buildMockTenants(r.Context())
	for _, t := range tenants {
		if t.ID == tenantID {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "租户不存在"})
}

// handleAdminUsage 全局用量统计。
// GET /api/v1/admin/usage
func (s *Server) handleAdminUsage(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	var (
		totalBrands   int64
		totalAudits   int64
		totalEmails   int64
		activeTenants int
	)
	tenants := s.buildMockTenants(r.Context())
	for _, t := range tenants {
		totalBrands += int64(t.Brands)
		totalAudits += int64(t.Audits)
		totalEmails += int64(t.Emails)
		if t.Status == "active" {
			activeTenants++
		}
	}
	// 从 HistoryDB 获取真实统计数据覆盖
	if s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		if st, err := s.brandEngine.HistoryDB().Stats(r.Context()); err == nil {
			totalBrands = st.Brands
			totalAudits = st.Records
		}
	}
	resp := map[string]interface{}{
		"total_brands":    totalBrands,
		"total_audits":    totalAudits,
		"total_emails":    totalEmails,
		"active_tenants":  activeTenants,
		"total_tenants":   len(tenants),
		"api_calls_today": hashToInt(fmt.Sprintf("%d", time.Now().Unix()/86400), 10000) + 500, // 模拟当日 API 调用量
	}
	// 从 ROI 追踪器获取真实 token 用量与成本
	if s.brandEngine != nil && s.brandEngine.ROITracker() != nil {
		roiReport := s.brandEngine.ROITracker().Report("all")
		resp["token_usage"] = map[string]interface{}{
			"total_calls":       roiReport.TotalCalls,
			"total_tokens":      roiReport.TotalTokens,
			"prompt_tokens":     roiReport.PromptTokens,
			"completion_tokens": roiReport.CompletionTokens,
			"total_cost_usd":    roiReport.TotalCost,
			"by_engine":         roiReport.ByEngine,
			"by_operation":      roiReport.ByOperation,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminAnnouncements 公告列表 / 创建公告。
// GET  /api/v1/admin/announcements
// POST /api/v1/admin/announcements
func (s *Server) handleAdminAnnouncements(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		announcementsMu.Lock()
		list := make([]Announcement, len(announcements))
		copy(list, announcements)
		announcementsMu.Unlock()
		// 倒序：最新的在前
		slices.SortFunc(list, func(a, b Announcement) int { return b.CreatedAt.Compare(a.CreatedAt) })
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"total":         len(list),
			"announcements": list,
		})
	case http.MethodPost:
		var body struct {
			Title     string     `json:"title"`
			Content   string     `json:"content"`
			Type      string     `json:"type"`
			Active    bool       `json:"active"`
			ExpiresAt *time.Time `json:"expires_at"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		if strings.TrimSpace(body.Title) == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "title 不能为空"})
			return
		}
		// 校验类型
		switch body.Type {
		case "info", "warning", "maintenance", "":
			if body.Type == "" {
				body.Type = "info"
			}
		default:
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: "type 仅支持 info/warning/maintenance",
				Code:  "INVALID_TYPE",
			})
			return
		}
		announcementsMu.Lock()
		announcementSeq++
		a := Announcement{
			ID:        fmt.Sprintf("ann-%04d", announcementSeq),
			Title:     body.Title,
			Content:   body.Content,
			Type:      body.Type,
			Active:    body.Active,
			CreatedAt: time.Now(),
			ExpiresAt: body.ExpiresAt,
		}
		announcements = append(announcements, a)
		announcementsMu.Unlock()
		writeJSON(w, http.StatusCreated, a)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET/POST"})
	}
}

// handleAdminAnnouncementDelete 删除公告。
// DELETE /api/v1/admin/announcements/{id}
func (s *Server) handleAdminAnnouncementDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/announcements/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少公告 ID"})
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 DELETE"})
		return
	}
	announcementsMu.Lock()
	defer announcementsMu.Unlock()
	for i, a := range announcements {
		if a.ID == id {
			announcements = append(announcements[:i], announcements[i+1:]...)
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": id})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "公告不存在"})
}

// handleAdminSystem 系统信息。
// GET /api/v1/admin/system
func (s *Server) handleAdminSystem(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminKey(r) {
		s.adminForbidden(w)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	uptime := time.Since(serverStartTime)
	// 磁盘使用：优先返回 HistoryDB 文件大小
	var diskUsed int64
	if s.brandEngine != nil && s.brandEngine.HistoryDB() != nil {
		if st, err := s.brandEngine.HistoryDB().Stats(r.Context()); err == nil {
			diskUsed = st.FileSize
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"go_version":     runtime.Version(),
		"goroutines":     runtime.NumGoroutine(),
		"uptime_seconds": int64(uptime.Seconds()),
		"uptime":         uptime.String(),
		"started_at":     serverStartTime,
		"memory": map[string]interface{}{
			"alloc_bytes":       ms.Alloc,
			"total_alloc_bytes": ms.TotalAlloc,
			"sys_bytes":         ms.Sys,
			"heap_objects":      ms.HeapObjects,
			"gc_count":          ms.NumGC,
		},
		"disk_used_bytes": diskUsed,
		"num_cpu":         runtime.NumCPU(),
		"version":         geoVersion,
	})
}

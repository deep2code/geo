// Package auth HTTP handler：注册/登录/刷新/登出/切换工作区/改密码/审计日志。
package auth

import (
	"log/slog"
	"net/http"
	"time"

	"my-geo/internal/httputil"
)

// writeInternalError 内部错误：日志记录详情，响应仅给通用信息（防泄露 DSN/SQL/路径等内部细节）。
func writeInternalError(w http.ResponseWriter, action string, err error) {
	slog.Error("auth 内部错误", slog.String("action", action), slog.String("err", err.Error()))
	writeJSON(w, http.StatusInternalServerError, AuthNResponse{Error: "内部错误，请稍后重试", Code: "INTERNAL"})
}

// ============================================================
// 请求 / 响应结构
// ============================================================

type registerRequest struct {
	Email         string `json:"email"`
	Password      string `json:"password"`
	DisplayName   string `json:"display_name"`
	WorkspaceName string `json:"workspace_name"`
	InviteCode    string `json:"invite_code,omitempty"` // 后续拓展
}

type loginRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	// 预留：Remember 保持更久登录（后续延长 refresh）
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type switchWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type loginResponse struct {
	Tokens     TokenPair           `json:"tokens"`
	User       *User               `json:"user"`
	Workspaces []WorkspaceWithRole `json:"workspaces"`
}

// ============================================================
// 辅助函数
// ============================================================

func writeJSON(w http.ResponseWriter, status int, data any) {
	httputil.WriteJSON(w, status, data)
}

func readJSON(r *http.Request, v any) error {
	return httputil.ReadJSON(r, v)
}

// ---- 可信代理与真实客户端 IP ----
//
// 与 server 中间件策略一致（实现见 httputil）：仅当 RemoteAddr 属于
// GEO_TRUSTED_PROXIES 时才解析 X-Forwarded-For/X-Real-IP，避免这些头被
// 任意客户端伪造绕过审计/限流。

// requestIP 获取真实客户端 IP（仅信任来自可信代理的转发头）。
func requestIP(r *http.Request) string { return httputil.ClientIP(r) }

func requestUA(r *http.Request) string {
	return r.Header.Get("User-Agent")
}

// ============================================================
// Handler 构造器（将 Service 绑定为 http.HandlerFunc 集合）
// ============================================================

// HandlerSet 注册到 mux 的各个 endpoint 处理器。
type HandlerSet struct {
	Svc *Service
	// FirstUserAllowInviteOnly：为 true 时首用户也需 invite code（私有部署可选）
	FirstUserInviteOnly bool
}

// NewHandlerSet 构建 handler 集。
func NewHandlerSet(svc *Service) HandlerSet { return HandlerSet{Svc: svc} }

// ---- 注册 ----

func (h HandlerSet) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST", Code: "METHOD_NOT_ALLOWED"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用（请设置 GEO_AUTH_ENABLED=true）", Code: "AUTH_DISABLED"})
		return
	}
	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error(), Code: "BAD_REQUEST"})
		return
	}
	// 非首用户注册：暂不开放（后续走邀请码或管理员邀请流程）。
	// 首用户直接创建。
	hasUsers, err := h.Svc.Store().HasUsers()
	if err != nil {
		writeInternalError(w, "register.has_users", err)
		return
	}
	if hasUsers {
		writeJSON(w, http.StatusForbidden, AuthNResponse{
			Error: "当前实例已有用户，普通注册通道已关闭。请联系工作区管理员邀请，或通过 invite_code 注册。",
			Code:  "REGISTRATION_CLOSED",
		})
		return
	}
	u, ws, err := h.Svc.Store().CreateUser(req.Email, req.Password, req.DisplayName, req.WorkspaceName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: err.Error(), Code: "REGISTER_FAILED"})
		return
	}
	// 留痕
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: u.ID, Actor: u.Email, Action: "user.register", Target: u.Email,
		Details: map[string]string{"workspace_id": ws.ID, "workspace_name": ws.Name},
		IP:      requestIP(r), UserAgent: requestUA(r),
	})
	wss, _ := h.Svc.Store().ListWorkspacesWithRole(u.ID)
	pair, err := h.Svc.issueTokenPair(u, ws.ID, RoleOwner)
	if err != nil {
		writeInternalError(w, "register.issue_token", err)
		return
	}
	_ = h.Svc.Store().UpdateLastLogin(u.ID)
	writeJSON(w, http.StatusOK, loginResponse{Tokens: *pair, User: u, Workspaces: wss})
}

// ---- 登录 ----

func (h HandlerSet) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST", Code: "METHOD_NOT_ALLOWED"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用", Code: "AUTH_DISABLED"})
		return
	}
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error(), Code: "BAD_REQUEST"})
		return
	}
	pair, u, wss, err := h.Svc.Login(req.Email, req.Password, req.WorkspaceID, requestIP(r), requestUA(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, AuthNResponse{Error: err.Error(), Code: "LOGIN_FAILED"})
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Tokens: *pair, User: u, Workspaces: wss})
}

// ---- 刷新 ----

func (h HandlerSet) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	var req refreshRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error()})
		return
	}
	pair, u, err := h.Svc.Refresh(req.RefreshToken, requestIP(r), requestUA(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, AuthNResponse{Error: err.Error(), Code: "REFRESH_FAILED"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tokens": pair,
		"user":   u,
	})
}

// ---- 登出（吊销 refresh token） ----

func (h HandlerSet) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	u := UserFromContext(r.Context())
	var req logoutRequest
	_ = readJSON(r, &req) // body 可能为空，优先 context 取用户
	if u != nil && req.RefreshToken != "" {
		_ = h.Svc.Logout(req.RefreshToken, u.ID)
		_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
			ActorID: u.ID, Actor: u.Email, Action: "user.logout", Target: u.Email,
			IP: requestIP(r), UserAgent: requestUA(r),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- /me：当前用户 + 工作区列表 ----

func (h HandlerSet) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 GET"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	u := UserFromContext(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, AuthNResponse{Error: "未登录", Code: "AUTH_REQUIRED"})
		return
	}
	wss, err := h.Svc.Store().ListWorkspacesWithRole(u.ID)
	if err != nil {
		writeInternalError(w, "me.list_workspaces", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":         u,
		"workspaces":   wss,
		"workspace_id": WorkspaceIDFromContext(r.Context()),
		"role":         RoleFromContext(r.Context()),
	})
}

// ---- 切换工作区 ----

func (h HandlerSet) SwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	u := UserFromContext(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, AuthNResponse{Error: "未登录"})
		return
	}
	var req switchWorkspaceRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error()})
		return
	}
	pair, err := h.Svc.SwitchWorkspace(u.ID, req.WorkspaceID)
	if err != nil {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: err.Error(), Code: "WORKSPACE_FORBIDDEN"})
		return
	}
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: u.ID, Actor: u.Email, Action: "user.switch_workspace", Target: req.WorkspaceID,
		IP: requestIP(r), UserAgent: requestUA(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"tokens": pair})
}

// ---- 改密码 ----

func (h HandlerSet) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	u := UserFromContext(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, AuthNResponse{Error: "未登录"})
		return
	}
	var req changePasswordRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error()})
		return
	}
	_, ok, _ := h.Svc.Store().VerifyPassword(u.Email, req.OldPassword)
	if !ok {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "原密码错误", Code: "OLD_PASSWORD_WRONG"})
		return
	}
	if err := h.Svc.Store().UpdatePassword(u.ID, req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: err.Error(), Code: "PASSWORD_UPDATE_FAILED"})
		return
	}
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: u.ID, Actor: u.Email, Action: "user.change_password", Target: u.Email,
		IP: requestIP(r), UserAgent: requestUA(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 工作区成员管理（仅 Admin/Owner 可用） ----

type addMemberRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

func (h HandlerSet) AddMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	if err := RequirePermission(r.Context(), PermManageMember); err != nil {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: err.Error(), Code: "PERMISSION_DENIED"})
		return
	}
	var req addMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败: " + err.Error()})
		return
	}
	if req.Role == "" {
		req.Role = RoleMember
	}
	if roleGt(req.Role) == 0 {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "角色非法（owner/admin/member/viewer）", Code: "ROLE_INVALID"})
		return
	}
	// 不能加人超过自己的角色
	actorRole := RoleFromContext(r.Context())
	if !RoleGte(actorRole, req.Role) {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "不能邀请高于自身角色的成员", Code: "ROLE_HIERARCHY_VIOLATION"})
		return
	}
	wid := WorkspaceIDFromContext(r.Context())
	if wid == "" {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "未选择工作区"})
		return
	}
	// 查找目标用户（不存在则后续走邀请邮件流程；当前版本需先注册）
	target, err := h.Svc.Store().GetUserByEmail(req.Email)
	if err != nil {
		writeInternalError(w, "member.get_user", err)
		return
	}
	if target == nil {
		writeJSON(w, http.StatusNotFound, AuthNResponse{
			Error: "该邮箱用户不存在，请先邀请其注册后再加入",
			Code:  "USER_NOT_FOUND",
		})
		return
	}
	now := time.Now().Unix()
	_, e := h.Svc.Store().db.Exec(
		`INSERT IGNORE INTO memberships(user_id,workspace_id,role,joined_at) VALUES(?,?,?,?)`,
		target.ID, wid, string(req.Role), now,
	)
	if e != nil {
		writeInternalError(w, "member.add", e)
		return
	}
	actor := UserFromContext(r.Context())
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: actor.ID, Actor: actor.Email, Action: "workspace.add_member", Target: req.Email,
		Details: map[string]string{"workspace_id": wid, "role": string(req.Role)},
		IP:      requestIP(r), UserAgent: requestUA(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "user_id": target.ID, "role": req.Role,
	})
}

type changeRoleRequest struct {
	UserID string `json:"user_id"`
	Role   Role   `json:"role"`
}

func (h HandlerSet) ChangeRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	if err := RequirePermission(r.Context(), PermManageMember); err != nil {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: err.Error()})
		return
	}
	var req changeRoleRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败"})
		return
	}
	if roleGt(req.Role) == 0 {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "角色非法"})
		return
	}
	actorRole := RoleFromContext(r.Context())
	wid := WorkspaceIDFromContext(r.Context())
	// Owner 不可被降级（除 Owner 自己外）
	existing, err := h.Svc.Store().GetUserRoleInWorkspace(req.UserID, wid)
	if err != nil || existing == "" {
		writeJSON(w, http.StatusNotFound, AuthNResponse{Error: "该用户不在此工作区", Code: "MEMBER_NOT_FOUND"})
		return
	}
	if existing == RoleOwner && actorRole != RoleOwner {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "仅 Owner 可修改 Owner 角色"})
		return
	}
	if !RoleGte(actorRole, req.Role) {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "不能授予高于自身的角色"})
		return
	}
	if _, e := h.Svc.Store().db.Exec(
		"UPDATE memberships SET role=? WHERE user_id=? AND workspace_id=?",
		string(req.Role), req.UserID, wid,
	); e != nil {
		writeInternalError(w, "member.change_role", e)
		return
	}
	actor := UserFromContext(r.Context())
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: actor.ID, Actor: actor.Email, Action: "workspace.change_role", Target: req.UserID,
		Details: map[string]string{"workspace_id": wid, "old_role": string(existing), "new_role": string(req.Role)},
		IP:      requestIP(r), UserAgent: requestUA(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type removeMemberRequest struct{ UserID string }

func (h HandlerSet) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 POST"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	if err := RequirePermission(r.Context(), PermManageMember); err != nil {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: err.Error()})
		return
	}
	var req removeMemberRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, AuthNResponse{Error: "参数解析失败"})
		return
	}
	actorRole := RoleFromContext(r.Context())
	wid := WorkspaceIDFromContext(r.Context())
	targetRole, err := h.Svc.Store().GetUserRoleInWorkspace(req.UserID, wid)
	if err != nil || targetRole == "" {
		writeJSON(w, http.StatusNotFound, AuthNResponse{Error: "该用户不在此工作区"})
		return
	}
	if targetRole == RoleOwner {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "不能移除工作区 Owner"})
		return
	}
	if !RoleGte(actorRole, targetRole) {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: "不能移除更高角色的成员"})
		return
	}
	if _, e := h.Svc.Store().db.Exec(
		"DELETE FROM memberships WHERE user_id=? AND workspace_id=?",
		req.UserID, wid,
	); e != nil {
		writeInternalError(w, "member.remove", e)
		return
	}
	actor := UserFromContext(r.Context())
	_ = h.Svc.Store().AppendAuditLog(&AdminAuditLog{
		ActorID: actor.ID, Actor: actor.Email, Action: "workspace.remove_member", Target: req.UserID,
		Details: map[string]string{"workspace_id": wid},
		IP:      requestIP(r), UserAgent: requestUA(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- 审计日志查询（平台级，需 PermManageAdmin；工作区级后续可扩展） ----

func (h HandlerSet) AdminAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, AuthNResponse{Error: "仅支持 GET"})
		return
	}
	if h.Svc == nil || !h.Svc.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, AuthNResponse{Error: "账号体系未启用"})
		return
	}
	if err := RequirePermission(r.Context(), PermManageAdmin); err != nil {
		writeJSON(w, http.StatusForbidden, AuthNResponse{Error: err.Error(), Code: "PERMISSION_DENIED"})
		return
	}
	q := r.URL.Query()
	action := q.Get("action")
	limit := 100
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	logs, err := h.Svc.Store().QueryAuditLog(action, limit, offset)
	if err != nil {
		writeInternalError(w, "audit_log.query", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "count": len(logs), "limit": limit, "offset": offset})
}

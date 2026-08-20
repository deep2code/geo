package billing

import (
	"io"
	"log/slog"
	"net/http"

	"my-geo/internal/auth"
	"my-geo/internal/billing/payment"
	"my-geo/internal/httputil"
)

// HandlerSet 计费 HTTP 接口集。
type HandlerSet struct {
	Svc     *Service
	AuthSvc *auth.Service // 用于解析当前工作区与角色（可为 nil：未启用账号体系）
}

// NewHandlerSet 构建 handler 集。
func NewHandlerSet(svc *Service, authSvc *auth.Service) HandlerSet {
	return HandlerSet{Svc: svc, AuthSvc: authSvc}
}

// ---- 公共：套餐目录 ----

// HandlePlans GET /api/v1/billing/plans
func (h HandlerSet) HandlePlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if h.Svc == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "计费未启用"})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"plans":           h.Svc.Plans(),
		"payment_methods": h.Svc.PaymentMethods(),
	})
}

// HandlePaymentMethods GET /api/v1/billing/payment-methods
// 返回全部可用支付方式（微信/支付宝/Stripe + 手动激活），并存、互不替代。
// 在线渠道按凭据齐备情况标注 configured，前端据此渲染亮/灰按钮。
func (h HandlerSet) HandlePaymentMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	if h.Svc == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "计费未启用"})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"methods": h.Svc.PaymentMethods(),
	})
}

// ---- 需鉴权：当前工作区订阅 / 用量 ----

// currentWorkspace 从请求上下文解析当前工作区；未启用账号体系返回 ("", false)。
func (h HandlerSet) currentWorkspace(r *http.Request) (string, bool) {
	if h.AuthSvc == nil || !h.AuthSvc.Enabled() {
		return "", false
	}
	return auth.WorkspaceIDFromContext(r.Context()), true
}

// requireWorkspace 要求已鉴权且能解析工作区，否则写错误并返回 false。
func (h HandlerSet) requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	wsID, ok := h.currentWorkspace(r)
	if !ok || wsID == "" {
		httputil.WriteJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "未鉴权或无工作区上下文（请先登录并设置 GEO_AUTH_ENABLED=true）",
		})
		return "", false
	}
	return wsID, true
}

// HandleSubscription GET /api/v1/billing/subscription
func (h HandlerSet) HandleSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}
	sub, err := h.Svc.Subscription(r.Context(), wsID)
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	plan := h.Svc.PlanOf(sub)
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"subscription": sub,
		"plan":         plan,
	})
}

// HandleUsage GET /api/v1/billing/usage
func (h HandlerSet) HandleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}
	usage, err := h.Svc.Usage(r.Context(), wsID)
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"usage": usage})
}

// ---- 手动激活（轻量版核心） ----

type activateRequest struct {
	Plan        string `json:"plan"`
	WorkspaceID string `json:"workspace_id,omitempty"` // 缺省激活自己的工作区
	PeriodDays  int    `json:"period_days,omitempty"`
}

// HandleActivate POST /api/v1/billing/subscription/activate
// 管理员/拥有者手动激活或变更套餐（无需支付渠道）。轻量版默认路径。
func (h HandlerSet) HandleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}
	// 角色校验：仅 owner / admin 可激活。
	if !h.hasRole(r, auth.RoleOwner, auth.RoleAdmin) {
		httputil.WriteJSON(w, http.StatusForbidden, map[string]string{
			"error": "仅工作区拥有者或管理员可激活/变更套餐",
		})
		return
	}
	var req activateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Plan == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "plan 不能为空"})
		return
	}
	// 指定他人工作区时，仅允许平台管理员（owner 激活自身；admin 管理成员）。
	target := wsID
	if req.WorkspaceID != "" && req.WorkspaceID != wsID {
		if !h.hasRole(r, auth.RoleOwner) {
			httputil.WriteJSON(w, http.StatusForbidden, map[string]string{
				"error": "仅工作区拥有者可代激活其他工作区",
			})
			return
		}
		target = req.WorkspaceID
	}
	uid := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		uid = u.ID
	}
	if err := h.Svc.Activate(r.Context(), target, PlanID(req.Plan), uid, req.PeriodDays); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"workspace_id": target,
		"plan":         req.Plan,
	})
}

// hasRole 当前用户是否具备给定角色之一（基于 JWT 中的工作区角色）。
func (h HandlerSet) hasRole(r *http.Request, roles ...auth.Role) bool {
	cur := auth.RoleFromContext(r.Context())
	for _, role := range roles {
		if cur == role {
			return true
		}
	}
	return false
}

// ---- 订单 ----

type orderRequest struct {
	Plan     string `json:"plan"`
	Provider string `json:"provider,omitempty"` // wechatpay / alipay / stripe / manual；付费套餐必填其一
}

// HandleCreateOrder POST /api/v1/billing/orders
func (h HandlerSet) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	wsID, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}
	var req orderRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Plan == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "plan 不能为空"})
		return
	}
	res, err := h.Svc.CreateOrder(r.Context(), wsID, PlanID(req.Plan), req.Provider)
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out := map[string]any{
		"order_id":     res.Order.ID,
		"plan":         res.Order.Plan,
		"amount_cents": res.Order.AmountCents,
		"currency":     res.Order.Currency,
		"provider":     res.Order.Provider,
		"status":       res.Order.Status,
		"manual":       res.Manual,
	}
	if res.Checkout != nil {
		out["checkout_url"] = res.Checkout.URL
	}
	if res.Manual {
		out["message"] = "订单已进入手动激活模式（免费/轻量版路径，始终可用）：请联系管理员激活。在线支付渠道（微信/支付宝/Stripe）配置后即可直接跳转支付。"
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// HandleGetOrder GET /api/v1/billing/orders/{id}
func (h HandlerSet) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	id := lastPathSegment(r.URL.Path, "/api/v1/billing/orders/")
	if id == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少订单 ID"})
		return
	}
	o, err := h.Svc.GetOrder(r.Context(), id)
	if err != nil {
		httputil.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "订单不存在"})
		return
	}
	// 工作区归属校验：仅订单所属工作区可见。
	if wsID, ok := h.currentWorkspace(r); ok && wsID != "" && o.WorkspaceID != wsID {
		httputil.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "无权访问该订单"})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, o)
}

// ---- 支付渠道 Webhook（公开，凭签名校验） ----

// HandleWebhook POST /api/v1/billing/webhook/{provider}
// 接收微信/支付宝/Stripe 的支付回调，校验签名后标记订单已支付并激活套餐。
func (h HandlerSet) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	provider := lastPathSegment(r.URL.Path, "/api/v1/billing/webhook/")
	if provider == "" {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少支付渠道"})
		return
	}
	prov := payment.GetProvider(provider)
	if prov == nil {
		httputil.WriteJSON(w, http.StatusNotFound, map[string]string{
			"error": "支付渠道未配置或未启用: " + provider,
		})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "读取请求体失败"})
		return
	}
	ev, err := prov.VerifyWebhook(r, body)
	if err != nil {
		slog.Warn("billing: webhook 校验失败",
			slog.String("provider", provider), slog.Any("error", err))
		httputil.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook 校验失败: " + err.Error()})
		return
	}
	if ev.Status == "paid" {
		if err := h.Svc.MarkOrderPaidAndActivate(r.Context(), ev.OrderID, ev.ProviderOrderID, ""); err != nil {
			httputil.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	// 支付宝 notify 网关要求响应体含字面量 "success" 才停止重试；
	// 其他渠道（微信/Stripe）返回标准 JSON 即可。
	if provider == "alipay" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "order_id": ev.OrderID, "status": ev.Status})
}

// lastPathSegment 从 path 中截去 prefix 后取尾部段。
func lastPathSegment(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	s := path[len(prefix):]
	// 去除末尾斜杠
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

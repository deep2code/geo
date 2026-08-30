package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"my-geo/internal/billing/payment"
)

// Service 计费业务服务。组合 store + 套餐目录 + 支付渠道抽象。
type Service struct {
	store *Store
}

// NewService 构建计费服务。
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Enabled 计费是否可用（数据库已连接即视为可用）。
func (s *Service) Enabled() bool { return s.store != nil }

// Store 返回底层存储（供队列共享连接）。
func (s *Service) Store() *Store { return s.store }

// ---- 套餐与订阅 ----

// Plans 返回套餐目录。
func (s *Service) Plans() []Plan { return Catalog() }

// PlanOf 返回订阅对应的套餐（未知套餐回落 free）。
func (s *Service) PlanOf(sub *Subscription) Plan {
	if p, ok := PlanByID(sub.Plan); ok {
		return p
	}
	p, _ := PlanByID(PlanFree)
	return p
}

// Subscription 取工作区订阅（自动补 free）。
func (s *Service) Subscription(ctx context.Context, wsID string) (*Subscription, error) {
	return s.store.GetSubscription(ctx, wsID)
}

// Usage 返回工作区当月各计量状态。仅返回在套餐中有定义上限的维度。
func (s *Service) Usage(ctx context.Context, wsID string) ([]MeterState, error) {
	sub, err := s.store.GetSubscription(ctx, wsID)
	if err != nil {
		return nil, err
	}
	plan := s.PlanOf(sub)
	month := MonthKey(time.Now())
	var out []MeterState
	for m := range plan.Limits {
		st, err := s.store.MeterStateOf(ctx, wsID, m, month)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, nil
}

// Activate 手动激活 / 变更工作区套餐（轻量版核心路径：管理员直接激活，
// 无需支付渠道）。periodDays 为订阅周期天数（默认 30）。
func (s *Service) Activate(ctx context.Context, wsID string, plan PlanID, adminID string, periodDays int) error {
	if _, ok := PlanByID(plan); !ok {
		return fmt.Errorf("billing: 未知套餐 %q", plan)
	}
	if periodDays <= 0 {
		periodDays = 30
	}
	return s.store.SetPlan(ctx, wsID, plan, adminID, periodDays)
}

// CanRun 检查工作区某计量是否还有额度。无上限或订阅未配置时放行。
// 返回 (allowed, state, error)。
func (s *Service) CanRun(ctx context.Context, wsID string, m Meter) (bool, *MeterState, error) {
	if wsID == "" {
		// 未启用账号体系 / 匿名请求：配额无法关联到工作区，放行（由全局限流兜底）。
		return true, nil, nil
	}
	sub, err := s.store.GetSubscription(ctx, wsID)
	if err != nil {
		return false, nil, err
	}
	plan := s.PlanOf(sub)
	limit, ok := plan.Limits[m]
	if !ok || limit == Unlimited {
		return true, nil, nil
	}
	month := MonthKey(time.Now())
	st, err := s.store.MeterStateOf(ctx, wsID, m, month)
	if err != nil {
		return false, nil, err
	}
	st.Limit = limit
	return st.Used < limit, st, nil
}

// RecordUsage 累加工作区某计量用量（审计/导出后调用）。
//
// 采用带套餐上限的原子条件累加（TryIncrementMeter）：并发请求即便都先通过了
// CanRun，累计用量也不会突破套餐上限，避免"剩 1 次、并发双通过、双双扣次"。
func (s *Service) RecordUsage(ctx context.Context, wsID string, m Meter) error {
	if wsID == "" {
		return nil
	}
	limit := Unlimited
	if sub, err := s.store.GetSubscription(ctx, wsID); err == nil && sub != nil {
		if l, ok := s.PlanOf(sub).Limits[m]; ok {
			limit = l
		}
	}
	_, _, err := s.store.TryIncrementMeter(ctx, wsID, m, MonthKey(time.Now()), 1, limit)
	return err
}

// ---- 订单与支付 ----

// CreateOrderResult 创建订单后的结果（含可选的支付跳转信息）。
type CreateOrderResult struct {
	Order    *Order
	Checkout *payment.CheckoutResult // 渠道已配置时为支付跳转；否则为 nil（手动模式）
	Manual   bool                    // true=手动激活模式（免费/轻量版或渠道调用失败时）
}

// PaymentMethods 返回全部可用支付方式（微信/支付宝/Stripe + 手动激活），并存。
// 在线渠道按其凭据齐备情况标注 configured，前端据此渲染亮/灰按钮。
func (s *Service) PaymentMethods() []payment.Method {
	return payment.AllMethods()
}

// CreateOrder 为工作区创建套餐订单。
//
// 支付方式并存模型（微信/支付宝/Stripe + 手动激活，互不替代）：
//   - 免费套餐（PriceCents==0，如 free / enterprise 面议）或显式 provider="manual"
//     → 进入「手动激活」模式（轻量版核心路径），订单不绑定在线渠道，等待管理员
//     通过 /activate 激活。该路径始终可用，与在线支付并存。
//   - 付费套餐（pro/team）必须指定在线渠道之一（wechatpay / alipay / stripe）：
//     · 渠道已配置 → 调用 CreateCheckout 返回跳转地址；
//     · 渠道未配置 → 明确报错，不静默降级（前端据此展示「未配置」态）；
//     · 渠道已配置但调用失败 → 降级为手动（网络异常兜底，保证下单不阻断）。
func (s *Service) CreateOrder(ctx context.Context, wsID string, plan PlanID, provider string) (*CreateOrderResult, error) {
	p, ok := PlanByID(plan)
	if !ok {
		return nil, fmt.Errorf("billing: 未知套餐 %q", plan)
	}
	// 确保有订阅行（即便 free）。
	if _, err := s.store.GetSubscription(ctx, wsID); err != nil {
		return nil, err
	}
	o := &Order{
		WorkspaceID: wsID,
		Plan:        plan,
		AmountCents: p.PriceCents,
		Currency:    p.Currency,
	}
	res := &CreateOrderResult{Order: o}

	// 1) 免费 / 显式手动 → 手动激活模式（始终可用，轻量版核心路径）。
	if p.PriceCents == 0 || provider == "manual" {
		o.Provider = "manual"
		o.Status = "created"
		if err := s.store.CreateOrder(ctx, o); err != nil {
			return nil, err
		}
		res.Manual = true
		slog.Info("billing: 订单进入手动激活模式",
			slog.String("workspace", wsID), slog.String("plan", string(plan)))
		return res, nil
	}

	// 2) 付费套餐：必须指定在线渠道（微信/支付宝/Stripe 并存可选）。
	if provider == "" {
		return nil, fmt.Errorf("billing: 付费套餐需指定支付方式（wechatpay/alipay/stripe）")
	}
	prov := payment.GetProvider(provider)
	if prov == nil {
		return nil, fmt.Errorf("billing: 支付渠道 %q 未配置或未启用，请在后台配置对应商户凭据", provider)
	}
	// 先落库生成订单 ID；支付渠道的 out_trade_no/order_id 必须非空，回调才能关联订单。
	o.Provider = prov.Name()
	if err := s.store.CreateOrder(ctx, o); err != nil {
		return nil, err
	}
	checkout, err := prov.CreateCheckout(ctx, payment.Order{
		ID:          o.ID,
		WorkspaceID: wsID,
		Plan:        string(plan),
		AmountCents: p.PriceCents,
		Currency:    p.Currency,
	}, "")
	if err != nil {
		// 渠道已配置但调用失败：降级为手动（网络异常兜底），保证下单不阻断。
		slog.Warn("billing: 支付渠道下单失败，降级为手动激活",
			slog.String("order", o.ID), slog.String("provider", o.Provider), slog.Any("error", err))
		o.Provider = "manual"
		o.Status = "created"
		if err2 := s.store.UpdateOrder(ctx, o); err2 != nil {
			return nil, err2
		}
		res.Manual = true
		return res, nil
	}
	o.ProviderOrderID = checkout.ProviderOrderID
	o.CheckoutURL = checkout.URL
	if err := s.store.UpdateOrder(ctx, o); err != nil {
		return nil, err
	}
	res.Checkout = checkout
	return res, nil
}

// MarkOrderPaidAndActivate 标记订单已支付并激活对应套餐（webhook / 管理员均可调用）。
//
// expectedAmountCents 为渠道回调实际支付金额（分）；>0 时与订单金额比对，
// 不一致拒绝激活（防"付小钱开大套餐"的纵深防御）。管理员手动激活传 0 跳过。
func (s *Service) MarkOrderPaidAndActivate(ctx context.Context, orderID, providerOrderID, adminID string, expectedAmountCents int64) error {
	o, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	// 金额一致性校验：渠道实付金额必须与订单金额一致（渠道未提供金额时跳过）
	if expectedAmountCents > 0 && expectedAmountCents != o.AmountCents {
		return fmt.Errorf("billing: 支付金额与订单不一致（订单 %d 分，实付 %d 分），拒绝激活",
			o.AmountCents, expectedAmountCents)
	}
	// 幂等：订单已支付"且订阅已生效"才直接返回。只看 status=paid 会把
	// "MarkOrderPaid 成功但 SetPlan 失败"的订单也挡掉——webhook 重试命中早退
	// 返回成功，渠道停止重试，用户已付款但套餐永远不激活。
	if o.Status == "paid" {
		sub, serr := s.store.GetSubscription(ctx, o.WorkspaceID)
		if serr == nil && sub != nil && sub.Status == "active" && sub.Plan == o.Plan &&
			sub.CurrentPeriodEnd > time.Now().Unix() {
			return nil
		}
		// 订阅未生效：继续走下方 SetPlan 补激活（MarkOrderPaid 幂等，重入安全）
	}
	if err := s.store.MarkOrderPaid(ctx, orderID, providerOrderID); err != nil {
		if !errors.Is(err, ErrOrderAlreadyPaid) {
			return err
		}
	}
	period := 30
	if o.Plan == PlanEnterprise {
		period = 365 // 企业版按年
	}
	by := adminID
	if by == "" {
		by = "payment:" + o.Provider
	}
	return s.store.SetPlan(ctx, o.WorkspaceID, o.Plan, by, period)
}

// GetOrder 取订单。
func (s *Service) GetOrder(ctx context.Context, id string) (*Order, error) {
	return s.store.GetOrder(ctx, id)
}

// ListOrders 取工作区订单列表。
func (s *Service) ListOrders(ctx context.Context, wsID string) ([]*Order, error) {
	return s.store.OrdersByWorkspace(ctx, wsID)
}

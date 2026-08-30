package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	// 注册 MySQL 驱动（本项目统一使用 go-sql-driver/mysql）。
	_ "github.com/go-sql-driver/mysql"

	"my-geo/internal/dbprovider"
)

// ErrOrderAlreadyPaid 表示订单已是 paid 状态（通常由 webhook 重复投递导致）。
// 调用方应将其视为幂等成功，而非错误，避免支付宝/Stripe 重试风暴。
var ErrOrderAlreadyPaid = errors.New("billing: 订单已支付")

// Store 计费数据访问层。底层复用 MySQL（默认与账号体系同库）。
type Store struct {
	db  *sql.DB
	dsn string
}

// OpenStore 打开（或复用）billing 数据库连接。
//
// dsn 为空时返回错误：调用方需先解析统一 GEO_MYSQL_DSN（见 server.go 的 newBillingFromEnv）。
// 表结构由 deploy/initdb 初始化（schema.sql），应用内不再内嵌 migration。
func OpenStore(dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("billing: 未配置 GEO_MYSQL_DSN")
	}
	dsn = dbprovider.NormalizeMySQLDSN(dsn)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("billing: 打开数据库失败: %w", err)
	}
	dbprovider.ConfigurePool(db, "billing")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("billing: 数据库 ping 失败: %w", err)
	}
	return &Store{db: db, dsn: dsn}, nil
}

// DB 返回底层连接（供队列等共享同一连接复用）。
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭连接。
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Path 返回脱敏后的 DSN（用于日志）。
func (s *Store) Path() string { return s.dsn }

// ---- 订阅 ----

// Subscription 工作区订阅记录。
type Subscription struct {
	ID                 string
	WorkspaceID        string
	Plan               PlanID
	Status             string
	CurrentPeriodStart int64
	CurrentPeriodEnd   int64
	ActivatedBy        string
	ActivatedAt        int64
	CreatedAt          int64
	CancelledAt        int64
}

// EnsureSubscription 确保工作区存在一条 free 订阅（幂等）。
func (s *Store) EnsureSubscription(ctx context.Context, wsID string) (*Subscription, error) {
	sub, err := s.GetSubscription(ctx, wsID)
	if err == nil && sub != nil {
		return sub, nil
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT IGNORE INTO subscriptions
		 (id, workspace_id, plan, status, current_period_start, current_period_end, created_at)
		 VALUES (?, ?, 'free', 'active', 0, 0, ?)`,
		newID("sub"), wsID, now)
	if err != nil {
		return nil, err
	}
	return s.GetSubscription(ctx, wsID)
}

// GetSubscription 取工作区订阅；无记录时自动补建 free 订阅。
func (s *Store) GetSubscription(ctx context.Context, wsID string) (*Subscription, error) {
	sub, err := s.querySubscription(ctx, wsID)
	if err == sql.ErrNoRows {
		return s.EnsureSubscription(ctx, wsID)
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *Store) querySubscription(ctx context.Context, wsID string) (*Subscription, error) {
	var sub Subscription
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, plan, status, current_period_start, current_period_end,
		        activated_by, activated_at, created_at, cancelled_at
		 FROM subscriptions WHERE workspace_id = ?`, wsID).
		Scan(&sub.ID, &sub.WorkspaceID, &sub.Plan, &sub.Status,
			&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.ActivatedBy,
			&sub.ActivatedAt, &sub.CreatedAt, &sub.CancelledAt)
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// SetPlan 设定工作区套餐（手动激活 / 支付成功回调共用）。Upsert 语义。
func (s *Store) SetPlan(ctx context.Context, wsID string, plan PlanID, activatedBy string, periodDays int) error {
	now := time.Now().Unix()
	end := now + int64(periodDays)*86400
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions
		 (id, workspace_id, plan, status, current_period_start, current_period_end, activated_by, activated_at, created_at)
		 VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   plan=VALUES(plan), status='active', current_period_start=VALUES(current_period_start),
		   current_period_end=VALUES(current_period_end), activated_by=VALUES(activated_by),
		   activated_at=VALUES(activated_at), cancelled_at=0`,
		newID("sub"), wsID, string(plan), now, end, activatedBy, now, now)
	return err
}

// Cancel 取消订阅（保留历史，状态置 cancelled）。
func (s *Store) Cancel(ctx context.Context, wsID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET status='cancelled', cancelled_at=? WHERE workspace_id=?`,
		time.Now().Unix(), wsID)
	return err
}

// ---- 用量计量 ----

// MeterState 某工作区某月某计量的已用与上限（-1 不限）。
type MeterState struct {
	Meter Meter
	Used  int64
	Limit int64
	Month string
}

// IncrementMeter 原子累加某工作区某月某计量；返回最新 used 值。
func (s *Store) IncrementMeter(ctx context.Context, wsID string, m Meter, month string, n int64) (int64, error) {
	if n <= 0 {
		n = 1
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_meters (workspace_id, meter, period_month, used, `+"`limit`"+`, updated_at)
		 VALUES (?, ?, ?, ?, -1, ?)
		 ON DUPLICATE KEY UPDATE used = used + VALUES(used), updated_at = VALUES(updated_at)`,
		wsID, string(m), month, n, now)
	if err != nil {
		return 0, err
	}
	var used int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT used FROM usage_meters WHERE workspace_id=? AND meter=? AND period_month=?`,
		wsID, string(m), month).Scan(&used); err != nil {
		return 0, err
	}
	return used, nil
}

// TryIncrementMeter 带上限的原子条件累加：仅当累加后 used <= limit（limit<0 不限）时
// 才写入，检查与累加在同一条 UPDATE 内原子完成，消除 CanRun 与 IncrementMeter
// 两步之间并发请求双双通过的 TOCTOU 竞争。返回是否成功与最新 used。
func (s *Store) TryIncrementMeter(ctx context.Context, wsID string, m Meter, month string, n, limit int64) (bool, int64, error) {
	if n <= 0 {
		n = 1
	}
	now := time.Now().Unix()
	// 幂等建行（首次使用，used 起始 0）
	if _, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO usage_meters (workspace_id, meter, period_month, used, `+"`limit`"+`, updated_at)
		 VALUES (?, ?, ?, 0, -1, ?)`,
		wsID, string(m), month, now); err != nil {
		return false, 0, err
	}
	var (
		res sql.Result
		err error
	)
	if limit < 0 {
		res, err = s.db.ExecContext(ctx,
			`UPDATE usage_meters SET used = used + ?, updated_at = ?
			 WHERE workspace_id=? AND meter=? AND period_month=?`,
			n, now, wsID, string(m), month)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE usage_meters SET used = used + ?, updated_at = ?
			 WHERE workspace_id=? AND meter=? AND period_month=? AND used + ? <= ?`,
			n, now, wsID, string(m), month, n, limit)
	}
	if err != nil {
		return false, 0, err
	}
	affected, _ := res.RowsAffected()
	var used int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT used FROM usage_meters WHERE workspace_id=? AND meter=? AND period_month=?`,
		wsID, string(m), month).Scan(&used); err != nil {
		return false, 0, err
	}
	return affected == 1, used, nil
}

// MeterStateOf 取某工作区某月某计量的状态。
func (s *Store) MeterStateOf(ctx context.Context, wsID string, m Meter, month string) (*MeterState, error) {
	var used, lim int64
	err := s.db.QueryRowContext(ctx,
		`SELECT used, `+"`limit`"+` FROM usage_meters WHERE workspace_id=? AND meter=? AND period_month=?`,
		wsID, string(m), month).Scan(&used, &lim)
	if err == sql.ErrNoRows {
		return &MeterState{Meter: m, Used: 0, Limit: Unlimited, Month: month}, nil
	}
	if err != nil {
		return nil, err
	}
	return &MeterState{Meter: m, Used: used, Limit: lim, Month: month}, nil
}

// ---- 订单与发票 ----

// Order 支付订单。
type Order struct {
	ID              string
	WorkspaceID     string
	Provider        string // wechatpay / alipay / stripe / manual
	Plan            PlanID
	AmountCents     int64
	Currency        string
	Status          string // created / paid / failed / refunded
	ProviderOrderID string
	CheckoutURL     string
	CreatedAt       int64
	PaidAt          int64
	Metadata        string
}

// CreateOrder 写入订单（created 状态）。
func (s *Store) CreateOrder(ctx context.Context, o *Order) error {
	if o.ID == "" {
		o.ID = newID("ord")
	}
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().Unix()
	}
	if o.Currency == "" {
		o.Currency = "CNY"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO payment_orders
		 (id, workspace_id, provider, plan, amount_cents, currency, status, provider_order_id, checkout_url, created_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, 'created', ?, ?, ?, ?)`,
		o.ID, o.WorkspaceID, o.Provider, string(o.Plan), o.AmountCents, o.Currency,
		o.ProviderOrderID, o.CheckoutURL, o.CreatedAt, o.Metadata)
	return err
}

// UpdateOrder 更新订单的可变字段（provider/status/provider_order_id/checkout_url/paid_at/metadata）。
func (s *Store) UpdateOrder(ctx context.Context, o *Order) error {
	if o.ID == "" {
		return fmt.Errorf("billing: UpdateOrder 订单 ID 不能为空")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE payment_orders
		 SET provider=?, status=?, provider_order_id=?, checkout_url=?, paid_at=?, metadata=?
		 WHERE id=?`,
		o.Provider, o.Status, o.ProviderOrderID, o.CheckoutURL, o.PaidAt, o.Metadata, o.ID)
	return err
}

// GetOrder 取订单。
func (s *Store) GetOrder(ctx context.Context, id string) (*Order, error) {
	var o Order
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workspace_id, provider, plan, amount_cents, currency, status,
		        provider_order_id, checkout_url, created_at, paid_at, metadata
		 FROM payment_orders WHERE id=?`, id).
		Scan(&o.ID, &o.WorkspaceID, &o.Provider, &o.Plan, &o.AmountCents, &o.Currency,
			&o.Status, &o.ProviderOrderID, &o.CheckoutURL, &o.CreatedAt, &o.PaidAt, &o.Metadata)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// OrdersByWorkspace 取工作区全部订单（按创建时间倒序）。
func (s *Store) OrdersByWorkspace(ctx context.Context, wsID string) ([]*Order, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workspace_id, provider, plan, amount_cents, currency, status,
		        provider_order_id, checkout_url, created_at, paid_at, metadata
		 FROM payment_orders WHERE workspace_id=? ORDER BY created_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.WorkspaceID, &o.Provider, &o.Plan, &o.AmountCents,
			&o.Currency, &o.Status, &o.ProviderOrderID, &o.CheckoutURL, &o.CreatedAt,
			&o.PaidAt, &o.Metadata); err != nil {
			return nil, err
		}
		out = append(out, &o)
	}
	return out, rows.Err()
}

// MarkOrderPaid 标记订单已支付并补建发票。
func (s *Store) MarkOrderPaid(ctx context.Context, id, providerOrderID string) error {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE payment_orders SET status='paid', paid_at=?, provider_order_id=?
		 WHERE id=? AND status IN ('created','failed')`, now, providerOrderID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 订单已处于 paid（或 refunded）状态，视为幂等哨兵，由上层忽略。
		return ErrOrderAlreadyPaid
	}
	o, err := s.GetOrder(ctx, id)
	if err != nil {
		return err
	}
	inv := &Invoice{
		ID:          newID("inv"),
		WorkspaceID: o.WorkspaceID,
		OrderID:     o.ID,
		AmountCents: o.AmountCents,
		Currency:    o.Currency,
		Status:      "issued",
		IssuedAt:    now,
	}
	return s.CreateInvoice(ctx, inv)
}

// Invoice 发票/账单记录。
type Invoice struct {
	ID          string
	WorkspaceID string
	OrderID     string
	AmountCents int64
	Currency    string
	Status      string
	IssuedAt    int64
	URL         string
}

// CreateInvoice 写入发票。
func (s *Store) CreateInvoice(ctx context.Context, inv *Invoice) error {
	if inv.ID == "" {
		inv.ID = newID("inv")
	}
	if inv.IssuedAt == 0 {
		inv.IssuedAt = time.Now().Unix()
	}
	if inv.Status == "" {
		inv.Status = "issued"
	}
	if inv.Currency == "" {
		inv.Currency = "CNY"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO invoices (id, workspace_id, order_id, amount_cents, currency, status, issued_at, url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, inv.WorkspaceID, inv.OrderID, inv.AmountCents, inv.Currency, inv.Status, inv.IssuedAt, inv.URL)
	return err
}

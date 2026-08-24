package config

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"my-geo/internal/dsnutil"

	_ "github.com/go-sql-driver/mysql" // MySQL 驱动（注册 sql.Open("mysql")）
)

// 配置读取（2026-08-21 用户要求：运行参数只读 DB，默认值由建库 SQL 植入）。
//
// 设计要点：
//   - config.Env(key, fallback) 是全局唯一读取入口；本包维护一个内存覆盖表，
//     启动时从 MySQL app_settings 表加载（见 InitSettings）：
//     非引导项：只读 DB（默认值由 deploy/initdb/schema.sql 种子 INSERT 建库时植入）→ fallback，
//     **不回退环境变量**；引导项（DSN/初始管理员/AUTH 开关/JWT 密钥）：始终用环境变量 → fallback。
//   - 未调用 InitSettings（无 DB 或调用失败）时：非引导项仅返回 fallback，引导项读环境变量。
//   - 引导项在 DB 加载前已决定系统引导（如 DSN 自身、账号体系开关），管理后台对其只读展示。
//
// 表结构见 deploy/initdb/02-schema.sql（app_settings），应用内零建表。

// Setting 一条可管理配置项（注册表条目 + 当前值）。
type Setting struct {
	Key            string `json:"key"`
	Value          string `json:"value"` // 当前生效值（secret 类由接口层脱敏）
	DefaultValue   string `json:"default_value"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	Type           string `json:"type"` // string / bool / int / float / secret
	IsSecret       bool   `json:"is_secret"`
	IsBootstrap    bool   `json:"is_bootstrap"`    // 引导变量：DB 不覆盖，只读
	RequiresRestart bool  `json:"requires_restart"` // 修改后需重启生效（运行期缓存）
	Source         string `json:"source"`          // db / env / default / unset
}

// SettingsManager 运行时配置管理器（全局单例）。
type SettingsManager struct {
	mu        sync.RWMutex
	overrides map[string]string // DB 覆盖值（非 bootstrap）
	db        *sql.DB
	loaded    bool
	catalog   []Setting // 注册表（含 DB 中已登记条目的 value）
}

var settings = &SettingsManager{
	overrides: map[string]string{},
	catalog:   mergeCatalog(buildCatalog(), extraCatalog()),
}

// mergeCatalog 合并自动生成与手工补充的注册表，手工项优先：
//   - 相同 key：保留生成项的默认值（可能有），手工项的标注（分类/类型/敏感/重启）覆盖；
//   - 描述优先保留非空者。
func mergeCatalog(gen, extra []Setting) []Setting {
	idx := make(map[string]int, len(gen)+len(extra))
	out := make([]Setting, 0, len(gen)+len(extra))
	for _, s := range gen {
		idx[s.Key] = len(out)
		out = append(out, s)
	}
	for _, s := range extra {
		if i, ok := idx[s.Key]; ok {
			cur := &out[i]
			if cur.DefaultValue == "" {
				cur.DefaultValue = s.DefaultValue
			}
			if cur.Description == "" {
				cur.Description = s.Description
			}
			if cur.Category == "general" {
				cur.Category = s.Category
			}
			if s.Type != "" {
				cur.Type = s.Type
			}
			if s.IsSecret {
				cur.IsSecret = true
			}
			cur.IsBootstrap = cur.IsBootstrap || s.IsBootstrap
			cur.RequiresRestart = cur.RequiresRestart || s.RequiresRestart
			continue
		}
		idx[s.Key] = len(out)
		out = append(out, s)
	}
	return out
}

// DB 返回底层数据库连接（可能为 nil）。
func (m *SettingsManager) DB() *sql.DB { m.mu.RLock(); defer m.mu.RUnlock(); return m.db }

// Loaded 是否已从 DB 加载覆盖表。
func (m *SettingsManager) Loaded() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.loaded }

// InitSettings 连接 MySQL 并加载 app_settings 覆盖表（幂等）。
//
// dsn 为空时跳过（等价未初始化）。DB 不可达时返回错误但**不 panic**——
// 调用方应记录告警并继续启动（非引导项退回代码默认值，引导项仍读环境变量）。
// seed=true 时把注册表元信息（默认值/描述/分类/类型等）幂等同步到 DB
// （INSERT ... ON DUPLICATE KEY UPDATE 元信息，不写 svalue、不覆盖用户修改）。
func InitSettings(ctx context.Context, dsn string, seed bool) error {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		slog.Info("config: 未配置设置库 DSN，跳过 DB 配置加载（非引导项使用代码默认值，引导项读环境变量）")
		return nil
	}
	dsn = dsnutil.NormalizeMySQLDSN(dsn)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("config: 打开设置库失败: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("config: 设置库连接失败: %w", err)
	}
	if seed {
		if err := seedSettings(ctx, db); err != nil {
			_ = db.Close()
			return fmt.Errorf("config: 初始化 app_settings 失败: %w", err)
		}
	}
	if err := loadSettings(ctx, db); err != nil {
		_ = db.Close()
		return fmt.Errorf("config: 加载 app_settings 失败: %w", err)
	}
	settings.mu.Lock()
	settings.db = db
	settings.loaded = true
	settings.mu.Unlock()
	n := len(settings.Overrides())
	slog.Info("config: DB 配置已加载", slog.Int("overrides", n))
	// 校验 DB 覆盖的 JWT 签名密钥强度（env 侧已在 config.Validate 校验过，DB 侧补一次）
	if sec, ok := settings.Get("GEO_JWT_SECRET"); ok && len(strings.TrimSpace(sec)) < 32 {
		slog.Warn("GEO_JWT_SECRET（配置表）长度不足 32 字节，签名强度偏弱，建议重新生成后更新配置表")
	}
	return nil
}

// Close 关闭设置库连接（服务退出时调用）。
func Close() {
	settings.mu.Lock()
	defer settings.mu.Unlock()
	if settings.db != nil {
		_ = settings.db.Close()
		settings.db = nil
	}
	settings.loaded = false
}

// Overrides 返回 DB 覆盖键数量（日志用）。
func (m *SettingsManager) Overrides() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.overrides))
	for k, v := range m.overrides {
		out[k] = v
	}
	return out
}

// Get 返回某 key 的 DB 覆盖值；bootstrap 项或未加载时返回 "", false。
func (m *SettingsManager) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.loaded || m.isBootstrapLocked(key) {
		return "", false
	}
	v, ok := m.overrides[key]
	return v, ok
}

func (m *SettingsManager) isBootstrapLocked(key string) bool {
	for _, s := range m.catalog {
		if s.Key == key {
			return s.IsBootstrap
		}
	}
	return false
}

// UpdateSetting 更新配置值（写 DB + 更新内存覆盖表）。
//
// 返回 (restartRequired bool, err)。bootstrap 项拒绝修改。
func UpdateSetting(ctx context.Context, key, value string) (bool, error) {
	settings.mu.RLock()
	db := settings.db
	loaded := settings.loaded
	settings.mu.RUnlock()
	if db == nil || !loaded {
		return false, fmt.Errorf("config: 设置库未加载")
	}
	s := findSetting(key)
	if s == nil {
		return false, fmt.Errorf("config: 未知配置项 %s", key)
	}
	if s.IsBootstrap {
		return false, fmt.Errorf("config: %s 为引导变量，请通过环境变量配置", key)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO app_settings (skey, svalue, default_value, description, category, stype, is_secret, is_bootstrap, requires_restart, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE svalue = VALUES(svalue), updated_at = VALUES(updated_at)`,
		key, value, s.DefaultValue, s.Description, s.Category, s.Type,
		boolInt(s.IsSecret), boolInt(s.IsBootstrap), boolInt(s.RequiresRestart), time.Now().Unix(),
	); err != nil {
		return false, fmt.Errorf("config: 写入设置失败: %w", err)
	}
	settings.mu.Lock()
	settings.overrides[key] = value
	settings.mu.Unlock()
	return s.RequiresRestart, nil
}

// ResetSetting 恢复某配置项为默认值（清空 DB 覆盖值）。
func ResetSetting(ctx context.Context, key string) error {
	settings.mu.RLock()
	db := settings.db
	settings.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("config: 设置库未加载")
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE app_settings SET svalue = '', updated_at = ? WHERE skey = ?`,
		time.Now().Unix(), key); err != nil {
		return fmt.Errorf("config: 重置设置失败: %w", err)
	}
	settings.mu.Lock()
	delete(settings.overrides, key)
	settings.mu.Unlock()
	return nil
}

// ListSettings 返回注册表全量 + 当前生效值（供管理后台）。
//
// env 覆盖判定使用进程环境变量；未初始化 DB 时 source 为 env/default/unset。
func ListSettings() []Setting {
	settings.mu.RLock()
	overrides := make(map[string]string, len(settings.overrides))
	for k, v := range settings.overrides {
		overrides[k] = v
	}
	loaded := settings.loaded
	settings.mu.RUnlock()

	out := make([]Setting, 0, len(settings.catalog))
	for _, s := range settings.catalog {
		cur := s
		cur.Value = ""
		// 非引导项：只读 DB（初始化时默认值已植入，用户改过则显示 DB 值）。
		// 引导项（DSN/管理员/AUTH/JWT）：环境变量引导，后台只读展示。
		if dbv, ok := overrides[s.Key]; ok && !s.IsBootstrap {
			cur.Value = dbv
			cur.Source = "db"
		} else if ev := os.Getenv(s.Key); ev != "" && s.IsBootstrap {
			cur.Value = ev
			cur.Source = "env"
		} else if s.DefaultValue != "" {
			cur.Value = s.DefaultValue
			cur.Source = "default"
		} else {
			cur.Source = "unset"
		}
		_ = loaded
		out = append(out, cur)
	}
	return out
}

// seedSettings 幂等写入注册表默认值（不覆盖用户已改 value）。
func seedSettings(ctx context.Context, db *sql.DB) error {
	cats := settings.catalog
	if len(cats) == 0 {
		return nil
	}
	stmt, err := db.PrepareContext(ctx, `INSERT INTO app_settings
		(skey, svalue, default_value, description, category, stype, is_secret, is_bootstrap, requires_restart, updated_at)
		VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE default_value = VALUES(default_value),
			description = VALUES(description), category = VALUES(category),
			stype = VALUES(stype), is_secret = VALUES(is_secret),
			is_bootstrap = VALUES(is_bootstrap), requires_restart = VALUES(requires_restart)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, s := range cats {
		// 默认值由 deploy/initdb/schema.sql 的种子 INSERT 在建库时植入（见
		// scripts/gen_app_settings_seed），此处仅同步元信息、不写 svalue；
		// 已存在的行保持用户修改值不动。
		if _, err := stmt.ExecContext(ctx, s.Key, s.DefaultValue, s.Description,
			s.Category, s.Type, boolInt(s.IsSecret), boolInt(s.IsBootstrap),
			boolInt(s.RequiresRestart), now); err != nil {
			return fmt.Errorf("config: 写入 %s: %w", s.Key, err)
		}
	}
	return nil
}

// loadSettings 加载 DB 中非空的 svalue 为覆盖表。
func loadSettings(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT skey, svalue, default_value, description, category, stype, is_secret, is_bootstrap, requires_restart
		 FROM app_settings WHERE svalue <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ov := map[string]string{}
	catalogIdx := map[string]int{}
	for i, s := range settings.catalog {
		catalogIdx[s.Key] = i
	}
	// 以 DB 为准刷新 catalog 的元信息（描述/分类/默认值可能已在 DB 修正）
	for rows.Next() {
		var (
			key, val, def, desc, cat, typ string
			sec, boot, restart            int
		)
		if err := rows.Scan(&key, &val, &def, &desc, &cat, &typ, &sec, &boot, &restart); err != nil {
			return err
		}
		ov[key] = val
		if i, ok := catalogIdx[key]; ok {
			settings.catalog[i].DefaultValue = def
			settings.catalog[i].Description = desc
			settings.catalog[i].Category = cat
			settings.catalog[i].Type = typ
			settings.catalog[i].IsSecret = sec == 1
			settings.catalog[i].IsBootstrap = boot == 1
			settings.catalog[i].RequiresRestart = restart == 1
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	settings.mu.Lock()
	settings.overrides = ov
	settings.mu.Unlock()
	return nil
}

func findSetting(key string) *Setting {
	for i := range settings.catalog {
		if settings.catalog[i].Key == key {
			return &settings.catalog[i]
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

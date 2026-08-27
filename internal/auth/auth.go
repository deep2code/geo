// Package auth 提供身份、账号、工作区、会话与 RBAC 能力。
//
// 范围：
//   - 用户注册 / 登录 / 登出（邮箱+密码，bcrypt hash）
//   - 多租户 Workspace 隔离（用户可属于多个工作区，带角色）
//   - JWT 访问令牌 + 刷新令牌（access/refresh token 双令牌模型）
//   - RBAC 角色：Owner / Admin / Member / Viewer，带权限检查中间件
//   - Admin 操作审计日志（登录/登出/改密码/改角色/删用户/删品牌 留痕）
//
// 设计要点：
//   - 数据库：使用 github.com/go-sql-driver/mysql（MySQL 8.0+），
//     环境变量 GEO_MYSQL_DSN 自定义连接串（单库架构唯一 DSN）。
//   - 鉴权唯一方式：JWT 账号体系（GEO_AUTH_ENABLED=true）。未启用时中间件
//     无鉴权放行（本地开发 / 反向代理场景），管理类接口一律 403。
//   - 首用户注册引导：DB 无用户时允许首用户注册并自动设为 Owner
//     （首个默认工作区 "Default Workspace"）。
//   - WorkspaceID 注入 context：鉴权中间件解析出当前工作区后，
//     业务 handler 可通过 auth.WorkspaceIDFromContext(ctx) 获取。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "github.com/go-sql-driver/mysql"

	"my-geo/internal/config"
	"my-geo/internal/dbprovider"
	"my-geo/internal/util"
)

// ============================================================
// 1. 常量与角色权限模型
// ============================================================

// Role 用户在工作区中的角色（Owner > Admin > Member > Viewer）。
type Role string

const (
	RoleOwner  Role = "owner"  // 工作区拥有者，最高权限（不可删除自身，唯一或少数）
	RoleAdmin  Role = "admin"  // 管理员：管理成员、品牌、审计、设置
	RoleMember Role = "member" // 普通成员：审计、发邮件、写内容
	RoleViewer Role = "viewer" // 只读成员：查看报告/历史
)

// AllRoles 所有合法角色列表。
var AllRoles = []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}

// Permission 权限枚举，与角色映射。
type Permission string

const (
	PermViewReport      Permission = "report:view"      // 查看报告与历史
	PermWriteAudit      Permission = "audit:write"      // 发起审计、生成报告
	PermSendMail        Permission = "mail:send"        // 发送邮件/PDF
	PermManageBrand     Permission = "brand:manage"     // 增删改品牌档案
	PermManageWorkspace Permission = "workspace:manage" // 编辑工作区设置
	PermInviteMember    Permission = "member:invite"    // 邀请/加入成员
	PermManageMember    Permission = "member:manage"    // 调整角色、移除成员
	PermManageAdmin     Permission = "admin:manage"     // 平台级管理（系统用）
	PermDeleteWorkspace Permission = "workspace:delete" // 删除工作区（仅 Owner）
	PermManageData      Permission = "data:manage"      // 清理/重置数据（清空离线库/审计历史/缓存，仅 Owner/Admin）
)

// rolePermissions 角色 → 权限集映射。
var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner: permSet(
		PermViewReport, PermWriteAudit, PermSendMail, PermManageBrand,
		PermManageWorkspace, PermInviteMember, PermManageMember,
		PermManageAdmin, PermDeleteWorkspace, PermManageData,
	),
	RoleAdmin: permSet(
		PermViewReport, PermWriteAudit, PermSendMail, PermManageBrand,
		PermManageWorkspace, PermInviteMember, PermManageMember, PermManageData,
	),
	RoleMember: permSet(
		PermViewReport, PermWriteAudit, PermSendMail, PermManageBrand,
		PermInviteMember,
	),
	RoleViewer: permSet(PermViewReport),
}

func permSet(ps ...Permission) map[Permission]bool {
	m := make(map[Permission]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

// HasRolePermission 静态检查某角色是否拥有权限。
func HasRolePermission(r Role, p Permission) bool {
	set, ok := rolePermissions[r]
	if !ok {
		return false
	}
	return set[p]
}

// roleGt 返回角色层级序号（越大权限越高）。
func roleGt(r Role) int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// RoleGte 判断 a 角色是否 ≥ b（如 Admin ≥ Member 为 true）。
func RoleGte(a, b Role) bool { return roleGt(a) >= roleGt(b) }

// ============================================================
// 2. 核心模型
// ============================================================

// User 用户账号。
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // 不对外序列
	DisplayName  string    `json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
	Verified     bool      `json:"verified"`
	// TokenVersion 会话版本号：改密 / 管理员手动吊销时 +1，
	// 使该用户此前签发的全部 JWT access token 立即失效。
	TokenVersion int64 `json:"-"`
}

// Workspace 工作区（多租户隔离边界）。
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	OwnerID   string    `json:"owner_id"`
	Plan      string    `json:"plan"` // free / pro / enterprise
}

// Membership 用户-工作区-角色 关联。
type Membership struct {
	UserID      string    `json:"user_id"`
	WorkspaceID string    `json:"workspace_id"`
	Role        Role      `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

// WorkspaceWithRole 工作区信息 + 当前用户在其中的角色（用于 /me 列表）。
type WorkspaceWithRole struct {
	Workspace
	Role Role `json:"role"`
}

// AdminAuditLog 平台级管理员操作留痕。
type AdminAuditLog struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	ActorID   string            `json:"actor_id"`
	Actor     string            `json:"actor"`   // 操作人邮箱
	Action    string            `json:"action"`  // 事件标识
	Target    string            `json:"target"`  // 被操作对象（用户ID/邮箱/品牌名/工作区ID）
	Details   map[string]string `json:"details"` // 附加字段
	IP        string            `json:"ip"`
	UserAgent string            `json:"user_agent"`
}

// TokenPair 登录 / 刷新接口返回的双令牌。
type TokenPair struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	TokenType      string `json:"token_type"` // "Bearer"
	ExpiresIn      int    `json:"expires_in"` // access token 过期秒数
	RefreshExpires int    `json:"refresh_expires_in"`
}

// ============================================================
// 3. JWT 模块（手工 HMAC-SHA256，避免引入额外依赖）
// ============================================================

const (
	accessTokenTTL  = 2 * time.Hour       // 访问令牌 2 小时
	refreshTokenTTL = 14 * 24 * time.Hour // 刷新令牌 14 天
)

type jwtClaims struct {
	Sub         string `json:"sub"`           // 用户 ID
	Email       string `json:"email"`         // 邮箱
	WorkspaceID string `json:"wid,omitempty"` // 工作区
	Role        Role   `json:"role,omitempty"`
	JTI         string `json:"jti"` // Token ID（用于 revoke）
	Exp         int64  `json:"exp"`
	Iat         int64  `json:"iat"`
	Type        string `json:"typ"` // "access" / "refresh"
	V           int64  `json:"v"`   // 用户 token 版本号（改密/手动吊销后 +1，旧 token 立即失效）
}

// getJWTSecret 获取 JWT 签名密钥（DB 配置表 > 环境变量；两者都未配置时生成一次性启动密钥）。
// 生产部署必须配置 GEO_JWT_SECRET（≥ 32 字节，可经管理后台写入配置表），否则重启后所有会话失效。
func getJWTSecret() []byte {
	s := strings.TrimSpace(config.Env("GEO_JWT_SECRET", ""))
	if s == "" {
		slog.Warn("未配置 GEO_JWT_SECRET，使用一次性启动密钥（重启后所有会话失效）。" +
			"生产环境建议：export GEO_JWT_SECRET=$(openssl rand -hex 32)")
		buf := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, buf); err != nil {
			slog.Error("JWT 密钥生成失败，无法获取安全随机数", slog.String("err", err.Error()))
			return nil // 调用方需处理 nil
		}
		s = hex.EncodeToString(buf)
	} else if len(s) < 32 {
		// 显式配置但强度不足：明确告警（不阻断启动，避免破坏已有部署；
		// 但安全审计时该警告必须被处理）。
		slog.Warn("GEO_JWT_SECRET 长度不足 32 字节，签名强度偏弱，建议重新生成：" +
			"export GEO_JWT_SECRET=$(openssl rand -hex 32)")
	}
	return []byte(s)
}

var (
	jwtSecretOnce sync.Once
	jwtSecretVal  []byte
)

func jwtSecret() []byte {
	jwtSecretOnce.Do(func() { jwtSecretVal = getJWTSecret() })
	return jwtSecretVal
}

// base64URL 基于 RawURLEncoding 的编解码（JWT 规范）。
func b64uEncode(b []byte) string          { return base64.RawURLEncoding.EncodeToString(b) }
func b64uDecode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// hmacSHA256 标准库实现 HMAC-SHA256。
func hmacSHA256(key, msg []byte) []byte {
	const block = 64
	if len(key) > block {
		s := sha256.Sum256(key)
		key = s[:]
	}
	ipad := make([]byte, block)
	opad := make([]byte, block)
	copy(ipad, key)
	copy(opad, key)
	for i := range block {
		ipad[i] ^= 0x36
		opad[i] ^= 0x5c
	}
	inner := sha256.New()
	inner.Write(ipad)
	inner.Write(msg)
	is := inner.Sum(nil)
	outer := sha256.New()
	outer.Write(opad)
	outer.Write(is)
	return outer.Sum(nil)
}

// signJWT 签发 JWT（header.claims.signature）。
func signJWT(c jwtClaims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	cb, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("jwt claims marshal: %w", err)
	}
	signing := b64uEncode(hb) + "." + b64uEncode(cb)
	sig := hmacSHA256(jwtSecret(), []byte(signing))
	return signing + "." + b64uEncode(sig), nil
}

// parseJWT 校验 JWT 并解析 claims。
func parseJWT(token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("jwt: 格式非法")
	}
	// 验签
	signing := parts[0] + "." + parts[1]
	want := hmacSHA256(jwtSecret(), []byte(signing))
	got, err := b64uDecode(parts[2])
	if err != nil {
		return jwtClaims{}, errors.New("jwt: signature base64 非法")
	}
	if !hmacEqual(want, got) {
		return jwtClaims{}, errors.New("jwt: 签名不匹配")
	}
	// 解析 claims
	cb, err := b64uDecode(parts[1])
	if err != nil {
		return jwtClaims{}, errors.New("jwt: claims base64 非法")
	}
	var c jwtClaims
	if err := json.Unmarshal(cb, &c); err != nil {
		return jwtClaims{}, fmt.Errorf("jwt: claims 解析: %w", err)
	}
	// 过期检查（1 分钟宽容期）。强制要求 exp 存在：
	// 缺少 exp 的 token 一律视为无效，杜绝"无过期时间"的永久令牌。
	if c.Exp <= 0 {
		return jwtClaims{}, errors.New("jwt: 缺少过期时间")
	}
	if time.Now().Unix() > c.Exp+60 {
		return jwtClaims{}, errors.New("jwt: 已过期")
	}
	return c, nil
}

// hmacEqual 防时序攻击的字节比较。
func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	diff := byte(0)
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// ============================================================
// 4. 密码哈希（PBKDF2 + SHA-256 + 16 字节盐 + 310K 迭代）
//    OWASP 建议 PBKDF2-HMAC-SHA256 ≥ 310,000 次迭代（2023+）。
//    verifyPassword 从存储串读取迭代数，旧哈希自动兼容。
// ============================================================

const (
	pbkdf2Iters   = 310_000
	pbkdf2SaltLen = 16
	pbkdf2KeyLen  = 32
)

// hashPassword 生成 PBKDF2 格式字符串：sha256$iters$saltB64$hashB64。
func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}
	dk := pbkdf2SHA256([]byte(password), salt, pbkdf2Iters, pbkdf2KeyLen)
	return fmt.Sprintf("sha256$%d$%s$%s",
		pbkdf2Iters,
		b64uEncode(salt),
		b64uEncode(dk),
	), nil
}

// verifyPassword 校验密码。
func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 {
		return false
	}
	if parts[0] != "sha256" {
		return false
	}
	iters, err := atoi(parts[1])
	if err != nil || iters <= 0 {
		return false
	}
	salt, err := b64uDecode(parts[2])
	if err != nil {
		return false
	}
	want, err := b64uDecode(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iters, len(want))
	return hmacEqual(got, want)
}

// pbkdf2SHA256 标准库实现 PBKDF2-HMAC-SHA256。
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := func(key, data []byte) []byte { return hmacSHA256(key, data) }
	out := make([]byte, 0, keyLen)
	var U, T []byte
	var i uint32 = 1
	for keyLen > 0 {
		U = prf(password, append(salt, byte(i>>24), byte(i>>16), byte(i>>8), byte(i)))
		T = make([]byte, len(U))
		copy(T, U)
		for n := 2; n <= iter; n++ {
			U = prf(password, U)
			for k := range T {
				T[k] ^= U[k]
			}
		}
		out = append(out, T...)
		i++
		keyLen -= len(T)
	}
	return out[:len(out):len(out)]
}

// atoi 极简字符串转整数，避免引入 strconv 循环依赖（其实不依赖，仅保持简洁）。
func atoi(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errors.New("non-digit")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ============================================================
// 5. Context 键与提取
// ============================================================

type ctxKey string

const (
	ctxKeyUser      ctxKey = "auth.user"
	ctxKeyWorkspace ctxKey = "auth.workspace"
	ctxKeyRole      ctxKey = "auth.role"
)

// UserFromContext 从 context 取出已鉴权的用户（未鉴权返回 nil）。
func UserFromContext(ctx context.Context) *User {
	if u, ok := ctx.Value(ctxKeyUser).(*User); ok {
		return u
	}
	return nil
}

// WorkspaceIDFromContext 从 context 取出当前工作区 ID。
func WorkspaceIDFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(ctxKeyWorkspace).(string); ok {
		return s
	}
	return ""
}

// RoleFromContext 取出当前角色（仅在已指定工作区后有效）。
func RoleFromContext(ctx context.Context) Role {
	if r, ok := ctx.Value(ctxKeyRole).(Role); ok {
		return r
	}
	return ""
}

// RequirePermission 权限检查辅助：context 中角色必须拥有该权限。
func RequirePermission(ctx context.Context, p Permission) error {
	r := RoleFromContext(ctx)
	if r == "" {
		return errors.New("未登录或未选择工作区")
	}
	if !HasRolePermission(r, p) {
		return fmt.Errorf("权限不足（需要 %s）", p)
	}
	return nil
}

// ============================================================
// 6. MySQL Store
// ============================================================

// Store 账号/权限持久化（MySQL 实现）。
type Store struct {
	mu  sync.RWMutex
	dsn string
	db  *sql.DB
}

// defaultAuthDSN 默认 MySQL 连接串。
// 注意：账号体系默认不启用（GEO_AUTH_ENABLED=false）；一旦显式启用就必须
// 配置统一 GEO_MYSQL_DSN，绝不静默回退到弱口令默认值（防扫描爆破）。
func defaultAuthDSN() (string, error) {
	d := strings.TrimSpace(config.Env("GEO_MYSQL_DSN", ""))
	if d != "" {
		return d, nil
	}
	return "", errors.New("启用账号体系（GEO_AUTH_ENABLED=true）必须配置 GEO_MYSQL_DSN，例如 geo:pass@tcp(127.0.0.1:3306)/geo?parseTime=true&charset=utf8mb4&loc=Local")
}

// maskDSN 把 DSN 里的密码替换成 ***，避免在日志/响应中泄露凭据。
// user:pass@tcp(host:3306)/db -> user:***@tcp(host:3306)/db
func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	if at := strings.LastIndexByte(dsn, '@'); at > 0 {
		prefix := dsn[:at]
		suffix := dsn[at:]
		if colon := strings.IndexByte(prefix, ':'); colon > 0 {
			return prefix[:colon+1] + "***" + suffix
		}
	}
	return dsn
}

// OpenStore 打开/创建账号数据库。
func OpenStore() (*Store, error) {
	rawDSN, err := defaultAuthDSN()
	if err != nil {
		return nil, err
	}
	dsn := dbprovider.NormalizeMySQLDSN(rawDSN)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}
	dbprovider.ConfigurePool(db, "auth")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("auth db ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, "SET NAMES utf8mb4, sql_mode='STRICT_TRANS_TABLES,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set mysql session: %w", err)
	}

	// 数据库表结构由 deploy/initdb/schema.sql 全量初始化，
	// 应用内不再内嵌 migration。
	return &Store{dsn: dsn, db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// Path 返回数据库 DSN（保持与外部 server.go slog 的兼容性）。
func (s *Store) Path() string { return s.dsn }

// HasUsers 是否存在用户（用于首用户注册引导）。
func (s *Store) HasUsers() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n > 0, err
}

// --- 用户 CRUD ---

// CreateUser 创建用户（事务内创建默认工作区并以 Owner 加入）。
// name 为空时用邮箱前缀；workspaceName 为空时取 "<display_name>'s Workspace"。
func (s *Store) CreateUser(email, password, displayName, workspaceName string) (*User, *Workspace, error) {
	if strings.TrimSpace(email) == "" {
		return nil, nil, errors.New("邮箱不能为空")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, nil, errors.New("邮箱格式非法")
	}
	if err := validatePasswordStrength(password); err != nil {
		return nil, nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, nil, err
	}
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	if workspaceName == "" {
		workspaceName = displayName + "'s Workspace"
	}
	uid := "u_" + util.RandomHexID(8)
	wid := "ws_" + util.RandomHexID(8)
	now := time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"INSERT INTO users(id,email,password_hash,display_name,created_at) VALUES(?,?,?,?,?)",
		uid, email, hash, displayName, now,
	); err != nil {
		return nil, nil, fmt.Errorf("创建用户: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO workspaces(id,name,created_at,owner_id) VALUES(?,?,?,?)",
		wid, workspaceName, now, uid,
	); err != nil {
		return nil, nil, fmt.Errorf("创建工作区: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO memberships(user_id,workspace_id,role,joined_at) VALUES(?,?,?,?)",
		uid, wid, string(RoleOwner), now,
	); err != nil {
		return nil, nil, fmt.Errorf("加入工作区: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	u := &User{
		ID:           uid,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		CreatedAt:    time.Unix(now, 0),
	}
	w := &Workspace{
		ID:        wid,
		Name:      workspaceName,
		CreatedAt: time.Unix(now, 0),
		OwnerID:   uid,
		Plan:      "free",
	}
	return u, w, nil
}

// GetUserByEmail 根据邮箱查用户（找不到返回 nil）。
func (s *Store) GetUserByEmail(email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanUser(s.db.QueryRow(
		"SELECT id,email,password_hash,display_name,created_at,last_login_at,verified,token_version FROM users WHERE email=?",
		email,
	))
}

// GetUserByID 根据 ID 查用户。
func (s *Store) GetUserByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanUser(s.db.QueryRow(
		"SELECT id,email,password_hash,display_name,created_at,last_login_at,verified,token_version FROM users WHERE id=?",
		id,
	))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var (
		u                    User
		createdAt, lastLogin int64
		verified             int
	)
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &createdAt, &lastLogin, &verified, &u.TokenVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	if lastLogin > 0 {
		u.LastLoginAt = time.Unix(lastLogin, 0)
	}
	u.Verified = verified > 0
	return &u, nil
}

// UpdateLastLogin 更新最后登录时间。
func (s *Store) UpdateLastLogin(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE users SET last_login_at=? WHERE id=?", time.Now().Unix(), userID)
	return err
}

// AddMembership 将用户加入工作区（INSERT IGNORE 幂等）。
func (s *Store) AddMembership(userID, workspaceID string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT IGNORE INTO memberships(user_id,workspace_id,role,joined_at) VALUES(?,?,?,?)",
		userID, workspaceID, string(role), time.Now().Unix(),
	)
	return err
}

// UpdatePassword 改密码（同时吊销该用户全部会话：refresh token 删除 +
// token_version +1 使已签发的 access token 立即失效）。
func (s *Store) UpdatePassword(userID, newPassword string) error {
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	h, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE users SET password_hash=?, token_version=token_version+1 WHERE id=?", h, userID); err != nil {
		return err
	}
	// 改密即吊销该用户所有 refresh token，杜绝旧 token 继续换新会话
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE user_id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeUserTokens 手动吊销某用户的全部会话（管理员封禁/安全事件处置）：
// token_version +1 使所有已签发 access token 立即失效，同时清空 refresh token。
func (s *Store) RevokeUserTokens(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE users SET token_version=token_version+1 WHERE id=?", userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE user_id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// validatePasswordStrength 密码强度策略（OWASP 2023 建议）：
//   - 长度 8–128（上限防止超长密码拖慢 PBKDF2 计算，构成 CPU DoS）
//   - 至少含一个字母与一个数字（拒绝纯数字 / 纯字母弱口令）
func validatePasswordStrength(pw string) error {
	if pw == "" {
		return errors.New("密码不能为空")
	}
	if len(pw) < 8 {
		return errors.New("密码至少 8 位")
	}
	if len(pw) > 128 {
		return errors.New("密码长度不能超过 128 位")
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("密码需同时包含字母和数字")
	}
	return nil
}

// VerifyPassword 校验密码（找不到用户或不匹配返回 false）。
func (s *Store) VerifyPassword(email, password string) (*User, bool, error) {
	u, err := s.GetUserByEmail(email)
	if err != nil || u == nil {
		return nil, false, err
	}
	return u, verifyPassword(password, u.PasswordHash), nil
}

// --- 工作区 ---

// ListWorkspacesWithRole 列出用户所属所有工作区及其角色。
func (s *Store) ListWorkspacesWithRole(userID string) ([]WorkspaceWithRole, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`
		SELECT w.id,w.name,w.created_at,w.owner_id,w.plan,m.role
		FROM workspaces w
		INNER JOIN memberships m ON m.workspace_id=w.id
		WHERE m.user_id=?
		ORDER BY m.role='owner' DESC, w.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceWithRole
	for rows.Next() {
		var wwr WorkspaceWithRole
		var createdAt int64
		var role string
		if err := rows.Scan(&wwr.ID, &wwr.Name, &createdAt, &wwr.OwnerID, &wwr.Plan, &role); err != nil {
			return nil, err
		}
		wwr.CreatedAt = time.Unix(createdAt, 0)
		wwr.Role = Role(role)
		out = append(out, wwr)
	}
	return out, rows.Err()
}

// GetUserRoleInWorkspace 获取用户在工作区的角色。
func (s *Store) GetUserRoleInWorkspace(userID, workspaceID string) (Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var role string
	err := s.db.QueryRow(
		"SELECT role FROM memberships WHERE user_id=? AND workspace_id=?",
		userID, workspaceID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return Role(role), err
}

// --- Refresh Tokens ---

// SaveRefreshToken 持久化刷新令牌（用于 revoke 检查）。
func (s *Store) SaveRefreshToken(jti, userID, workspaceID string, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO refresh_tokens(jti,user_id,workspace_id,expires_at,created_at) VALUES(?,?,?,?,?)
		 ON DUPLICATE KEY UPDATE user_id=VALUES(user_id), workspace_id=VALUES(workspace_id), expires_at=VALUES(expires_at), created_at=VALUES(created_at)`,
		jti, userID, workspaceID, expires.Unix(), time.Now().Unix(),
	)
	return err
}

// IsRefreshTokenActive 刷新令牌是否仍有效（存在且未过期）。
func (s *Store) IsRefreshTokenActive(jti, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var expires int64
	err := s.db.QueryRow(
		"SELECT expires_at FROM refresh_tokens WHERE jti=? AND user_id=?",
		jti, userID,
	).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return time.Now().Unix() <= expires, nil
}

// RevokeRefreshToken 登出时作废指定 refresh token。
func (s *Store) RevokeRefreshToken(jti, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM refresh_tokens WHERE jti=? AND user_id=?", jti, userID)
	return err
}

// CleanupExpiredRefresh 清理过期 refresh token（启动/登录时可调用）。
func (s *Store) CleanupExpiredRefresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM refresh_tokens WHERE expires_at < ?", time.Now().Unix())
	return err
}

// --- Admin Audit Log ---

// AppendAuditLog 追加审计记录。
func (s *Store) AppendAuditLog(al *AdminAuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if al.ID == "" {
		al.ID = "audit_" + util.RandomHexID(8)
	}
	if al.Timestamp.IsZero() {
		al.Timestamp = time.Now()
	}
	detailsJSON, _ := json.Marshal(al.Details)
	_, err := s.db.Exec(
		`INSERT INTO admin_audit_log(id,timestamp,actor_id,actor,action,target,details_json,ip,user_agent) VALUES(?,?,?,?,?,?,?,?,?)`,
		al.ID, al.Timestamp.Unix(), al.ActorID, al.Actor, al.Action, al.Target, string(detailsJSON), al.IP, al.UserAgent,
	)
	return err
}

// QueryAuditLog 分页查询审计日志。
func (s *Store) QueryAuditLog(action string, limit, offset int) ([]AdminAuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var (
		rows *sql.Rows
		err  error
	)
	if action != "" {
		rows, err = s.db.Query(`
			SELECT id,timestamp,actor_id,actor,action,target,details_json,ip,user_agent
			FROM admin_audit_log WHERE action=? ORDER BY timestamp DESC LIMIT ? OFFSET ?`,
			action, limit, offset)
	} else {
		rows, err = s.db.Query(`
			SELECT id,timestamp,actor_id,actor,action,target,details_json,ip,user_agent
			FROM admin_audit_log ORDER BY timestamp DESC LIMIT ? OFFSET ?`,
			limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminAuditLog
	for rows.Next() {
		var a AdminAuditLog
		var ts int64
		var detailsJSON sql.NullString
		if err := rows.Scan(&a.ID, &ts, &a.ActorID, &a.Actor, &a.Action, &a.Target, &detailsJSON, &a.IP, &a.UserAgent); err != nil {
			return nil, err
		}
		a.Timestamp = time.Unix(ts, 0)
		if detailsJSON.Valid && detailsJSON.String != "" {
			_ = json.Unmarshal([]byte(detailsJSON.String), &a.Details)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ============================================================
// 7. 高层认证服务（Auth Service）
// ============================================================

// Service 封装鉴权业务逻辑，供 HTTP handler 调用。
type Service struct {
	store   *Store
	enabled bool // 是否启用账号体系（GEO_AUTH_ENABLED=true）

	// 登录失败防护：按规范化邮箱计数，连续失败达上限后临时锁定（防暴力破解）
	loginMu    sync.Mutex
	loginFails map[string]loginFailState
}

// loginFailState 登录失败状态。
type loginFailState struct {
	count int   // 连续失败次数
	until int64 // 锁定截止时间（Unix 秒，0=未锁定）
}

const (
	maxLoginFails   = 5       // 连续失败次数上限
	loginLockWindow = 15 * 60 // 锁定时长（秒）
	loginFailWindow = 10 * 60 // 失败计数窗口（秒）
)

// NewService 构建认证服务。
func NewService() (*Service, error) {
	enabled := strings.EqualFold(strings.TrimSpace(config.Env("GEO_AUTH_ENABLED", "")), "true")
	if !enabled {
		slog.Info("账号体系未启用（未设置 GEO_AUTH_ENABLED）：API 匿名放行，管理接口 403。生产请启用账号体系。")
		return &Service{store: nil, enabled: false, loginFails: map[string]loginFailState{}}, nil
	}
	st, err := OpenStore()
	if err != nil {
		return nil, err
	}
	_ = st.CleanupExpiredRefresh()
	slog.Info("账号体系已启用", slog.String("db", maskDSN(st.Path())))
	return &Service{store: st, enabled: true, loginFails: map[string]loginFailState{}}, nil
}

// Enabled 账号体系是否启用。
func (svc *Service) Enabled() bool { return svc.enabled }

// Store 返回底层存储（未启用时为 nil）。
func (svc *Service) Store() *Store { return svc.store }

// BootstrapAdmin 启动时初始化管理员账号（不依赖注册接口）。
//
// 读取 GEO_ADMIN_EMAIL / GEO_ADMIN_PASSWORD：
//   - 账号体系未启用、或 GEO_ADMIN_EMAIL 未配置 → 直接跳过（返回 nil）；
//   - GEO_ADMIN_EMAIL 已配置但密码为空 → 返回错误（由调用方告警，不阻断启动）；
//   - 该邮箱已存在 → 跳过（幂等，重复启动不会覆盖已有账号）；
//   - 不存在 → 创建管理员（Owner + 默认工作区 "Default Workspace"）。
//
// 密码需满足强度要求（≥8 位且同时含字母与数字，见 validatePasswordStrength）。
func (svc *Service) BootstrapAdmin() error {
	if svc == nil || !svc.enabled || svc.store == nil {
		return nil
	}
	email := strings.TrimSpace(strings.ToLower(os.Getenv("GEO_ADMIN_EMAIL")))
	if email == "" {
		return nil
	}
	password := os.Getenv("GEO_ADMIN_PASSWORD")
	if password == "" {
		return fmt.Errorf("GEO_ADMIN_EMAIL 已配置但 GEO_ADMIN_PASSWORD 为空，跳过管理员初始化")
	}
	existing, err := svc.store.GetUserByEmail(email)
	if err != nil {
		return fmt.Errorf("查询管理员邮箱失败: %w", err)
	}
	if existing != nil {
		slog.Info("管理员账号已存在，跳过初始化", slog.String("email", email))
		return nil
	}
	if _, _, err := svc.store.CreateUser(email, password, "Administrator", "Default Workspace"); err != nil {
		return fmt.Errorf("创建初始管理员失败: %w", err)
	}
	slog.Info("已创建初始管理员（Owner）", slog.String("email", email))
	return nil
}

// Close 关闭存储。
func (svc *Service) Close() error {
	if svc.store != nil {
		return svc.store.Close()
	}
	return nil
}

// Login 邮箱 + 密码登录，返回 TokenPair + 工作区列表。
// workspaceID 指定首个令牌作用的工作区（缺省时取用户 Owner 级或首个工作区）。
func (svc *Service) Login(email, password, workspaceID string, ip, ua string) (*TokenPair, *User, []WorkspaceWithRole, error) {
	if svc.store == nil {
		return nil, nil, nil, errors.New("账号体系未启用")
	}
	// 暴力破解防护：连续失败达上限后临时锁定
	key := strings.ToLower(strings.TrimSpace(email))
	now := time.Now().Unix()
	svc.loginMu.Lock()
	st, exists := svc.loginFails[key]
	if exists {
		if st.until > now {
			svc.loginMu.Unlock()
			slog.Warn("登录尝试被临时锁定", slog.String("email", key), slog.String("ip", ip))
			return nil, nil, nil, errors.New("失败次数过多，账号已临时锁定，请稍后再试")
		}
		// 锁定已过期，重置计数
		if now-st.until > loginFailWindow {
			delete(svc.loginFails, key)
		}
	}
	svc.loginMu.Unlock()

	u, ok, err := svc.store.VerifyPassword(email, password)
	if err != nil {
		// 数据库层错误：记录详情，响应仅给通用信息，避免泄露内部细节
		slog.Error("登录校验失败", slog.String("email", key), slog.String("ip", ip), slog.String("err", err.Error()))
		return nil, nil, nil, errors.New("登录失败，请稍后重试")
	}
	if !ok || u == nil {
		svc.loginMu.Lock()
		cur := svc.loginFails[key]
		// 计数窗口内才累计；超窗重置
		if cur.count > 0 && now-cur.until > loginFailWindow {
			cur = loginFailState{}
		}
		cur.count++
		if cur.count >= maxLoginFails {
			cur.until = now + loginLockWindow
			cur.count = 0
			slog.Warn("登录失败次数过多，触发临时锁定", slog.String("email", key), slog.String("ip", ip))
		} else {
			cur.until = now
		}
		svc.loginFails[key] = cur
		svc.loginMu.Unlock()
		return nil, nil, nil, errors.New("邮箱或密码错误")
	}
	// 登录成功：清除失败计数
	svc.loginMu.Lock()
	delete(svc.loginFails, key)
	svc.loginMu.Unlock()

	_ = svc.store.UpdateLastLogin(u.ID)
	u.LastLoginAt = time.Now()
	_ = svc.store.AppendAuditLog(&AdminAuditLog{
		ActorID: u.ID, Actor: u.Email, Action: "user.login", Target: u.Email,
		IP: ip, UserAgent: ua,
	})

	wss, err := svc.store.ListWorkspacesWithRole(u.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(wss) == 0 {
		return nil, nil, nil, errors.New("用户不属于任何工作区")
	}

	// 选定工作区
	var role Role
	if workspaceID == "" {
		workspaceID = wss[0].ID
		role = wss[0].Role
	} else {
		for _, w := range wss {
			if w.ID == workspaceID {
				role = w.Role
				break
			}
		}
		if role == "" {
			return nil, nil, nil, errors.New("不属于该工作区")
		}
	}

	pair, err := svc.issueTokenPair(u, workspaceID, role)
	if err != nil {
		return nil, nil, nil, err
	}
	return pair, u, wss, nil
}

// issueTokenPair 签发 access + refresh 双令牌。
func (svc *Service) issueTokenPair(u *User, wid string, role Role) (*TokenPair, error) {
	now := time.Now()
	jtiAccess := util.RandomHexID(12)
	jtiRefresh := util.RandomHexID(12)

	access, err := signJWT(jwtClaims{
		Sub:         u.ID,
		Email:       u.Email,
		WorkspaceID: wid,
		Role:        role,
		JTI:         jtiAccess,
		Iat:         now.Unix(),
		Exp:         now.Add(accessTokenTTL).Unix(),
		Type:        "access",
		V:           u.TokenVersion,
	})
	if err != nil {
		return nil, err
	}
	refresh, err := signJWT(jwtClaims{
		Sub:         u.ID,
		Email:       u.Email,
		WorkspaceID: wid,
		Role:        role,
		JTI:         jtiRefresh,
		Iat:         now.Unix(),
		Exp:         now.Add(refreshTokenTTL).Unix(),
		Type:        "refresh",
	})
	if err != nil {
		return nil, err
	}
	if err := svc.store.SaveRefreshToken(jtiRefresh, u.ID, wid, now.Add(refreshTokenTTL)); err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:    access,
		RefreshToken:   refresh,
		TokenType:      "Bearer",
		ExpiresIn:      int(accessTokenTTL.Seconds()),
		RefreshExpires: int(refreshTokenTTL.Seconds()),
	}, nil
}

// Refresh 刷新令牌：拿有效 refresh token 换新双令牌。
// 会吊销旧 refresh token，保证一次性使用（避免泄露后滥用）。
func (svc *Service) Refresh(rawRefresh string, ip, ua string) (*TokenPair, *User, error) {
	if svc.store == nil {
		return nil, nil, errors.New("账号体系未启用")
	}
	c, err := parseJWT(rawRefresh)
	if err != nil {
		// 不返回签名/解析细节，避免帮助攻击者探测
		return nil, nil, errors.New("刷新令牌无效")
	}
	if c.Type != "refresh" {
		return nil, nil, errors.New("令牌类型错误（非 refresh）")
	}
	active, err := svc.store.IsRefreshTokenActive(c.JTI, c.Sub)
	if err != nil {
		return nil, nil, err
	}
	if !active {
		return nil, nil, errors.New("刷新令牌已失效")
	}
	// 吊销旧 refresh（失败则拒绝本次刷新，防止被盗 token 无限重放）
	if err := svc.store.RevokeRefreshToken(c.JTI, c.Sub); err != nil {
		slog.Error("吊销旧刷新令牌失败", slog.String("jti", c.JTI), slog.String("user", c.Sub), slog.String("err", err.Error()))
		return nil, nil, errors.New("刷新失败，请稍后重试")
	}

	u, err := svc.store.GetUserByID(c.Sub)
	if err != nil || u == nil {
		slog.Error("刷新令牌对应用户不存在", slog.String("user", c.Sub), slog.String("err", func() string {
			if err != nil {
				return err.Error()
			}
			return "not found"
		}()))
		return nil, nil, errors.New("刷新失败，请稍后重试")
	}
	role := c.Role
	if role == "" && c.WorkspaceID != "" {
		r, _ := svc.store.GetUserRoleInWorkspace(u.ID, c.WorkspaceID)
		role = r
	}
	pair, err := svc.issueTokenPair(u, c.WorkspaceID, role)
	if err != nil {
		return nil, nil, err
	}
	_ = svc.store.AppendAuditLog(&AdminAuditLog{
		ActorID: u.ID, Actor: u.Email, Action: "user.token_refresh", Target: u.Email,
		IP: ip, UserAgent: ua,
	})
	return pair, u, nil
}

// Logout 注销指定 refresh token。
func (svc *Service) Logout(rawRefresh string, userID string) error {
	if svc.store == nil {
		return nil
	}
	c, err := parseJWT(rawRefresh)
	if err != nil || c.Type != "refresh" {
		return nil
	}
	return svc.store.RevokeRefreshToken(c.JTI, userID)
}

// SwitchWorkspace 切换当前工作区（签发新双令牌）。
func (svc *Service) SwitchWorkspace(userID, workspaceID string) (*TokenPair, error) {
	if svc.store == nil {
		return nil, errors.New("账号体系未启用")
	}
	u, err := svc.store.GetUserByID(userID)
	if err != nil || u == nil {
		return nil, errors.New("用户不存在")
	}
	role, err := svc.store.GetUserRoleInWorkspace(userID, workspaceID)
	if err != nil || role == "" {
		return nil, errors.New("不属于该工作区")
	}
	return svc.issueTokenPair(u, workspaceID, role)
}

// ============================================================
// 8. HTTP 中间件：JWT 鉴权 + workspace 上下文注入 + 权限检查
// ============================================================

// AuthNResponse 统一鉴权错误响应（复用 middleware 同名结构签名）。
type AuthNResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// MiddlewareConfig 中间件配置。
type MiddlewareConfig struct {
	Svc *Service
	// PublicPaths 公开路径（跳过鉴权）
	PublicPaths map[string]bool
}

// WithAuthN 鉴权中间件（认证）。
// 顺序：账号体系启用时优先 Bearer JWT → Cookie → 公开路径放行 → 401；
// 账号体系未启用时全部放行（本地开发 / 反向代理鉴权场景，生产必须启用账号体系）。
// 认证通过后把 user/workspace/role 注入 context。
func WithAuthN(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	public := cfg.PublicPaths
	if public == nil {
		public = map[string]bool{}
	}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// 账号体系未启用 → 无鉴权放行（本地开发 / 反向代理场景）。
			// 管理类接口另有 requireDataAdmin 等角色守卫，未启用账号体系时一律 403。
			if cfg.Svc == nil || !cfg.Svc.Enabled() {
				h.ServeHTTP(w, r)
				return
			}

			// 公开路径放行。
			// SPA 架构：非 /api/ 路径一律公开——前端 HTML/JS/CSS 壳与前端路由路径
			// （/landing、/dashboard、/admin/login 等）无敏感数据，handleWebSPA 负责
			// 静态资源 + SPA fallback；数据与操作鉴权全部收敛在 /api/*（公开端点走下方
			// 白名单，其余需 JWT，管理类另有 requireDataAdmin 等角色守卫）。
			// 注意：仅注册/登录/刷新/登出这 4 个 auth 端点免鉴权；其余 /api/v1/auth/*
			// （me / change-password / workspace/* / admin/audit）需要 JWT 上下文，
			// 若整前缀豁免会让它们拿到 nil user → 一律 401/403（账号管理功能全废）。
			if public[path] || isServerPublicPath(path) ||
				!strings.HasPrefix(path, "/api/") ||
				path == "/api/v1/auth/register" || path == "/api/v1/auth/login" ||
				path == "/api/v1/auth/refresh" || path == "/api/v1/auth/logout" ||
				strings.HasPrefix(path, "/api/v1/billing/webhook/") {
				h.ServeHTTP(w, r)
				return
			}

			// 解析 JWT access token
			auth := r.Header.Get("Authorization")
			var raw string
			if strings.HasPrefix(auth, "Bearer ") {
				raw = strings.TrimSpace(auth[7:])
			}
			if raw == "" {
				// 支持 cookie（SPA 前端 httpOnly 可选模式）
				if c, _ := r.Cookie("geo_access_token"); c != nil {
					raw = c.Value
				}
			}
			if raw == "" {
				writeErr(w, http.StatusUnauthorized, "需要登录", "AUTH_REQUIRED")
				return
			}
			c, err := parseJWT(raw)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "登录状态已失效", "AUTH_TOKEN_INVALID")
				return
			}
			if c.Type != "access" {
				writeErr(w, http.StatusUnauthorized, "令牌类型错误", "AUTH_WRONG_TOKEN_TYPE")
				return
			}
			// 验证用户存在
			u, err := cfg.Svc.Store().GetUserByID(c.Sub)
			if err != nil || u == nil {
				writeErr(w, http.StatusUnauthorized, "用户不存在", "AUTH_USER_NOT_FOUND")
				return
			}
			// 会话版本校验：改密/手动吊销后 token_version 递增，
			// 旧 access token（v 小于当前版本）立即失效。
			if c.V != u.TokenVersion {
				writeErr(w, http.StatusUnauthorized, "登录状态已失效，请重新登录", "AUTH_TOKEN_REVOKED")
				return
			}
			// 注入 context
			ctx := context.WithValue(r.Context(), ctxKeyUser, u)
			ctx = context.WithValue(ctx, ctxKeyWorkspace, c.WorkspaceID)
			ctx = context.WithValue(ctx, ctxKeyRole, c.Role)
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isServerPublicPath 全局公开路径（保持与 middleware.go 一致）。
// 探活端点必须放行，否则启用 API Key 后 K8s/云平台探活会 401 误判宕机。
func isServerPublicPath(path string) bool {
	if path == "/" || path == "/api/v1/health" || path == "/api/v1/ready" ||
		path == "/healthz" || path == "/readyz" {
		return true
	}
	// 前端 dist 静态资源（go:embed 嵌入的 chunks / 图标 / manifest 等）
	// 必须在 auth 之前放行，否则浏览器加载 HTML 后拉 JS/CSS 会 401 → React 不执行 → 白屏
	if strings.HasPrefix(path, "/assets/") ||
		path == "/favicon.ico" || path == "/favicon.svg" ||
		path == "/manifest.webmanifest" || path == "/safari-pinned-tab.svg" ||
		path == "/index.html" || path == "/robots.txt" || path == "/legal/bot" {
		return true
	}
	// 帮助中心公开接口（帮助文章/新手引导，无需登录即可查看与完成）。
	// 前缀匹配覆盖 /api/v1/help/articles/{id} 带 ID 的详情路径。
	if strings.HasPrefix(path, "/api/v1/help/") {
		return true
	}
	// 公开定价方案详情（/api/v1/pricing/plans/{id}，无敏感数据）。
	if strings.HasPrefix(path, "/api/v1/pricing/plans/") {
		return true
	}
	// 外部系统提交大模型对话：走独立密钥鉴权（X-GEO-External-Key），
	// 外部系统无平台 JWT，前缀放行覆盖尾斜杠变体，鉴权交由 checkExternalKey。
	if strings.HasPrefix(path, "/api/v1/external/submissions") {
		return true
	}
	return false
}

// RequirePermissionMiddleware 返回权限检查（授权）中间件。
// 要求角色必须包含该 permission，否则 403。
func RequirePermissionMiddleware(p Permission) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := RequirePermission(r.Context(), p); err != nil {
				writeErr(w, http.StatusForbidden, err.Error(), "PERMISSION_DENIED")
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

func writeErr(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(AuthNResponse{Error: msg, Code: code})
}

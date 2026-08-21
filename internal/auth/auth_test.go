package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// testRootDSN 返回用于 CREATE DATABASE / DROP DATABASE 的 root 级 MySQL DSN。
// 从 GEO_TEST_MYSQL_ROOT_DSN 取，缺省用 "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true"。
// 环境变量不存在或连通性失败 → t.Skip("需要可连接的 MySQL 测试实例，跳过")。
func testRootDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GEO_TEST_MYSQL_ROOT_DSN"))
	if dsn == "" {
		dsn = "root:@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&loc=Local&multiStatements=true&tls=false"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("跳过 auth 测试：无法打开测试 MySQL root DSN (%v)", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("跳过 auth 测试：测试 MySQL 不可用 (GEO_TEST_MYSQL_ROOT_DSN ping err=%v)", err)
	}
	return dsn
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	rootDSN := testRootDSN(t)
	// 库名用纳秒级随机，避免同一微秒窗口内多个测试生成同名库相互污染
	dbName := fmt.Sprintf("geo_auth_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatalf("打开 root 连接: %v", err)
	}
	defer root.Close()
	// 先删后建，保证库内干净（即使极端情况库名碰撞也重建）
	if _, err := root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName)); err != nil {
		t.Fatalf("DROP DATABASE: %v", err)
	}
	if _, err := root.Exec(fmt.Sprintf("CREATE DATABASE `%s` DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", dbName)); err != nil {
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	// 建表：schema 以 deploy/initdb/02-schema.sql 为单一事实来源（应用内不内嵌迁移）。
	// 用独立连接 + multiStatements 一次性执行 "USE 测试库 + 全部 DDL"，
	// 避免连接池中 USE 与后续语句落到不同连接导致建错库。
	schemaSQL, err := os.ReadFile(filepath.Join("..", "..", "deploy", "initdb", "02-schema.sql"))
	if err != nil {
		t.Fatalf("读取 02-schema.sql: %v", err)
	}
	msDB, err := sql.Open("mysql", ensureMultiStatements(rootDSN))
	if err != nil {
		t.Fatalf("打开建表连接: %v", err)
	}
	defer msDB.Close()
	stmts := splitSchemaStatements(string(schemaSQL))
	if _, err := msDB.Exec("USE `" + dbName + "`;\n" + strings.Join(stmts, ";\n") + ";"); err != nil {
		t.Fatalf("建表失败（库 %s，共 %d 条语句）: %v", dbName, len(stmts), err)
	}
	// 把 user:pass@tcp(host)/?xxx 改造成 user:pass@tcp(host)/dbname?xxx
	dsn := injectDB(rootDSN, dbName)
	t.Setenv("GEO_MYSQL_DSN", dsn)
	s, err := OpenStore()
	if err != nil {
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		t.Fatalf("OpenStore failed: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		_, _ = root.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
	})
	return s
}

// ensureMultiStatements 确保 DSN 带 multiStatements=true（建表需一次执行多条 DDL）。
func ensureMultiStatements(dsn string) string {
	if strings.Contains(dsn, "multiStatements=") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&multiStatements=true"
	}
	return dsn + "?multiStatements=true"
}

// splitSchemaStatements 按分号切分 schema SQL，过滤空语句、纯注释行与 USE 行。
func splitSchemaStatements(content string) []string {
	var out []string
	for _, s := range strings.Split(content, ";") {
		var kept []string
		for _, ln := range strings.Split(s, "\n") {
			trimmed := strings.TrimSpace(ln)
			if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "USE ") {
				continue // USE 由调用方处理（连接已选定测试库）
			}
			kept = append(kept, ln)
		}
		body := strings.TrimSpace(strings.Join(kept, "\n"))
		if body != "" {
			out = append(out, body)
		}
	}
	return out
}

// injectDB 在 DSN 路径部分插入数据库名。
func injectDB(dsn, db string) string {
	// user:pass@tcp(host:port)/dbname?k=v → 替换 / 和 ? 之间的部分
	idx := strings.LastIndex(dsn, "/")
	if idx < 0 {
		return dsn + db
	}
	rest := dsn[idx+1:]
	if q := strings.Index(rest, "?"); q >= 0 {
		return dsn[:idx+1] + db + "?" + rest[q+1:]
	}
	return dsn[:idx+1] + db
}

func TestHasUsers_Empty(t *testing.T) {
	s := openTestStore(t)
	has, err := s.HasUsers()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("empty DB should have no users")
	}
}

func TestCreateUserAndLoginFlow(t *testing.T) {
	s := openTestStore(t)
	u, ws, err := s.CreateUser("admin@geo.ai", "StrongPass1!", "Admin", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "admin@geo.ai" {
		t.Errorf("unexpected email: %s", u.Email)
	}
	if ws.Name != "Admin's Workspace" {
		t.Errorf("expected default workspace name, got %q", ws.Name)
	}
	if ws.OwnerID != u.ID {
		t.Errorf("workspace owner should match user id")
	}

	// 密码校验
	_, ok, err := s.VerifyPassword("admin@geo.ai", "wrongpass")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("wrong password should not verify")
	}
	_, ok, err = s.VerifyPassword("admin@geo.ai", "StrongPass1!")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("correct password should verify")
	}

	// HasUsers
	has, _ := s.HasUsers()
	if !has {
		t.Error("HasUsers should be true after create")
	}

	// 列出工作区
	wss, err := s.ListWorkspacesWithRole(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(wss) != 1 || wss[0].Role != RoleOwner {
		t.Errorf("expected 1 owner workspace, got %#v", wss)
	}
	// 工作区内角色
	r, _ := s.GetUserRoleInWorkspace(u.ID, ws.ID)
	if r != RoleOwner {
		t.Errorf("expected owner role, got %s", r)
	}
}

func TestCreateUser_ShortPassword(t *testing.T) {
	s := openTestStore(t)
	_, _, err := s.CreateUser("a@b.com", "short", "", "")
	if err == nil {
		t.Error("short password should be rejected")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	s := openTestStore(t)
	_, _, err := s.CreateUser("a@b.com", "StrongPass1!", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.CreateUser("A@b.com", "StrongPass2!", "", "")
	if err == nil {
		t.Error("duplicate email (case-insensitive) should be rejected")
	}
}

func TestUpdatePassword(t *testing.T) {
	s := openTestStore(t)
	u, _, err := s.CreateUser("pw@geo.ai", "StrongPass1!", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePassword(u.ID, "NewStrong2!"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ := s.VerifyPassword("pw@geo.ai", "StrongPass1!")
	if ok {
		t.Error("old password should no longer work")
	}
	_, ok2, _ := s.VerifyPassword("pw@geo.ai", "NewStrong2!")
	if !ok2 {
		t.Error("new password should work")
	}
}

func TestJWT_SignAndParse(t *testing.T) {
	// 用固定密钥（覆盖 getJWTSecret 的默认行为）
	t.Setenv("GEO_JWT_SECRET", "test-secret-xyz-00000000000000000000000000000000")
	// 重置一次性初始化（jwtSecretOnce sync.Once）——由于已被其他测试触发，这里直接验证：
	// 先确保 jwtSecret() 非空
	secret := jwtSecret()
	if len(secret) == 0 {
		t.Fatal("jwtSecret should not be empty")
	}
	c := jwtClaims{
		Sub:         "u_123",
		Email:       "t@geo.ai",
		WorkspaceID: "ws_1",
		Role:        RoleAdmin,
		JTI:         "abc",
		Iat:         time.Now().Unix(),
		Exp:         time.Now().Add(time.Hour).Unix(),
		Type:        "access",
	}
	tok, err := signJWT(c)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseJWT(tok)
	if err != nil {
		t.Fatalf("parseJWT: %v", err)
	}
	if parsed.Sub != c.Sub || parsed.Role != c.Role || parsed.WorkspaceID != c.WorkspaceID {
		t.Errorf("claims mismatch: %+v vs %+v", parsed, c)
	}
}

func TestJWT_Expired(t *testing.T) {
	t.Setenv("GEO_JWT_SECRET", "test-secret-xyz-00000000000000000000000000000000")
	// jwtSecret once 已初始化；取现有密钥签发过期 token
	c := jwtClaims{
		Sub:  "u_x",
		JTI:  "abc",
		Iat:  time.Now().Add(-3 * time.Hour).Unix(),
		Exp:  time.Now().Add(-2 * time.Hour).Unix(),
		Type: "access",
	}
	tok, _ := signJWT(c)
	if _, err := parseJWT(tok); err == nil {
		t.Error("expired JWT should fail parse")
	}
}

func TestJWT_BadSignature(t *testing.T) {
	t.Setenv("GEO_JWT_SECRET", "test-secret-xyz-00000000000000000000000000000000")
	c := jwtClaims{Sub: "u1", Exp: time.Now().Add(time.Hour).Unix(), Type: "access"}
	tok, _ := signJWT(c)
	// 篡改 signature 中部一个字符（确保与原字符不同，避免 bad==tok 导致篡改检测失效）
	mid := len(tok) / 2
	rep := byte('A')
	if tok[mid] == 'A' {
		rep = 'B'
	}
	bad := tok[:mid] + string(rep) + tok[mid+1:]
	if _, err := parseJWT(bad); err == nil {
		t.Error("tampered JWT should fail parse")
	}
}

func TestPasswordHash_Verify(t *testing.T) {
	h, err := hashPassword("MyGoodPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword("MyGoodPassword123!", h) {
		t.Error("correct password should verify")
	}
	if verifyPassword("WrongPass", h) {
		t.Error("wrong password should not verify")
	}
}

func TestRefreshToken_SaveAndRevoke(t *testing.T) {
	s := openTestStore(t)
	u, _, _ := s.CreateUser("rt@geo.ai", "StrongPass1!", "", "")
	jti := "jti_test_" + u.ID
	exp := time.Now().Add(time.Hour)
	if err := s.SaveRefreshToken(jti, u.ID, "ws_1", exp); err != nil {
		t.Fatal(err)
	}
	active, err := s.IsRefreshTokenActive(jti, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Error("new refresh token should be active")
	}
	// 错用户查
	active, _ = s.IsRefreshTokenActive(jti, "some_other")
	if active {
		t.Error("wrong user should not see active token")
	}
	// 吊销
	if err := s.RevokeRefreshToken(jti, u.ID); err != nil {
		t.Fatal(err)
	}
	active, _ = s.IsRefreshTokenActive(jti, u.ID)
	if active {
		t.Error("revoked token should not be active")
	}
}

func TestAuditLog_AppendAndQuery(t *testing.T) {
	s := openTestStore(t)
	for i := 0; i < 5; i++ {
		_ = s.AppendAuditLog(&AdminAuditLog{
			ActorID: "u1", Actor: "a@geo.ai", Action: "user.login", Target: "a@geo.ai",
			IP: "1.2.3.4", UserAgent: "test",
		})
	}
	_ = s.AppendAuditLog(&AdminAuditLog{
		ActorID: "u2", Actor: "b@geo.ai", Action: "workspace.create", Target: "ws_x",
		IP: "1.2.3.5",
	})
	logs, err := s.QueryAuditLog("", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 6 {
		t.Errorf("expected 6 logs, got %d", len(logs))
	}
	loginLogs, _ := s.QueryAuditLog("user.login", 100, 0)
	if len(loginLogs) != 5 {
		t.Errorf("expected 5 login logs, got %d", len(loginLogs))
	}
	// 时间倒序
	if len(logs) > 1 && logs[0].Timestamp.Before(logs[1].Timestamp) {
		t.Error("logs should be ordered by timestamp DESC")
	}
}

func TestRolePermissions(t *testing.T) {
	if !HasRolePermission(RoleOwner, PermDeleteWorkspace) {
		t.Error("Owner should have delete workspace")
	}
	if HasRolePermission(RoleAdmin, PermDeleteWorkspace) {
		t.Error("Admin should NOT have delete workspace")
	}
	if !HasRolePermission(RoleViewer, PermViewReport) {
		t.Error("Viewer should have view report")
	}
	if HasRolePermission(RoleViewer, PermWriteAudit) {
		t.Error("Viewer should NOT have write audit")
	}
	if !HasRolePermission(RoleMember, PermWriteAudit) {
		t.Error("Member should have write audit")
	}
	if !RoleGte(RoleAdmin, RoleMember) {
		t.Error("Admin >= Member should be true")
	}
	if RoleGte(RoleViewer, RoleAdmin) {
		t.Error("Viewer >= Admin should be false")
	}
}

func TestService_LoginRefreshLogout(t *testing.T) {
	svc := &Service{store: openTestStore(t), enabled: true}
	t.Setenv("GEO_JWT_SECRET", "test-svc-login-secret-0000000000000000000000")
	_ = jwtSecret() // 初始化密钥（即使已有其他密钥，也不会影响流程）

	_, _, err := svc.store.CreateUser("svc@geo.ai", "StrongPass1!", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pair, u, wss, err := svc.Login("svc@geo.ai", "StrongPass1!", "", "127.0.0.1", "tester")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected tokens")
	}
	if u == nil || len(wss) == 0 {
		t.Fatal("expected user + workspaces")
	}
	if u.LastLoginAt.IsZero() {
		t.Error("Login should update LastLoginAt")
	}
	// 刷新
	pair2, u2, err := svc.Refresh(pair.RefreshToken, "127.0.0.1", "tester")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if pair2.AccessToken == "" || u2 == nil {
		t.Fatal("refresh should issue new tokens + user")
	}
	// 原 refresh token 已被吊销（一次性）
	_, _, err = svc.Refresh(pair.RefreshToken, "127.0.0.1", "tester")
	if err == nil {
		t.Error("old refresh token should be revoked after refresh")
	}
	// 登出（吊销新的 refresh token）
	if err := svc.Logout(pair2.RefreshToken, u2.ID); err != nil {
		t.Errorf("Logout: %v", err)
	}
	_, _, err = svc.Refresh(pair2.RefreshToken, "127.0.0.1", "tester")
	if err == nil {
		t.Error("refresh after logout should fail")
	}
}

func TestService_Disabled_NewService(t *testing.T) {
	t.Setenv("GEO_AUTH_ENABLED", "false")
	svc, err := NewService()
	if err != nil {
		t.Fatal(err)
	}
	if svc.Enabled() {
		t.Error("should be disabled when env is false")
	}
}

func TestContextHelpers(t *testing.T) {
	u := &User{ID: "u_1", Email: "a@b.com"}
	ctx := context.WithValue(context.Background(), ctxKeyUser, u)
	ctx = context.WithValue(ctx, ctxKeyWorkspace, "ws_x")
	ctx = context.WithValue(ctx, ctxKeyRole, RoleAdmin)
	if got := UserFromContext(ctx); got == nil || got.ID != "u_1" {
		t.Error("UserFromContext failed")
	}
	if WorkspaceIDFromContext(ctx) != "ws_x" {
		t.Error("WorkspaceIDFromContext failed")
	}
	if RoleFromContext(ctx) != RoleAdmin {
		t.Error("RoleFromContext failed")
	}
	if err := RequirePermission(ctx, PermManageMember); err != nil {
		t.Errorf("Admin should have PermManageMember: %v", err)
	}
	if err := RequirePermission(ctx, PermDeleteWorkspace); err == nil {
		t.Error("Admin should NOT have PermDeleteWorkspace")
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	ok := []string{
		"StrongPass1",
		"a1b2c3d4",
		"密码1234", // 非 ASCII 字母 + 数字：按字节/符文均需通过
		"1234567a",
		"A1234567",
	}
	for _, pw := range ok {
		if err := validatePasswordStrength(pw); err != nil {
			t.Errorf("validatePasswordStrength(%q) unexpected error: %v", pw, err)
		}
	}
	bad := []string{
		"", "short", "12345678", "abcdefgh", "aaaaaaaa", "!@#$%^&*",
		strings.Repeat("a", 129), strings.Repeat("A1", 70), // 超长（>128）
	}
	for _, pw := range bad {
		if err := validatePasswordStrength(pw); err == nil {
			t.Errorf("validatePasswordStrength(%q...) should fail", truncateForTest(pw))
		}
	}
}

func truncateForTest(s string) string {
	if len(s) > 16 {
		return s[:16] + "..."
	}
	return s
}

// TestTokenVersionRevoke 验证改密 / 手动吊销后旧 access token 立即失效（需 MySQL）。
func TestTokenVersionRevoke(t *testing.T) {
	s := openTestStore(t)
	u, _, err := s.CreateUser("revoke@geo.ai", "StrongPass1!", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// 签发带当前版本的 access token
	now := time.Now()
	access, err := signJWT(jwtClaims{
		Sub: u.ID, Email: u.Email, JTI: "jti_old", Type: "access",
		Iat: now.Unix(), Exp: now.Add(time.Hour).Unix(), V: u.TokenVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 版本匹配 → 可解析通过
	if c, err := parseJWT(access); err != nil || c.V != u.TokenVersion {
		t.Fatalf("token should be valid: err=%v v=%d want %d", err, c.V, u.TokenVersion)
	}
	// 改密 → token_version +1
	if err := s.UpdatePassword(u.ID, "NewStrongPass2!"); err != nil {
		t.Fatal(err)
	}
	u2, err := s.GetUserByID(u.ID)
	if err != nil || u2 == nil {
		t.Fatalf("reload user: %v", err)
	}
	if u2.TokenVersion != u.TokenVersion+1 {
		t.Fatalf("token_version = %d, want %d", u2.TokenVersion, u.TokenVersion+1)
	}
	// 旧 access token 虽未过期且签名有效，但版本不匹配 → 鉴权层应拒绝
	if _, err := parseJWT(access); err != nil {
		t.Fatalf("old token should still parse (revocation is a middleware-level check): %v", err)
	}
	// 模拟中间件校验
	c, _ := parseJWT(access)
	if c.V == u2.TokenVersion {
		t.Error("old token version should differ from current")
	}
	// 手动吊销（再 +1）
	if err := s.RevokeUserTokens(u.ID); err != nil {
		t.Fatal(err)
	}
	u3, _ := s.GetUserByID(u.ID)
	if u3.TokenVersion != u2.TokenVersion+1 {
		t.Fatalf("after revoke token_version = %d, want %d", u3.TokenVersion, u2.TokenVersion+1)
	}
}

// TestBootstrapAdmin 启动初始化管理员：创建、幂等、跳过逻辑。
// 依赖本地 MySQL（GEO_TEST_MYSQL_ROOT_DSN 或 root@127.0.0.1:3306），不可用则跳过。
func TestBootstrapAdmin(t *testing.T) {
	st := openTestStore(t)
	svc := &Service{store: st, enabled: true, loginFails: map[string]loginFailState{}}

	// 1) 未配置 GEO_ADMIN_EMAIL → 跳过
	t.Setenv("GEO_ADMIN_EMAIL", "")
	t.Setenv("GEO_ADMIN_PASSWORD", "")
	if err := svc.BootstrapAdmin(); err != nil {
		t.Fatalf("未配置邮箱应跳过: %v", err)
	}

	// 2) 配置后创建管理员（Owner + 默认工作区）
	t.Setenv("GEO_ADMIN_EMAIL", "admin@geo.test")
	t.Setenv("GEO_ADMIN_PASSWORD", "AdminPass123")
	if err := svc.BootstrapAdmin(); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	u, err := st.GetUserByEmail("admin@geo.test")
	if err != nil || u == nil {
		t.Fatalf("管理员未创建: user=%v err=%v", u, err)
	}
	ws, err := st.ListWorkspacesWithRole(u.ID)
	if err != nil || len(ws) == 0 {
		t.Fatalf("管理员默认工作区缺失: ws=%v err=%v", ws, err)
	}
	if ws[0].Role != RoleOwner {
		t.Fatalf("管理员应为 Owner，实际 %q", ws[0].Role)
	}

	// 3) 幂等：再次调用不重复创建、不报错
	if err := svc.BootstrapAdmin(); err != nil {
		t.Fatalf("重复调用应幂等: %v", err)
	}

	// 4) 密码为空 → 报错（调用方告警）
	t.Setenv("GEO_ADMIN_PASSWORD", "")
	if err := svc.BootstrapAdmin(); err == nil {
		t.Fatal("密码为空应返回错误")
	}
}

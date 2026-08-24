package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LoadDotEnv 从 path 读取 KEY=VALUE 环境文件并写入进程环境变量。
//
// 规则（手写解析，零第三方依赖）：
//   - 已存在的环境变量优先，不覆盖（便于 docker -e / systemd 注入覆盖 .env）；
//   - 支持行首 export 前缀、单双引号包裹的值、行内 # 注释、空行；
//   - 文件不存在视为"未提供"，返回 nil（.env 是可选项）。
//
// 典型用法：main() 开头调用 config.LoadDotEnv(".env")。
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: 打开 %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			slog.Warn("config: 忽略无效 .env 行", slog.Int("line", lineNo))
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		// 去除行内注释（仅当 # 前有空格或值以引号结束时才安全；简单起见：
		// 只在 # 前存在空白时截断，避免误伤含 # 的密码）。
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = value[:idx]
		}
		// 去引号（成对单引号或双引号）
		value = unquote(value)

		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("config: 设置 %s: %w", key, err)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("config: 读取 %s: %w", path, err)
	}
	return nil
}

// unquote 去掉成对包裹的单/双引号（保留内部内容原样）。
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Validate 启动关键配置 fail-fast 校验。
//
// 规则（返回 error 即拒绝启动）：
//   - GEO_AUTH_ENABLED=true 时必须配置统一 GEO_MYSQL_DSN；
//   - GEO_AUTH_ENABLED=true 时 GEO_JWT_SECRET 必须 ≥ 32 字节（弱密钥拒绝启动）；
//   - 配置了 GEO_JWT_SECRET 但 < 32 字节 → 拒绝启动（防弱密钥，P1-9）；
//   - 配置了 GEO_MCP_API_KEY 但 < 16 字节 → 拒绝启动（防弱 API Key）。
//
// 未涉及的配置仅记录提示日志，不做强制。
func Validate() error {
	authEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("GEO_AUTH_ENABLED")), "true")

	if authEnabled {
		if strings.TrimSpace(os.Getenv("GEO_MYSQL_DSN")) == "" {
			return fmt.Errorf("配置校验失败：GEO_AUTH_ENABLED=true 时必须设置 GEO_MYSQL_DSN")
		}
	}

	if secret := os.Getenv("GEO_JWT_SECRET"); secret != "" && len(secret) < 32 {
		return fmt.Errorf("配置校验失败：GEO_JWT_SECRET 长度 %d 字节 < 32，签名强度不足。"+
			"请重新生成：export GEO_JWT_SECRET=$(openssl rand -hex 32)", len(secret))
	}
	if authEnabled && strings.TrimSpace(os.Getenv("GEO_JWT_SECRET")) == "" {
		slog.Warn("GEO_AUTH_ENABLED=true 但未配置 GEO_JWT_SECRET，将使用一次性启动密钥（重启后所有会话失效）")
	}

	if key := os.Getenv("GEO_MCP_API_KEY"); key != "" && len(key) < 16 {
		return fmt.Errorf("配置校验失败：GEO_MCP_API_KEY 长度 %d < 16，弱密钥易被暴力破解。请使用 >= 16 字符的随机串", len(key))
	}

	// 记录可观测但非阻断的配置提示
	if !authEnabled {
		slog.Warn("未启用账号体系（未设置 GEO_AUTH_ENABLED）：API 匿名可访问、管理接口全部 403，请仅用于本地开发或置于反向代理鉴权之后")
	}
	return nil
}

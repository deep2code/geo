// Package dsnutil 提供 MySQL DSN 规范化工具，不依赖任何业务包，
// 避免 config / dbprovider 之间的 import cycle。
package dsnutil

import (
	"strings"

	"github.com/go-sql-driver/mysql"
)

// NormalizeMySQLDSN 为 DSN 幂等追加性能与安全性参数（缺省即最佳实践）：
//
//   - interpolateParams=true  客户端就地替换占位符，避免 PREPARE/EXECUTE 2 次 roundtrip
//   - maxAllowedPacket=64M   允许存储 MEDIUMTEXT / MEDIUMBLOB 最大 16MB
//   - timeout=10s            连接握手超时，避免 DNS/网络 hang
//   - readTimeout=30s        单次读取超时，防 SQL 挂起
//   - writeTimeout=30s       单次写入超时，防大事务/大导入卡死
//   - parseTime + charset=utf8mb4 + loc=Local + tls=preferred
//
// 注意：不注入 multiStatements=true —— 该参数放大 SQL 注入面，仅建表/导入路径
// 需要时由调用方在 DSN 中显式声明。业务查询一律走参数化单语句。
//
// 已存在的同名参数不会被覆盖，方便用户自定义（例如企业生产 tls=true 场景）。
// tls=preferred：若服务端支持 TLS 则加密传输；不支持时回退明文（兼容本地内网
// 未启用 TLS 的 MySQL），比强制 tls=false 更安全。生产环境建议显式配置 tls=true。
//
// 使用 mysql.ParseDSN 正确解析，避免密码中的 '?' 被误当作查询串起点截断。
func NormalizeMySQLDSN(raw string) string {
	if raw == "" {
		return raw
	}
	required := map[string]string{
		"parseTime":         "true",
		"charset":           "utf8mb4",
		"loc":               "Local",
		"interpolateParams": "true",
		"maxAllowedPacket":  "67108864",
		"tls":               "preferred",
		"timeout":           "10s",
		"readTimeout":       "30s",
		"writeTimeout":      "30s",
	}

	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		// 解析失败时退回字符串拼接（保留向后兼容，但无法处理密码含 '?' 的场景）。
		return appendQueryParams(raw, required)
	}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	// 逐项检查 struct 字段：go-sql-driver 会把这些键解析进 Config 字段而
	// 不留在 cfg.Params，按 cfg.Params 判断"用户已设置"恒为假——注入参数
	// 经 FormatDSN 追加在尾部后生效，会静默覆盖用户的显式配置
	// （如生产 tls=true 被降级为 preferred、readTimeout=60s 被改 30s）。
	if !cfg.ParseTime {
		cfg.Params["parseTime"] = "true"
	}
	if !cfg.InterpolateParams {
		cfg.Params["interpolateParams"] = "true"
	}
	if cfg.MaxAllowedPacket == 0 {
		cfg.Params["maxAllowedPacket"] = "67108864"
	}
	if cfg.Timeout == 0 {
		cfg.Params["timeout"] = "10s"
	}
	if cfg.ReadTimeout == 0 {
		cfg.Params["readTimeout"] = "30s"
	}
	if cfg.WriteTimeout == 0 {
		cfg.Params["writeTimeout"] = "30s"
	}
	if cfg.TLSConfig == "" {
		cfg.Params["tls"] = "preferred"
	}
	// loc：仅在未显式设置（nil/UTC）时补 Local；显式其它时区（如
	// Asia/Shanghai）保留用户值
	if cfg.Loc == nil || cfg.Loc.String() == "UTC" {
		cfg.Params["loc"] = "Local"
	}
	// charset：驱动无公开的"是否显式设置"信号（未写也会填默认排序规则），
	// 项目库表统一 utf8mb4，维持强制注入
	cfg.Params["charset"] = "utf8mb4"
	return cfg.FormatDSN()
}

// appendQueryParams 是 NormalizeMySQLDSN 的降级路径：当 mysql.ParseDSN 失败时，
// 用字符串方式追加参数。注意：密码中含 '?' 时结果可能不正确。
func appendQueryParams(raw string, params map[string]string) string {
	qStart := strings.Index(raw, "?")
	sep := "?"
	query := ""
	if qStart >= 0 {
		sep = "&"
		query = raw[qStart+1:]
		raw = raw[:qStart]
	}
	has := map[string]bool{}
	pairs := []string{}
	if query != "" {
		for _, p := range strings.Split(query, "&") {
			if p == "" {
				continue
			}
			pairs = append(pairs, p)
			if eq := strings.Index(p, "="); eq > 0 {
				has[strings.ToLower(p[:eq])] = true
			}
		}
	}
	for k, v := range params {
		if !has[strings.ToLower(k)] {
			pairs = append(pairs, k+"="+v)
		}
	}
	if len(pairs) == 0 {
		return raw
	}
	return raw + sep + strings.Join(pairs, "&")
}

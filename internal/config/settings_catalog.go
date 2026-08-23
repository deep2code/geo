package config

import "strings"

// extraCatalog 手工补充的配置注册表条目。
//
// 与 settings_catalog_gen.go（自动扫描生成）合并为完整注册表。
// 这里补充：
//  1. 引擎 API Key / BaseURL / Model —— 代码中通过 engineEnvKeys 动态拼接读取，
//     脚本扫描不到，需手工登记；
//  2. RequiresRestart 标注 —— 启动时一次性消费的连接类变量（HTTP 端口、Redis、
//     支付凭据、LLM base 等），DB 修改后需重启进程才生效。
func extraCatalog() []Setting {
	engineKeys := []string{
		"GEO_OPENAI_KEY", "GEO_PERPLEXITY_KEY", "GEO_GEMINI_KEY", "GEO_CLAUDE_KEY",
		"GEO_GROK_KEY", "GEO_QWEN_KEY", "GEO_GLM_KEY", "GEO_DEEPSEEK_KEY",
		"GEO_KIMI_KEY", "GEO_ERNIE_KEY", "GEO_DOUBAO_KEY", "GEO_XIAOMI_KEY",
		"GEO_XUNFEI_KEY", "GEO_YUANBAO_KEY",
	}
	engineBases := []string{"GEO_ERNIE_BASE", "GEO_QWEN_BASE", "GEO_KIMI_BASE", "GEO_DOUBAO_BASE"}
	engineModels := []string{"GEO_ERNIE_MODEL", "GEO_QWEN_MODEL", "GEO_KIMI_MODEL", "GEO_DOUBAO_MODEL"}

	out := make([]Setting, 0, len(engineKeys)+len(engineBases)+len(engineModels)+8)
	for _, k := range engineKeys {
		out = append(out, Setting{Key: k, Category: "engines", Type: "secret", IsSecret: true})
	}
	// 各引擎联网搜索开关（模拟真实用户 App 联网提问，弥补 API 无网测量差）。
	// 默认全部开启（各主流模型均已支持内置联网；端点不支持自动降级无网查询）。
	for _, k := range engineKeys {
		wsKey := strings.TrimSuffix(k, "_KEY") + "_WEB_SEARCH"
		out = append(out, Setting{
			Key: wsKey, Category: "engines", Type: "bool", DefaultValue: "true",
			Description: "引擎联网搜索开关（模拟 App 联网提问；端点不支持自动降级无网查询）",
		})
	}
	// GEO_JWT_SECRET / GEO_AUTH_ENABLED 为引导类（2026-08-21 起）：运行参数只读 DB 后，
	// 账号体系开关与签名密钥必须能通过环境变量一次性引导启动（否则未启用 AUTH 时后台 403，
	// 无法从 DB 开启，形成死锁）。后台对其只读展示，改环境变量 + 重启生效。
	out = append(out, Setting{
		Key: "GEO_JWT_SECRET", Category: "auth", Type: "secret", IsSecret: true, IsBootstrap: true,
		Description:     "JWT 签名密钥（≥32 字节，引导类：环境变量设置 + 重启生效；改动后所有会话失效）",
		RequiresRestart: true,
	})
	out = append(out, Setting{
		Key: "GEO_AUTH_ENABLED", Category: "auth", Type: "bool", IsBootstrap: true,
		Description: "账号体系开关（引导类：环境变量设置 + 重启生效；true 启用 JWT/RBAC）",
	})
	for _, k := range engineBases {
		out = append(out, Setting{Key: k, Category: "engines", Description: "引擎 API 基地址（默认走内置官方地址）"})
	}
	for _, k := range engineModels {
		out = append(out, Setting{Key: k, Category: "engines", Description: "引擎模型名（默认走内置默认模型）"})
	}

	// 启动时一次性消费、修改后需重启的连接类变量
	restartKeys := []Setting{
		{Key: "GEO_AUDIT_SAMPLES", Category: "server", Type: "int", DefaultValue: "1",
			Description: "品牌审计采样次数（每个查询×引擎重复查询 N 次多数票判定，1=单次；建议 3；单个请求可用 profile.samples 覆盖）",
			RequiresRestart: true},
		{Key: "GEO_PORT", Category: "server", Description: "HTTP 监听端口", Type: "int", RequiresRestart: true},
		{Key: "GEO_MCP_PORT", Category: "mcp", Description: "MCP Server 监听端口", Type: "int", RequiresRestart: true},
		// Redis 与 MySQL DSN 同属"连接引导类"（IsBootstrap）：环境变量引导（compose 内
		// 用服务名 redis:6379，本地用 127.0.0.1:6379），避免"运行参数只读 DB"导致部署环境失效。
		{Key: "GEO_REDIS_ADDR", Category: "queue", Description: "Redis 地址（asynq 队列）", IsBootstrap: true, RequiresRestart: true},
		{Key: "GEO_REDIS_PASSWORD", Category: "queue", Description: "Redis 密码", IsSecret: true, IsBootstrap: true, RequiresRestart: true},
		{Key: "GEO_REDIS_DB", Category: "queue", Description: "Redis DB 编号", Type: "int", RequiresRestart: true},
		// Meilisearch（外部中文全文检索引擎；工商库搜索已迁移至此，MariaDB 不支持 MySQL 的
		// ngram 解析器）。与 Redis 同属"连接引导类"（IsBootstrap）：环境变量引导，避免
		// "运行参数只读 DB" 导致部署环境失效。URL 留空则工商搜索降级 MariaDB LIKE。
		{Key: "GEO_MEILISEARCH_URL", Category: "search", Description: "Meilisearch 地址（工商库中文全文检索；留空则降级 LIKE）", IsBootstrap: true, RequiresRestart: true},
		{Key: "GEO_MEILISEARCH_API_KEY", Category: "search", Description: "Meilisearch API Key（实例无鉴权则留空）", Type: "secret", IsSecret: true, IsBootstrap: true, RequiresRestart: true},
		{Key: "GEO_LLM_BASE", Category: "llm", Description: "默认 LLM API 基地址", RequiresRestart: true},
		{Key: "GEO_LLM_MODEL", Category: "llm", Description: "默认 LLM 模型名", RequiresRestart: true},
		{Key: "GEO_LLM_KEY", Category: "llm", Description: "默认 LLM API Key", Type: "secret", IsSecret: true},
		{Key: "GEO_LLM_BUDGET_USD", Category: "llm", Description: "LLM 月度预算上限（USD，0=不限）", Type: "float"},
		{Key: "GEO_SMTP_HOST", Category: "mail", Description: "SMTP 服务器", RequiresRestart: true},
		{Key: "GEO_SMTP_PORT", Category: "mail", Description: "SMTP 端口", Type: "int", RequiresRestart: true},
		{Key: "GEO_SMTP_USER", Category: "mail", Description: "SMTP 用户名", RequiresRestart: true},
		{Key: "GEO_SMTP_PASSWORD", Category: "mail", Description: "SMTP 密码", Type: "secret", IsSecret: true, RequiresRestart: true},
		{Key: "GEO_LLM_KEY_OPENAI", Category: "llm", Description: "OpenAI 兼容密钥（LLM 管理器）", Type: "secret", IsSecret: true},
		{Key: "GEO_LLM_MODEL_OPENAI", Category: "llm", Description: "OpenAI 兼容模型", RequiresRestart: true},
		{Key: "GEO_OPENAPI_KEY", Category: "admin", Description: "开放测量 API 鉴权 Key（X-GEO-API-Key）", Type: "secret", IsSecret: true},
		// 运行时读取（handler 内 config.Env 每次调用），修改即时生效，无需重启。
		{Key: "GEO_EXTERNAL_API_KEY", Category: "admin", Description: "外部提交接口鉴权 Key（X-GEO-External-Key；留空则该接口 401）", Type: "secret", IsSecret: true},
		// 启动时一次性读取（中间件/httputil），修改后需重启。
		{Key: "GEO_CORS_ORIGINS", Category: "general", Description: "CORS 白名单（逗号分隔 Origin；默认仅 localhost）", RequiresRestart: true},
		{Key: "GEO_TRUSTED_PROXIES", Category: "general", Description: "可信代理 IP/CIDR（逗号分隔；用于正确解析客户端 IP）", RequiresRestart: true},
	}
	out = append(out, restartKeys...)
	return out
}

package config

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
	// GEO_JWT_SECRET 非例外（用户原则：仅数据库连接 + 初始管理员账号走环境变量）：
	// 放配置表管理（DB > 环境变量 > 默认值）。签名密钥改动会使已签发 JWT 全部失效，
	// 故标注需重启；长度校验见 config.Validate（env）与 InitSettings（DB 覆盖后）。
	out = append(out, Setting{
		Key: "GEO_JWT_SECRET", Category: "auth", Type: "secret", IsSecret: true,
		Description:     "JWT 签名密钥（≥32 字节）。存配置表，修改后需重启且所有会话失效",
		RequiresRestart: true,
	})
	for _, k := range engineBases {
		out = append(out, Setting{Key: k, Category: "engines", Description: "引擎 API 基地址（默认走内置官方地址）"})
	}
	for _, k := range engineModels {
		out = append(out, Setting{Key: k, Category: "engines", Description: "引擎模型名（默认走内置默认模型）"})
	}

	// 启动时一次性消费、修改后需重启的连接类变量
	restartKeys := []Setting{
		{Key: "GEO_PORT", Category: "server", Description: "HTTP 监听端口", Type: "int", RequiresRestart: true},
		{Key: "GEO_MCP_PORT", Category: "mcp", Description: "MCP Server 监听端口", Type: "int", RequiresRestart: true},
		{Key: "GEO_REDIS_ADDR", Category: "queue", Description: "Redis 地址（asynq 队列）", RequiresRestart: true},
		{Key: "GEO_REDIS_PASSWORD", Category: "queue", Description: "Redis 密码", IsSecret: true, RequiresRestart: true},
		{Key: "GEO_REDIS_DB", Category: "queue", Description: "Redis DB 编号", Type: "int", RequiresRestart: true},
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
	}
	out = append(out, restartKeys...)
	return out
}

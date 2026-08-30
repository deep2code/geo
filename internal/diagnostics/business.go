package diagnostics

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"my-geo/internal/dbprovider"
	"my-geo/internal/models"
	"my-geo/pkg/geo"
)

// dsnHostPortRe 解析 MySQL DSN 中的 host:port：user:pass@tcp(host:port)/db?...
var dsnHostPortRe = regexp.MustCompile(`@tcp\(([^):]+):(\d+)\)`)

// BusinessHealth 探测关键业务路径是否真的能工作。
//
// 涵盖：评分管线、内容分析、规则化优化、LLM 改写（若已配置）、
// 以及三个 MySQL 模块（离线工商库 / 审计历史 / China-Check 缓存）的可达性。
// 除 LLM 探测会发起一次真实改写调用外，其余均为本地计算或 TCP 探活，开销可控。
func BusinessHealth(ctx context.Context, engine *geo.Engine) []CheckResult {
	results := make([]CheckResult, 0, 8)
	results = append(results, probeScore(ctx, engine))
	results = append(results, probeAnalyze(ctx, engine))
	results = append(results, probeOptimizeRule(ctx, engine))
	results = append(results, probeLLM(ctx, engine))
	for _, mod := range []dbprovider.ModuleKind{
		dbprovider.ModuleOfflineCompanies,
		dbprovider.ModuleAuditHistory,
		dbprovider.ModuleChinaCheckCache,
	} {
		results = append(results, probeDB(ctx, mod))
	}
	return results
}

// probeScore 验证评分管线产出合法评分与明细。
func probeScore(_ context.Context, engine *geo.Engine) CheckResult {
	start := time.Now()
	res := CheckResult{Name: "评分管线", Category: CategoryBusiness}
	sample := "北京是中国的首都。生成式引擎优化（GEO）旨在提升内容被 AI 搜索引擎引用的概率，" +
		"常用策略包括补充权威引用、统计数据和结构化表达。"
	score, bd := engine.Score(sample)
	switch {
	case score < 0 || score > 100:
		res.Status = SeverityError
		res.Message = fmt.Sprintf("评分越界：%.1f（应在 0–100）", score)
	case len(bd) == 0:
		res.Status = SeverityError
		res.Message = "评分明细为空，评分管线可能未初始化"
	default:
		res.Status = SeverityOK
		res.Message = fmt.Sprintf("评分管线正常（示例得分 %.1f，%d 个维度）", score, len(bd))
	}
	res.DurationMs = sinceMs(start)
	return res
}

// probeAnalyze 验证内容分析返回有效结构（词数 > 0）。
func probeAnalyze(_ context.Context, engine *geo.Engine) CheckResult {
	start := time.Now()
	res := CheckResult{Name: "内容分析管线", Category: CategoryBusiness}
	a := engine.Analyze("生成式引擎优化是一种面向 AI 搜索引擎的内容优化方法。")
	if a == nil {
		res.Status = SeverityError
		res.Message = "分析返回 nil，分析管线异常"
		res.DurationMs = sinceMs(start)
		return res
	}
	if a.WordCount <= 0 {
		res.Status = SeverityWarn
		res.Message = "分析返回词数为 0，可能分词异常"
	} else {
		res.Status = SeverityOK
		res.Message = fmt.Sprintf("分析管线正常（词数 %d）", a.WordCount)
	}
	res.DurationMs = sinceMs(start)
	return res
}

// probeOptimizeRule 验证优化管线能产出结构化合法结果（规则化路径，不依赖 LLM）。
func probeOptimizeRule(ctx context.Context, engine *geo.Engine) CheckResult {
	start := time.Now()
	res := CheckResult{Name: "优化管线", Category: CategoryBusiness}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := engine.Optimize(cctx, &models.OptimizationRequest{
		Content:  "生成式引擎优化能提升品牌在 AI 搜索中的可见度。",
		Language: "zh",
	})
	if err != nil {
		res.Status = SeverityError
		res.Message = "优化管线执行失败"
		res.Detail = err.Error()
		res.DurationMs = sinceMs(start)
		return res
	}
	if strings.TrimSpace(resp.OptimizedContent) == "" {
		res.Status = SeverityError
		res.Message = "优化返回空内容"
		res.DurationMs = sinceMs(start)
		return res
	}
	if resp.ScoreAfter < 0 || resp.ScoreAfter > 100 {
		res.Status = SeverityWarn
		res.Message = fmt.Sprintf("优化后评分越界：%.1f", resp.ScoreAfter)
	} else {
		res.Status = SeverityOK
		res.Message = fmt.Sprintf("优化管线正常（优化后评分 %.1f）", resp.ScoreAfter)
	}
	res.DurationMs = sinceMs(start)
	return res
}

// probeLLM 验证 LLM 改写业务端到端可用（仅在已配置可用 Provider 时发起一次真实调用）。
func probeLLM(ctx context.Context, engine *geo.Engine) CheckResult {
	res := CheckResult{Name: "LLM 改写业务", Category: CategoryBusiness}
	if !engine.LLMAvailable() {
		res.Status = SeverityInfo
		res.Message = "跳过：未配置可用 LLM（规则化优化仍可用，改写类能力不可用）"
		return res
	}
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := engine.Optimize(cctx, &models.OptimizationRequest{
		Content:  "请用一句话说明什么是生成式引擎优化（GEO）。",
		Language: "zh",
	})
	if err != nil {
		res.Status = SeverityError
		res.Message = "LLM 改写业务失败"
		res.Detail = err.Error()
		res.DurationMs = sinceMs(start)
		return res
	}
	if strings.TrimSpace(resp.OptimizedContent) == "" {
		res.Status = SeverityError
		res.Message = "LLM 改写返回空内容"
		res.DurationMs = sinceMs(start)
		return res
	}
	res.Status = SeverityOK
	res.Message = "LLM 改写业务正常（端到端调用成功）"
	res.DurationMs = sinceMs(start)
	return res
}

// probeDB 探测指定 MySQL 模块的可达性（TCP 探活，不鉴权不写数据）。
func probeDB(ctx context.Context, mod dbprovider.ModuleKind) CheckResult {
	name := dbModuleName(mod)
	res := CheckResult{Name: "数据库可达性：" + name, Category: CategoryBusiness}
	if !dbprovider.EnabledFor(mod) {
		res.Status = SeverityInfo
		res.Message = "模块已禁用，跳过"
		return res
	}
	dsn := dbprovider.DSNFor(mod)
	if dsn == "" {
		res.Status = SeverityWarn
		res.Message = "未配置 DSN，将使用内置默认值（运行时可能连接失败）"
		return res
	}
	host, port, err := parseMySQLDSN(dsn)
	if err != nil {
		res.Status = SeverityWarn
		res.Message = "DSN 解析失败，跳过可达性探测"
		res.Detail = err.Error()
		return res
	}
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(cctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		res.Status = SeverityError
		res.Message = fmt.Sprintf("无法连接 %s:%s（%s）", host, port, name)
		res.Detail = err.Error()
		res.DurationMs = sinceMs(start)
		return res
	}
	_ = conn.Close()
	res.Status = SeverityOK
	res.Message = fmt.Sprintf("可连接 %s:%s（%s）", host, port, name)
	res.DurationMs = sinceMs(start)
	return res
}

// dbModuleName 返回模块的友好中文名。
func dbModuleName(mod dbprovider.ModuleKind) string {
	switch mod {
	case dbprovider.ModuleOfflineCompanies:
		return "离线工商库"
	case dbprovider.ModuleAuditHistory:
		return "审计历史库"
	case dbprovider.ModuleChinaCheckCache:
		return "China-Check 缓存库"
	default:
		return string(mod)
	}
}

// parseMySQLDSN 从 MySQL DSN 中提取 host 与 port。
// 支持：user:pass@tcp(127.0.0.1:3306)/dbname?params
func parseMySQLDSN(dsn string) (host, port string, err error) {
	m := dsnHostPortRe.FindStringSubmatch(dsn)
	if m == nil {
		return "", "", fmt.Errorf("无法从 DSN 解析 @tcp(host:port)：%s", maskDSN(dsn))
	}
	return m[1], m[2], nil
}

// maskDSN 脱敏 DSN 中的密码，用于日志/详情展示。
func maskDSN(dsn string) string {
	// 先定位 @ 再向前找用户名/密码分隔符：":secret@tcp(...)"（空用户名 DSN）
	// 的首个 ':' 在位置 0，旧判断 i > 0 会跳过脱敏把密码原样带进日志
	at := strings.Index(dsn, "@")
	if at < 0 {
		return dsn
	}
	if i := strings.LastIndex(dsn[:at], ":"); i >= 0 {
		return dsn[:i] + ":***" + dsn[at:]
	}
	return dsn
}

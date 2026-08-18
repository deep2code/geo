// Package main 是 GEO 命令行工具入口。
//
// 子命令参考 geo-optimizer-skill 的 CLI 设计：
//
//	geo optimize  优化内容
//	geo score     评分
//	geo analyze   分析信号
//	geo strategies 列出策略
//	geo serve     启动 API 服务
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"my-geo/internal/config"
	"my-geo/internal/models"
	"my-geo/internal/server"
	"my-geo/pkg/geo"
)

// 版本信息，通过 ldflags 注入：
//
//	go build -ldflags "-X main.version=v1.2.3 -X main.commit=$(git rev-parse --short HEAD) -X 'main.buildAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
var (
	version = "dev"   // 语义化版本号（release 时由 CI 注入）
	commit  = "none"  // git commit 短哈希
	buildAt = "unknown" // 构建时间（UTC ISO8601）
)

func main() {
	// 先加载 .env（可选，文件不存在时静默跳过），使其中配置（含日志级别）先于后续初始化生效。
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "警告: 加载 .env 失败:", err)
	}
	initLogger()
	// 同步构建信息到 server 包（/metrics 的 geo_build_info 与 geo --version 保持一致）。
	server.SetBuildInfo(version, commit)
	// 启动关键配置 fail-fast 校验（弱密钥/缺 DSN 拒绝启动）。
	if err := config.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

// initLogger 初始化 slog 日志处理器。
// 支持环境变量：
//   - GEO_LOG_LEVEL: debug | info | warn | error（默认 info）
//   - GEO_LOG_FORMAT: text | json（默认 text；K8s/Docker 建议用 json）
//   - 自动检测 K8s 环境（KUBERNETES_SERVICE_HOST）时默认 JSON
func initLogger() {
	var level slog.Level = slog.LevelInfo
	if v := strings.TrimSpace(os.Getenv("GEO_LOG_LEVEL")); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			slog.Warn("GEO_LOG_LEVEL 无效，使用默认级别 info", slog.String("value", v), slog.Any("error", err))
		}
	}
	format := strings.TrimSpace(os.Getenv("GEO_LOG_FORMAT"))
	if format == "" {
		// K8s 环境自动切 JSON
		if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
			format = "json"
		} else {
			format = "text"
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "geo",
		Short: "GEO 生成式引擎优化工具",
		Long: `GEO (Generative Engine Optimization) 生成式引擎优化系统。
优化内容使其更容易被 AI 搜索引擎（ChatGPT、Perplexity、Gemini、Claude）引用。

基于 Princeton GEO 论文 (KDD 2024) 的 9 种优化策略，
融合 AutoGEO (ICLR 2026) 的 GEO/GEU 双评分体系。`,
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, buildAt),
	}
	root.AddCommand(
		newOptimizeCmd(),
		newScoreCmd(),
		newAnalyzeCmd(),
		newStrategiesCmd(),
		newServeCmd(),
		newBrandCmd(),
		// 向后兼容：保留旧的顶层 brand-* 命令（已废弃，建议改用 geo brand <sub>）
		withDeprecated(buildBrandAuditCmd("brand-audit"), "use 'geo brand audit' instead"),
		withDeprecated(buildBrandCacheCmd("brand-cache"), "use 'geo brand cache' instead"),
		withDeprecated(buildBrandDBCmd("brand-db"), "use 'geo brand db' instead"),
		newMCPServerCmd(),
		newReadinessCmd(),
		newCrawlabilityCmd(),
		newTopSourceCmd(),
		newVerticalCmd(),
		newLocalSEOCmd(),
		newExternalSignalsCmd(),
		newAutoRewriteCmd(),
		newDiscoverCmd(),
		newDriftCmd(),
		newRulesCmd(),
		newCostCmd(),
		newEvaluateCmd(),
	)
	return root
}

// withDeprecated 标记命令为废弃（运行时仍可用，仅打印警告），用于平滑迁移。
func withDeprecated(cmd *cobra.Command, msg string) *cobra.Command {
	cmd.Deprecated = msg
	return cmd
}

// newBrandCmd brand 命令组：将品牌域子命令归组到 `geo brand <sub>`。
func newBrandCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "brand",
		Short: "品牌域命令（审计 / 工商库 / 缓存）",
		Long:  "品牌域命令集合：可见度审计、离线工商库管理、China-Check 缓存管理。",
	}
	cmd.AddCommand(
		buildBrandAuditCmd("audit"),
		buildBrandCacheCmd("cache"),
		buildBrandDBCmd("db"),
	)
	return cmd
}

// llmFlags 通用 LLM 配置 flags。
func llmFlags(cmd *cobra.Command) {
	cmd.Flags().String("llm-key", "", "LLM API Key（OpenAI 兼容），不设则仅规则化预处理")
	cmd.Flags().String("llm-base", "", "LLM API Base URL（默认 https://api.openai.com）")
	cmd.Flags().String("llm-model", "", "LLM 模型名（默认 gpt-4o-mini）")
}

// buildEngine 根据 flags 构建 Engine，flag 未设时回退到环境变量。
//
// 环境变量：GEO_LLM_KEY / GEO_LLM_BASE / GEO_LLM_MODEL
func buildEngine(cmd *cobra.Command) *geo.Engine {
	key, _ := cmd.Flags().GetString("llm-key")
	base, _ := cmd.Flags().GetString("llm-base")
	model, _ := cmd.Flags().GetString("llm-model")
	if key == "" {
		key = config.Env("GEO_LLM_KEY", "")
	}
	if base == "" {
		base = config.Env("GEO_LLM_BASE", "")
	}
	if model == "" {
		model = config.Env("GEO_LLM_MODEL", "")
	}
	// 月度预算（USD）：优先 --budget-usd，回退 GEO_LLM_BUDGET_USD 环境变量。
	budget, _ := cmd.Flags().GetFloat64("budget-usd")
	if budget <= 0 {
		if v := os.Getenv("GEO_LLM_BUDGET_USD"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				budget = f
			}
		}
	}
	return geo.New(geo.WithOpenAI(key, base, model), geo.WithBudgetUSD(budget))
}

// readContent 读取内容：-f 文件 / --content / stdin。
func readContent(cmd *cobra.Command) (string, error) {
	file, _ := cmd.Flags().GetString("file")
	content, _ := cmd.Flags().GetString("content")

	if content != "" {
		return content, nil
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("读取文件失败: %w", err)
		}
		return string(b), nil
	}
	// 从 stdin 读取
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", fmt.Errorf("请通过 -f <文件>、--content <内容> 或管道提供内容")
}

func writeOutput(cmd *cobra.Command, content string) error {
	out, _ := cmd.Flags().GetString("output")
	if out != "" {
		return os.WriteFile(out, []byte(content), 0644)
	}
	fmt.Println(content)
	return nil
}

// newOptimizeCmd optimize 子命令。
func newOptimizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "优化内容，提升 AI 搜索引擎可见度",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readContent(cmd)
			if err != nil {
				return err
			}
			engine := buildEngine(cmd)
			if err := applyRulesFlag(cmd, engine); err != nil {
				return err
			}

			req := &models.OptimizationRequest{Content: content}

			if title, _ := cmd.Flags().GetString("title"); title != "" {
				req.Title = title
			}
			if url, _ := cmd.Flags().GetString("url"); url != "" {
				req.URL = url
			}
			if domain, _ := cmd.Flags().GetString("domain"); domain != "" {
				req.DomainType = models.DomainType(domain)
			}
			if engines, _ := cmd.Flags().GetStringArray("engine"); len(engines) > 0 {
				for _, e := range engines {
					req.TargetEngines = append(req.TargetEngines, models.EngineType(e))
				}
			}
			if strats, _ := cmd.Flags().GetStringArray("strategy"); len(strats) > 0 {
				for _, s := range strats {
					req.Strategies = append(req.Strategies, models.StrategyType(s))
				}
			}
			if company, _ := cmd.Flags().GetString("company"); company != "" {
				req.Enterprise = &models.Enterprise{
					CompanyName: company,
					ProductName: productNameFlag(cmd),
					Description: companyDescFlag(cmd),
				}
			}

			resp, err := engine.Optimize(context.Background(), req)
			if err != nil {
				return err
			}

			// 若指定 --output 则写入优化内容，否则打印完整 JSON 报告
			if out, _ := cmd.Flags().GetString("output"); out != "" {
				if err := os.WriteFile(out, []byte(resp.OptimizedContent), 0644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "优化内容已写入 %s\n", out)
			} else {
				b, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Println(string(b))
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "输入文件路径")
	cmd.Flags().String("content", "", "直接输入内容")
	cmd.Flags().StringP("output", "o", "", "输出文件路径（仅写优化后内容）")
	cmd.Flags().String("title", "", "内容标题")
	cmd.Flags().String("url", "", "内容 URL（用于生成 JSON-LD/llms.txt）")
	cmd.Flags().String("domain", "", "领域类型: serious|soft|knowledge")
	cmd.Flags().StringArray("engine", nil, "目标引擎: chatgpt|perplexity|gemini|claude（可多次指定）")
	cmd.Flags().StringArray("strategy", nil, "指定策略（可多次指定），不指定则自动推荐")
	cmd.Flags().String("company", "", "企业名称（用于品牌实体增强）")
	cmd.Flags().String("company-product", "", "产品名称")
	cmd.Flags().String("company-desc", "", "企业描述")
	cmd.Flags().Float64("budget-usd", 0, "LLM 月度预算上限（USD），超限后熔断后续 LLM 调用")
	llmFlags(cmd)
	cmd.Flags().String("rules", "", "规则集 JSON 路径（覆盖评分权重/策略系数）；也可用 GEO_RULES 环境变量")
	return cmd
}

func productNameFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("company-product")
	return v
}
func companyDescFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("company-desc")
	return v
}

// newScoreCmd score 子命令。
func newScoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "score",
		Short: "评估内容的 GEO 评分（0-100）",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readContent(cmd)
			if err != nil {
				return err
			}
			engine := buildEngine(cmd)
			if err := applyRulesFlag(cmd, engine); err != nil {
				return err
			}
			score, breakdowns := engine.Score(content)

			fmt.Printf("GEO 评分: %.1f/100  等级: %s\n\n", score, scoreGrade(score))
			fmt.Println("评分明细：")
			for _, b := range breakdowns {
				pct := 0.0
				if b.MaxScore > 0 {
					pct = b.Score / b.MaxScore * 100
				}
				fmt.Printf("  %-18s %5.1f / %.0f  (%.0f%%)\n", b.Category, b.Score, b.MaxScore, pct)
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "输入文件路径")
	cmd.Flags().String("content", "", "直接输入内容")
	llmFlags(cmd)
	cmd.Flags().String("rules", "", "规则集 JSON 路径（覆盖评分权重/策略系数）；也可用 GEO_RULES 环境变量")
	return cmd
}

// newAnalyzeCmd analyze 子命令。
func newAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "分析内容的 GEO 信号",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readContent(cmd)
			if err != nil {
				return err
			}
			engine := buildEngine(cmd)
			if err := applyRulesFlag(cmd, engine); err != nil {
				return err
			}
			analysis := engine.Analyze(content)

			fmt.Printf("词数: %d  常青度: %d/100\n\n", analysis.WordCount, analysis.EvergreenScore)

			fmt.Println("可引用性信号：")
			printSignals(analysis.CitabilitySignals)

			fmt.Println("\n结构信号：")
			printSignals(analysis.StructureSignals)

			if len(analysis.NegativeSignals) > 0 {
				fmt.Println("\n负向信号：")
				for _, n := range analysis.NegativeSignals {
					fmt.Printf("  ✗ %s\n", n)
				}
			} else {
				fmt.Println("\n负向信号：无")
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "输入文件路径")
	cmd.Flags().String("content", "", "直接输入内容")
	llmFlags(cmd)
	cmd.Flags().String("rules", "", "规则集 JSON 路径（覆盖评分权重/策略系数）；也可用 GEO_RULES 环境变量")
	return cmd
}

// newStrategiesCmd strategies 子命令。
func newStrategiesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "strategies",
		Short: "列出全部可用 GEO 优化策略",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("GEO 优化策略（基于 Princeton 论文 9 策略 + 工程化扩展）：")
			fmt.Printf("  %-20s %-12s %s\n", "策略", "效果系数", "说明")
			fmt.Println(strings.Repeat("-", 70))
			for _, s := range config.AllStrategies {
				eff := config.StrategyEffectiveness[s]
				fmt.Printf("  %-20s +%4.0f%%       %s\n", s, eff*100, strategyDesc(s))
			}
			return nil
		},
	}
}

// newServeCmd serve 子命令。
func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 REST API 服务",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 优先级：--port flag > GEO_PORT 环境变量 > 8080。
			// flag 默认值为空，避免默认值遮蔽环境变量回退分支。
			port, _ := cmd.Flags().GetString("port")
			if port == "" {
				port = config.Env("GEO_PORT", "8080")
			}
			engine := buildEngine(cmd)
			if err := applyRulesFlag(cmd, engine); err != nil {
				return err
			}
			srv := server.New(engine, ":"+port)
			fmt.Printf("GEO API 服务已启动: http://localhost:%s\n", port)
			fmt.Println("接口：")
			fmt.Println("  GET  /api/v1/health")
			fmt.Println("  GET  /api/v1/strategies")
			fmt.Println("  POST /api/v1/analyze")
			fmt.Println("  POST /api/v1/score")
			fmt.Println("  POST /api/v1/optimize")
			fmt.Println("可观测性：")
			fmt.Println("  GET  /metrics           Prometheus 指标")
			fmt.Println("  GET  /debug/pprof/      性能剖析")
			return srv.ListenAndServe()
		},
	}
	cmd.Flags().StringP("port", "p", "", "服务端口（默认 GEO_PORT 环境变量，缺省 8080）")
	llmFlags(cmd)
	cmd.Flags().String("rules", "", "规则集 JSON 路径（覆盖评分权重/策略系数）；也可用 GEO_RULES 环境变量")
	return cmd
}

func printSignals(signals map[string]bool) {
	for sig, ok := range signals {
		mark := "✗"
		if ok {
			mark = "✓"
		}
		fmt.Printf("  %s %s\n", mark, sig)
	}
}

func scoreGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

func strategyDesc(s models.StrategyType) string {
	descs := map[models.StrategyType]string{
		models.StrategyCiteSources:    "为论断补充可信来源引用",
		models.StrategyStatistics:     "补充统计数据与数值",
		models.StrategyAuthoritative:  "增强权威语气与背书",
		models.StrategyQuotation:      "补充权威引用语",
		models.StrategyFluency:        "提升流畅度与可读性",
		models.StrategyEasyUnderstand: "通俗易懂化改写",
		models.StrategyKeyword:        "自然融入关键词",
		models.StrategyUniqueWords:    "丰富词汇多样性",
		models.StrategyTechnicalTerms: "补充专业术语",
		models.StrategyStructure:      "标题/列表/表格结构化",
		models.StrategyFAQ:            "生成 FAQ 问答",
		models.StrategySchema:         "生成 JSON-LD 结构化数据",
		models.StrategyAnswerFirst:    "核心结论前置",
	}
	if d, ok := descs[s]; ok {
		return d
	}
	return ""
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"my-geo/internal/brand"
	"my-geo/internal/brand/crawlability"
	"my-geo/internal/brand/drift"
	"my-geo/internal/brand/externalsignals"
	"my-geo/internal/brand/history"
	"my-geo/internal/brand/localseo"
	"my-geo/internal/brand/readiness"
	"my-geo/internal/brand/topsource"
	"my-geo/internal/brand/vertical"
	"my-geo/internal/llm"
	"my-geo/internal/models"
	"my-geo/internal/optimizer/autorewriter"
)

// newReadinessCmd AI 可见度就绪审计命令（8 维 + CI 闸门）。
//
// 检查目标网站对 AI 搜索引擎的可见度就绪度，支持 --ci-gate 阈值判定，
// 未达阈值时以非零退出码中断，便于 CI/CD 流水线集成。
func newReadinessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "readiness",
		Short: "AI 可见度就绪审计（8 维 + CI 闸门）",
		Long: `AI 可见度就绪审计。

检查目标网站对 AI 搜索引擎（ChatGPT/Perplexity/Gemini/Claude）的可见度就绪度，
覆盖 8 个维度：robots.txt AI 爬虫、llms.txt、结构化数据、sitemap.xml、TTFB、
标题清晰度、FAQ 质量、实体身份。

配合 --ci-gate <阈值> 可作为 CI/CD 流水线闸门：得分低于阈值时返回非零退出码。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			url, _ := cmd.Flags().GetString("url")
			if strings.TrimSpace(url) == "" {
				return fmt.Errorf("请通过 --url 指定目标网站")
			}
			result, err := readiness.Audit(context.Background(), url)
			if err != nil {
				return err
			}
			ciGate, _ := cmd.Flags().GetFloat64("ci-gate")
			if cmd.Flags().Changed("ci-gate") {
				gate := readiness.CIGateReportWithThreshold(result, ciGate)
				printReadinessResult(result, gate)
				if !gate.Passed {
					os.Exit(1)
				}
				return nil
			}
			printReadinessResult(result, nil)
			return nil
		},
	}
	cmd.Flags().String("url", "", "目标网站 URL（必填）")
	cmd.Flags().Float64("ci-gate", 0, "CI 门禁阈值（0-100），未达阈值时以非零退出码中断")
	return cmd
}

// printReadinessResult 打印就绪审计结果与可选的 CI 闸门判定。
func printReadinessResult(r *readiness.AuditResult, gate *readiness.CIGateResult) {
	divider := strings.Repeat("═", 50)
	fmt.Println(divider)
	fmt.Printf(" AI 可见度就绪审计：%s\n", r.URL)
	fmt.Println(divider)
	fmt.Printf(" 综合得分：%.1f / 100   等级：%s\n\n", r.TotalScore, r.Grade)
	fmt.Println("【8 维检查明细】")
	for _, c := range r.Checks {
		mark := "✓"
		if c.Status == "fail" {
			mark = "✗"
		} else if c.Status == "warn" {
			mark = "!"
		}
		fmt.Printf("  %s %-22s %5.1f  [%s] %s\n", mark, c.Name, c.Score, c.Severity, c.Detail)
	}
	if gate != nil {
		fmt.Println()
		fmt.Println("【CI 闸门判定】")
		passMark := "✗ 未通过"
		if gate.Passed {
			passMark = "✓ 通过"
		}
		fmt.Printf("  %s  得分 %.1f / 阈值 %.1f\n", passMark, gate.Score, gate.Threshold)
		if len(gate.BlockingIssues) > 0 {
			fmt.Println("  阻断项：")
			for _, b := range gate.BlockingIssues {
				fmt.Printf("    • %s：%s\n", b.Name, b.Detail)
			}
		}
		if gate.Summary != "" {
			fmt.Printf("  %s\n", gate.Summary)
		}
	}
	fmt.Println(divider)
}

// newCrawlabilityCmd AI 可爬取性审计命令。
//
// 审计网站对 27 个 AI 爬虫的可见性：robots.txt 放行状态、JSON-LD schema 丰富度、
// llms.txt 存在性与深度、知识图谱（Wikidata/Wikipedia/百度百科）存在性。
// 这是 GEO 的技术基础层：屏蔽 AI 爬虫会导致被引用率降低 73%。
func newCrawlabilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crawlability",
		Short: "AI 可爬取性审计（robots.txt/llms.txt/schema/知识图谱）",
		Long: `AI 可爬取性审计。

检查网站是否允许 AI 爬虫访问、是否有结构化数据与 llms.txt、是否存在于知识图谱。
这是 GEO 的技术基础层——在评估"内容是否被引用"之前，先确认"网站是否可被爬取"。

数据显示：
  • 65% 的网站在不知情的情况下屏蔽了 AI 爬虫
  • 屏蔽 GPTBot 的网站被引用率降低 73%
  • JSON-LD schema 使 GPT-4 提取准确率从 16% 提升到 54%
  • 84.4 万站点已部署 llms.txt

评分公式（透明公开）：
  爬虫访问 40% + 结构化数据 30% + llms.txt 20% + 知识图谱 10%`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL, _ := cmd.Flags().GetString("url")
			if strings.TrimSpace(rawURL) == "" {
				return fmt.Errorf("请通过 --url 指定目标网站")
			}
			result, err := crawlability.Audit(context.Background(), rawURL)
			if err != nil {
				return err
			}
			output, _ := cmd.Flags().GetString("output")
			if output != "" {
				data, _ := json.MarshalIndent(result, "", "  ")
				return os.WriteFile(output, data, 0644)
			}
			printCrawlabilityResult(result)
			return nil
		},
	}
	cmd.Flags().String("url", "", "目标网站 URL（必填）")
	cmd.Flags().StringP("output", "o", "", "输出 JSON 报告到文件")
	return cmd
}

// printCrawlabilityResult 打印可爬取性审计结果。
func printCrawlabilityResult(r *crawlability.AuditResult) {
	divider := strings.Repeat("═", 50)
	fmt.Println(divider)
	fmt.Printf(" AI 可爬取性审计：%s\n", r.URL)
	fmt.Println(divider)
	fmt.Printf(" 综合得分：%.1f / 100   等级：%s\n\n", r.TotalScore, r.Grade)

	// 爬虫检查摘要
	allowed, blocked := 0, 0
	for _, b := range r.BotChecks {
		if b.Allowed {
			allowed++
		} else {
			blocked++
		}
	}
	fmt.Println("【AI 爬虫放行状态】")
	fmt.Printf("  允许：%d  屏蔽：%d  共计：%d\n", allowed, blocked, len(r.BotChecks))
	if blocked > 0 {
		fmt.Println("  被屏蔽的爬虫：")
		for _, b := range r.BotChecks {
			if !b.Allowed {
				fmt.Printf("    ✗ %-20s [%s] %s\n", b.Bot.UserAgent, b.Bot.Tier, b.Bot.Vendor)
			}
		}
	}
	fmt.Println()

	// Schema 检查
	fmt.Println("【JSON-LD 结构化数据】")
	if r.SchemaCheck.HasJSONLD {
		fmt.Printf("  ✓ 已部署  类型：%s  属性数：%d  丰富度：%s\n",
			strings.Join(r.SchemaCheck.SchemaTypes, "/"), r.SchemaCheck.AttrCount, r.SchemaCheck.Richness)
	} else {
		fmt.Println("  ✗ 未部署 JSON-LD（建议添加 WebSite/Organization schema）")
	}
	fmt.Println()

	// llms.txt 检查
	fmt.Println("【llms.txt】")
	if r.LlmsTxtCheck.Present {
		fmt.Printf("  ✓ 已部署  深度：%s  分区：%d  链接：%d  H1:%v  摘要:%v  full.txt:%v\n",
			r.LlmsTxtCheck.Depth, r.LlmsTxtCheck.Sections, r.LlmsTxtCheck.Links,
			r.LlmsTxtCheck.HasH1, r.LlmsTxtCheck.HasQuote, r.LlmsTxtCheck.HasFullTxt)
	} else {
		fmt.Println("  ✗ 未部署 llms.txt（建议生成面向 LLM 的站点摘要）")
	}
	fmt.Println()

	// 知识图谱检查
	fmt.Println("【知识图谱存在性】")
	fmt.Printf("  Wikidata:    %s\n", markBool(r.KGCheck.Wikidata))
	fmt.Printf("  Wikipedia EN: %s\n", markBool(r.KGCheck.WikipediaEN))
	fmt.Printf("  Wikipedia ZH: %s\n", markBool(r.KGCheck.WikipediaZH))
	fmt.Printf("  百度百科:      %s\n", markBool(r.KGCheck.BaiduBaike))
	fmt.Println()

	// 建议
	if len(r.Recommendations) > 0 {
		fmt.Println("【优化建议】")
		for _, rec := range r.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
		fmt.Println()
	}
	fmt.Println(divider)
}

func markBool(b bool) string {
	if b {
		return "✓ 已收录"
	}
	return "✗ 未收录"
}

// newTopSourceCmd Top Source 归因分析命令。
//
// 从品牌审计结果 JSON 中识别 LLM 引用的第三方权威域名，
// 指导品牌在哪些站点投入反向链接与 PR 资源。
func newTopSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topsource",
		Short: "Top Source 归因分析：识别 LLM 引用的权威域名",
		Long: `Top Source 归因分析。

输入品牌审计报告 JSON（含 results 字段），分析 LLM 在回答中引用的第三方域名，
识别品牌未曝光的权威源（missing sources）并给出可执行建议。

输入：geo brand-audit 的输出 JSON（-f 文件 / stdin）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			brandName, _ := cmd.Flags().GetString("brand-name")
			brandDomain, _ := cmd.Flags().GetString("brand-domain")
			data, err := readStdinOrFile(cmd)
			if err != nil {
				return err
			}
			var report struct {
				BrandName string `json:"brand_name"`
				Results   []struct {
					Prompt    string `json:"prompt"`
					Citations []struct {
						URL      string `json:"url"`
						Title    string `json:"title"`
						Snippet  string `json:"snippet"`
					} `json:"citations"`
				} `json:"results"`
			}
			if err := json.Unmarshal(data, &report); err != nil {
				return fmt.Errorf("解析审计报告 JSON 失败: %w", err)
			}
			if brandName == "" {
				brandName = report.BrandName
			}
			if brandName == "" {
				return fmt.Errorf("请通过 --brand-name 指定品牌名，或确保报告含 brand_name 字段")
			}
			// 转换为 brand.PromptResult（topsource.Analyze 只用 Prompt 与 Citations 字段）
			results := make([]brand.PromptResult, 0, len(report.Results))
			for _, r := range report.Results {
				pr := brand.PromptResult{Prompt: r.Prompt}
				for _, c := range r.Citations {
					pr.Citations = append(pr.Citations, models.Citation{
						URL:     c.URL,
						Title:   c.Title,
						Snippet: c.Snippet,
					})
				}
				results = append(results, pr)
			}
			ar := topsource.Analyze(brandName, results, brandDomain)
			b, _ := json.MarshalIndent(ar, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "审计报告 JSON 文件路径")
	cmd.Flags().String("brand-name", "", "品牌名（默认取报告 brand_name）")
	cmd.Flags().String("brand-domain", "", "品牌官网域名（可选，用于判定品牌曝光）")
	return cmd
}

// readStdinOrFile 从 -f 文件或 stdin 读取内容。
func readStdinOrFile(cmd *cobra.Command) ([]byte, error) {
	file, _ := cmd.Flags().GetString("file")
	if file != "" {
		return os.ReadFile(file)
	}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return readAll(os.Stdin)
	}
	return nil, fmt.Errorf("请通过 -f <文件> 或管道提供输入")
}

// newVerticalCmd 行业类型自动识别命令。
//
// 基于品牌画像识别 5 类业务形态，输出差异化评分权重与建议。
func newVerticalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vertical",
		Short: "行业类型自动识别与差异化建议",
		Long: `行业类型自动识别。

基于品牌画像 JSON（industry/category/domain/products/company 等字段）自动识别
5 类业务形态：SaaS / 本地服务 / 电商 / 媒体出版 / 代理咨询，
并输出该行业的差异化 BVS 评分权重与运营建议。

输入：品牌画像 JSON（-f 文件 / --profile / stdin）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readProfileBytes(cmd)
			if err != nil {
				return err
			}
			var profile map[string]interface{}
			if err := json.Unmarshal(data, &profile); err != nil {
				return fmt.Errorf("解析品牌画像 JSON 失败: %w", err)
			}
			v := vertical.Detect(profile)
			cfg := vertical.GetConfig(v)
			score, _ := cmd.Flags().GetFloat64("score")
			recs := vertical.RecommendationsFor(v, score)

			fmt.Printf("行业类型：%s（%s）\n", v, cfg.Label)
			fmt.Printf("说明：%s\n\n", cfg.Description)
			fmt.Println("【差异化 BVS 评分权重】")
			for dim, w := range cfg.ScoreWeights {
				fmt.Printf("  %-16s %.0f%%\n", dim, w*100)
			}
			if len(recs) > 0 {
				fmt.Println()
				fmt.Printf("【运营建议】（当前 BVS=%.1f）\n", score)
				for _, r := range recs {
					fmt.Printf("  [%s][%s] %s\n", r.Priority, r.Category, r.Title)
					fmt.Printf("      %s\n", r.Detail)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "品牌画像 JSON 文件路径")
	cmd.Flags().String("profile", "", "品牌画像 JSON 字符串")
	cmd.Flags().Float64("score", 0, "当前 BVS 评分（用于生成差异化建议，默认 0=低分建议）")
	return cmd
}

// readProfileBytes 读取品牌画像原始字节（-f / --profile / stdin）。
func readProfileBytes(cmd *cobra.Command) ([]byte, error) {
	file, _ := cmd.Flags().GetString("file")
	profileStr, _ := cmd.Flags().GetString("profile")
	if profileStr != "" {
		return []byte(profileStr), nil
	}
	if file != "" {
		return os.ReadFile(file)
	}
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return readAll(os.Stdin)
	}
	return nil, fmt.Errorf("请通过 -f <文件>、--profile <JSON> 或管道提供品牌画像")
}

// newLocalSEOCmd 本地 SEO / GMB 审计命令。
//
// 检查 NAP 一致性、Google Business Profile 完整度、本地引用收录情况。
func newLocalSEOCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "localseo",
		Short: "本地 SEO / GMB 审计：NAP 一致性 + 商家资料 + 本地引用",
		Long: `本地 SEO / GMB 审计。

检查品牌在本地搜索生态中的可见度与一致性，覆盖 3 个维度：
  - NAP（名称-地址-电话）跨目录一致性
  - Google Business Profile 资料完整度
  - 国内外主流商家目录的收录情况

输入 NAP JSON（-f 文件 / --nap / stdin），示例：
{"brand_name": "老王餐馆", "nap": {"name": "老王餐馆", "address": "北京市朝阳区xx路1号", "phone": "010-12345678"}}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readProfileBytes(cmd)
			if err != nil {
				return err
			}
			var body struct {
				BrandName string          `json:"brand_name"`
				NAP       localseo.NAPInfo `json:"nap"`
			}
			if err := json.Unmarshal(data, &body); err != nil {
				return fmt.Errorf("解析 NAP JSON 失败: %w", err)
			}
			if strings.TrimSpace(body.BrandName) == "" {
				return fmt.Errorf("brand_name 不能为空")
			}
			if strings.TrimSpace(body.NAP.Name) == "" {
				body.NAP.Name = body.BrandName
			}
			report, err := localseo.Audit(context.Background(), body.BrandName, body.NAP)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "NAP JSON 文件路径")
	cmd.Flags().String("nap", "", "NAP JSON 字符串")
	return cmd
}

// newExternalSignalsCmd 外部信号采集命令（按量付费第三方数据源）。
//
// 调用 DataForSEO（付费）或 Common Crawl（免费）采集关键词、反链、SERP 信号。
func newExternalSignalsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "externalsignals",
		Short: "外部信号采集（DataForSEO 付费 / Common Crawl 免费）",
		Long: `外部信号采集。

按量付费集成第三方 SEO 数据源，采集关键词搜索量/难度、反链与 SERP 特性。
未配置 DataForSEO API Key 时自动回退到 Common Crawl 免费接口或模拟数据。

环境变量：
  GEO_DFS_APIKEY / GEO_DFS_EMAIL  DataForSEO 凭据（可选，付费按量计费）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, _ := cmd.Flags().GetString("domain")
			if strings.TrimSpace(domain) == "" {
				return fmt.Errorf("请通过 --domain 指定目标域名")
			}
			var keywords []string
			if k, _ := cmd.Flags().GetStringArray("keywords"); len(k) > 0 {
				keywords = k
			}
			client := externalsignals.NewFromEnv()
			if !client.Available() {
				fmt.Fprintln(os.Stderr, "[提示] 未配置 DataForSEO 凭据，将回退到 Common Crawl / 模拟数据。")
			}
			report, err := client.FullReport(context.Background(), domain, keywords)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(report, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().String("domain", "", "目标域名（必填）")
	cmd.Flags().StringArray("keywords", nil, "关键词列表（可多次指定）")
	return cmd
}

// newAutoRewriteCmd AutoGEO 规则提取与改写命令。
//
// 自动发现 GEO 优化规则并据此改写文档，改写后执行 GEU 校验防止内容质量降级。
func newAutoRewriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autorewrite",
		Short: "AutoGEO 规则提取与改写（含 GEU 校验）",
		Long: `AutoGEO 规则提取与改写。

基于 Princeton GEO 论文 9 条默认规则（含 PWC 提升值）改写内容，使其更易被
AI 搜索引擎引用。配置 GEO_LLM_KEY 时使用 LLM 改写，否则走规则化降级路径。
改写后自动执行 GEU（生成式引擎效用）校验，确保 Precision/Recall/Clarity/Insight 不降级。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readContent(cmd)
			if err != nil {
				return err
			}
			query, _ := cmd.Flags().GetString("query")
			engine, _ := cmd.Flags().GetString("engine")
			preserveFacts, _ := cmd.Flags().GetBool("preserve-facts")
			listRules, _ := cmd.Flags().GetBool("list-rules")

			mgr := newLLMManagerFromEnvCLI(cmd)
			rw := autorewriter.New(mgr)

			if listRules {
				rules := autorewriter.DefaultRules()
				fmt.Printf("AutoGEO 默认规则（%d 条，含 Princeton PWC 提升值）：\n", len(rules))
				for _, r := range rules {
					boost := fmt.Sprintf("+%.1f%%", r.PWCBoost)
					if r.PWCBoost < 0 {
						boost = fmt.Sprintf("%.1f%%", r.PWCBoost)
					}
					fmt.Printf("  %-22s [%s] 优先级 %.2f  PWC %s\n", r.ID, r.Category, r.Priority, boost)
					fmt.Printf("      %s\n", r.Description)
				}
				return nil
			}

			req := &autorewriter.RewriteRequest{
				Content:       content,
				Query:         query,
				Engine:        engine,
				PreserveFacts: preserveFacts,
			}
			result, err := rw.Rewrite(context.Background(), req)
			if err != nil {
				return err
			}
			out, _ := cmd.Flags().GetString("output")
			if out != "" {
				if err := os.WriteFile(out, []byte(result.RewrittenContent), 0644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "改写内容已写入 %s\n", out)
			} else {
				fmt.Println("【改写后内容】")
				fmt.Println(result.RewrittenContent)
			}
			fmt.Fprintf(os.Stderr, "\n预估 PWC 提升：%.1f%%\n", result.EstimatedPWCBoost)
			fmt.Fprintf(os.Stderr, "GEU 校验：%s (Precision=%.2f Recall=%.2f Clarity=%.2f Insight=%.2f)\n",
				passLabel(result.GEUCheck.Passed), result.GEUCheck.Precision, result.GEUCheck.Recall,
				result.GEUCheck.Clarity, result.GEUCheck.Insight)
			if len(result.GEUCheck.Warnings) > 0 {
				fmt.Fprintln(os.Stderr, "GEU 警告：")
				for _, w := range result.GEUCheck.Warnings {
					fmt.Fprintf(os.Stderr, "  • %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "输入文件路径")
	cmd.Flags().String("content", "", "直接输入内容")
	cmd.Flags().StringP("output", "o", "", "输出文件路径（仅写改写后内容）")
	cmd.Flags().String("query", "", "用户查询（用于上下文相关改写）")
	cmd.Flags().String("engine", "", "目标引擎（chatgpt/perplexity/gemini/claude 等）")
	cmd.Flags().Bool("preserve-facts", false, "严格保持事实准确性（GEU 校验更严格）")
	cmd.Flags().Bool("list-rules", false, "仅列出默认规则集与 PWC 提升值，不改写")
	llmFlags(cmd)
	return cmd
}

// newLLMManagerFromEnvCLI 从 flags/环境变量构建 LLM 管理器，适配 autorewriter.LLMClient。
func newLLMManagerFromEnvCLI(cmd *cobra.Command) *llmCLIAdapter {
	key, _ := cmd.Flags().GetString("llm-key")
	base, _ := cmd.Flags().GetString("llm-base")
	model, _ := cmd.Flags().GetString("llm-model")
	if key == "" {
		return &llmCLIAdapter{mgr: llm.NewManager(llm.NewStub())}
	}
	opts := []llm.OpenAIOption{}
	if base != "" {
		opts = append(opts, llm.WithBaseURL(base))
	}
	if model != "" {
		opts = append(opts, llm.WithModel(model))
	}
	return &llmCLIAdapter{mgr: llm.NewManager(llm.NewOpenAI(key, opts...))}
}

// llmCLIAdapter 将 llm.Manager 适配为 autorewriter.LLMClient。
type llmCLIAdapter struct {
	mgr *llm.Manager
}

func (a *llmCLIAdapter) Available() bool {
	return a.mgr != nil && a.mgr.HasAvailable()
}

func (a *llmCLIAdapter) Complete(ctx context.Context, prompt string) (string, error) {
	if a.mgr == nil {
		return "", fmt.Errorf("LLM 管理器未初始化")
	}
	return a.mgr.Rewrite(ctx, prompt, "")
}

func passLabel(passed bool) string {
	if passed {
		return "通过"
	}
	return "未通过"
}

// newDriftCmd diff/drift 回归检测命令。
//
// 对比两次品牌审计结果，检测各维度漂移与回归，可作为 CI/CD 回归闸门。
func newDriftCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "diff/drift 回归检测：对比两次审计结果",
		Long: `diff/drift 回归检测。

对比两次品牌审计结果，检测各维度漂移与回归，识别 BVS 评分、提及率、引用率、
声量份额、引用位置、情感等维度的显著变化，用于：
  - CI/CD 回归闸门：内容/SEO 改动后检测可见度是否退步
  - 运营趋势监控：对比历史审计快照，定位退步维度

两种输入方式：
  1. 历史库模式（推荐）：--brand <品牌名>，从审计历史库取最近两条对比
  2. 文件模式：--prev <prev.json> --cur <cur.json>，对比两份 history.Record JSON

配合 --ci-gate <阈值> 可作为 CI 闸门：存在 critical 回归或 BVS 下降超阈值时
返回非零退出码。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			brandName, _ := cmd.Flags().GetString("brand")
			prevFile, _ := cmd.Flags().GetString("prev")
			curFile, _ := cmd.Flags().GetString("cur")

			var report *drift.Report
			if brandName != "" {
				db, err := history.Open(os.Getenv("GEO_HISTORY_DB_PATH"))
				if err != nil {
					return fmt.Errorf("打开历史库失败: %w", err)
				}
				defer db.Close()
				report, err = drift.CompareLatest(context.Background(), db, brandName)
				if err != nil {
					return err
				}
				if report == nil {
					return fmt.Errorf("品牌 %s 历史记录不足两条，无法对比", brandName)
				}
			} else if prevFile != "" && curFile != "" {
				prev, err := readRecordFile(prevFile)
				if err != nil {
					return err
				}
				cur, err := readRecordFile(curFile)
				if err != nil {
					return err
				}
				report = drift.Compare(*prev, *cur)
			} else {
				return fmt.Errorf("请通过 --brand <品牌名> 或 --prev/--cur <文件> 指定对比来源")
			}

			output, _ := cmd.Flags().GetString("output")
			if output != "" {
				data, _ := json.MarshalIndent(report, "", "  ")
				return os.WriteFile(output, data, 0644)
			}

			ciGate, _ := cmd.Flags().GetFloat64("ci-gate")
			if cmd.Flags().Changed("ci-gate") {
				printDriftReport(report, &ciGate)
				if !report.CIGate(ciGate) {
					os.Exit(1)
				}
				return nil
			}
			printDriftReport(report, nil)
			return nil
		},
	}
	cmd.Flags().String("brand", "", "品牌名（历史库模式，从审计历史库取最近两条对比）")
	cmd.Flags().String("prev", "", "上一次审计记录 JSON 文件（文件模式）")
	cmd.Flags().String("cur", "", "当前审计记录 JSON 文件（文件模式）")
	cmd.Flags().StringP("output", "o", "", "输出 JSON 报告到文件")
	cmd.Flags().Float64("ci-gate", 0, "CI 门禁阈值（0-100），存在 critical 回归或 BVS 下降超阈值时非零退出")
	return cmd
}

// readRecordFile 读取 history.Record JSON 文件。
func readRecordFile(path string) (*history.Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件 %s 失败: %w", path, err)
	}
	var r history.Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("解析 history.Record JSON 失败: %w", err)
	}
	return &r, nil
}

// printDriftReport 打印漂移对比报告与可选的 CI 闸门判定。
func printDriftReport(r *drift.Report, gateThreshold *float64) {
	divider := strings.Repeat("═", 50)
	fmt.Println(divider)
	fmt.Printf(" diff/drift 回归检测：%s\n", r.BrandName)
	fmt.Println(divider)
	prevT := time.Unix(r.PreviousAt, 0).Format("2006-01-02 15:04")
	curT := time.Unix(r.CurrentAt, 0).Format("2006-01-02 15:04")
	fmt.Printf(" 对比区间：%s → %s\n", prevT, curT)
	verdictLabel := map[string]string{
		"improved":  "改善 ↑",
		"regressed": "回归 ↓",
		"stable":    "持平 →",
	}[r.Verdict]
	fmt.Printf(" 整体结论：%s   BVS 变化：%+.1f\n\n", verdictLabel, r.ScoreDelta)

	fmt.Println("【维度漂移明细】")
	for _, d := range r.Dimensions {
		mark := "·"
		switch d.Severity {
		case "critical":
			mark = "✗"
		case "warning":
			mark = "!"
		case "none":
			if d.Direction == "up" || d.Direction == "down" {
				mark = "•"
			}
		}
		arrow := "→"
		if d.Direction == "up" {
			arrow = "↑"
		} else if d.Direction == "down" {
			arrow = "↓"
		}
		sev := ""
		if d.Severity == "critical" || d.Severity == "warning" {
			sev = " [" + d.Severity + "]"
		}
		fmt.Printf("  %s %-14s %6.1f %s %6.1f  (%+.1f)%s\n", mark, d.Label, d.Previous, arrow, d.Current, d.Delta, sev)
	}

	if len(r.Engines) > 0 {
		fmt.Println()
		fmt.Println("【引擎级漂移】")
		for _, ed := range r.Engines {
			fmt.Printf("  ▸ %s\n", ed.Engine)
			for _, d := range ed.Dims {
				fmt.Printf("      %-12s %6.1f → %6.1f  (%+.1f)\n", d.Label, d.Previous, d.Current, d.Delta)
			}
		}
	}

	if len(r.Regressions) > 0 {
		fmt.Println()
		fmt.Println("【回归项】")
		for _, reg := range r.Regressions {
			fmt.Printf("  ✗ [%s] %s：%.1f → %.1f (%+.1f)\n", reg.Severity, reg.Label, reg.Previous, reg.Current, reg.Delta)
		}
	}
	if len(r.Improvements) > 0 {
		fmt.Println()
		fmt.Println("【改善项】")
		for _, imp := range r.Improvements {
			fmt.Printf("  ✓ %s：%.1f → %.1f (%+.1f)\n", imp.Label, imp.Previous, imp.Current, imp.Delta)
		}
	}

	if gateThreshold != nil {
		fmt.Println()
		fmt.Println("【CI 闸门判定】")
		passMark := "✗ 未通过"
		if r.CIGate(*gateThreshold) {
			passMark = "✓ 通过"
		}
		fmt.Printf("  %s  BVS 阈值 %.1f  实际变化 %+.1f\n", passMark, *gateThreshold, r.ScoreDelta)
	}
	fmt.Println(divider)
}

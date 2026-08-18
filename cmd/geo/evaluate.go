package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"my-geo/internal/config"
	"my-geo/internal/eval"
)

// newEvaluateCmd GEO 评测命令：加载评测集，跑改前/改后引用率与评分对比，产出可复现报告。
func newEvaluateCmd() *cobra.Command {
	var datasetPath, format, output string
	var live bool
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "运行 GEO 评测集，产出改前/改后引用率与评分对比报告",
		Long: `运行中文 GEO 评测集（战略级改进方向 #1，对标 AutoGEO GEO-Bench）。

示例：
  geo evaluate --dataset config/benchmarks/zh-geo-sample.json
  geo evaluate --dataset x.json --format json --output report.json
  geo evaluate --dataset x.json --rules config/rules/zh-ecommerce.json   # 用规则集跑评测
  geo evaluate --dataset x.json --live --llm-key sk-xxx   # 接入真实引擎实测引用（替换离线代理）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if datasetPath == "" {
				return fmt.Errorf("请通过 --dataset 指定评测集 JSON 路径")
			}
			b, err := os.ReadFile(datasetPath)
			if err != nil {
				return fmt.Errorf("读取评测集失败: %w", err)
			}
			var bench eval.Benchmark
			if err := json.Unmarshal(b, &bench); err != nil {
				return fmt.Errorf("解析评测集失败: %w", err)
			}
			engine := buildEngine(cmd)
			if err := applyRulesFlag(cmd, engine); err != nil {
				return err
			}
			var opts []eval.EvalOption
			if live {
				key, _ := cmd.Flags().GetString("llm-key")
				base, _ := cmd.Flags().GetString("llm-base")
				model, _ := cmd.Flags().GetString("llm-model")
				if key == "" {
					key = config.Env("GEO_LLM_KEY", "")
				}
				if base == "" {
					base = config.Env("GEO_LLM_BASE", "https://api.openai.com/v1")
				}
				if model == "" {
					model = config.Env("GEO_LLM_MODEL", "gpt-4o-mini")
				}
				if key == "" {
					return fmt.Errorf("--live 需要 LLM API Key（--llm-key 或 GEO_LLM_KEY）")
				}
				opts = append(opts, eval.WithLiveChecker(eval.NewHTTPLiveChecker(base, model, key)))
			}
			report, err := eval.Evaluate(context.Background(), engine, &bench, opts...)
			if err != nil {
				return err
			}
			var rendered string
			if format == "json" {
				out, err := eval.RenderJSON(report)
				if err != nil {
					return err
				}
				rendered = string(out)
			} else {
				rendered = eval.RenderMarkdown(report)
			}
			if output != "" {
				if err := os.WriteFile(output, []byte(rendered), 0644); err != nil {
					return fmt.Errorf("写入报告失败: %w", err)
				}
				fmt.Fprintf(os.Stderr, "评测报告已写入 %s\n", output)
			} else {
				fmt.Println(rendered)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&datasetPath, "dataset", "", "评测集 JSON 路径（必填）")
	cmd.Flags().StringVar(&format, "format", "md", "输出格式：md | json")
	cmd.Flags().StringVar(&output, "output", "", "输出文件路径（默认打印到 stdout）")
	llmFlags(cmd)
	cmd.Flags().String("rules", "", "规则集 JSON 路径（覆盖评分权重/策略系数）；也可用 GEO_RULES 环境变量")
	cmd.Flags().BoolVar(&live, "live", false, "接入真实生成式引擎实测引用（需 --llm-key），覆盖 Actual 指标；否则用离线代理")
	return cmd
}

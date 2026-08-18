package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"my-geo/internal/llm"
)

// newCostCmd 成本仪表盘命令组。
func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "LLM 成本仪表盘（token 与美元成本聚合）",
		Long: `按模型聚合 LLM token 消耗与美元成本，支持月度预算熔断（战略级改进方向 #3）。

示例：
  geo cost report                        # 打印内置模型单价表（参考）
  geo cost report --server http://localhost:8080   # 拉取运行中服务的实时成本
  geo optimize -f a.md --budget-usd 5    # 单次优化设置月度预算上限（USD）`,
	}
	cmd.AddCommand(newCostReportCmd())
	return cmd
}

// newCostReportCmd 打印单价表与（可选）实时成本。
func newCostReportCmd() *cobra.Command {
	var serverURL, adminKey string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "打印 LLM 成本报告",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1) 始终打印内置单价表（参考）
			printPricingTable()

			// 2) 若指定 --server，拉取运行中服务的实时成本
			if serverURL == "" {
				fmt.Println("\n提示：未指定 --server，仅显示单价表。运行 geo serve 后可用 --server 查看实时成本。")
				return nil
			}
			if adminKey == "" {
				adminKey = os.Getenv("GEO_ADMIN_KEY")
			}
			report, err := fetchCostReport(serverURL, adminKey)
			if err != nil {
				return fmt.Errorf("拉取实时成本失败: %w", err)
			}
			printCostReport(report)
			return nil
		},
	}
	cmd.Flags().StringVar(&serverURL, "server", "", "运行中的 GEO 服务地址（如 http://localhost:8080）")
	cmd.Flags().StringVar(&adminKey, "admin-key", "", "管理员密钥（X-Admin-Key）；缺省读 GEO_ADMIN_KEY")
	return cmd
}

// printPricingTable 打印内置模型单价表（USD / 1K tokens）。
func printPricingTable() {
	pricing := llm.ModelPricingTable()
	models := make([]string, 0, len(pricing))
	for m := range pricing {
		models = append(models, m)
	}
	sort.Strings(models)
	fmt.Printf("\n%-20s %14s %16s\n", "MODEL", "PROMPT/1K$", "COMPLETION/1K$")
	fmt.Println(strings.Repeat("-", 56))
	for _, m := range models {
		p := pricing[m]
		fmt.Printf("%-20s %14.5f %16.5f\n", m, p.PromptPer1K, p.CompletionPer1K)
	}
}

// printCostReport 打印实时成本报告。
func printCostReport(r llm.CostReport) {
	fmt.Printf("\n=== 实时 LLM 成本（运行中服务）===\n")
	if len(r.Rows) == 0 {
		fmt.Println("（暂无 LLM 调用记录）")
	}
	fmt.Printf("%-20s %8s %12s %12s %14s\n", "MODEL", "CALLS", "PROMPT_TOK", "COMP_TOK", "COST_USD")
	fmt.Println(strings.Repeat("-", 70))
	for _, row := range r.Rows {
		fmt.Printf("%-20s %8d %12d %12d %14.4f\n", row.Model, row.Calls, row.PromptTokens, row.CompletionTokens, row.CostUSD)
	}
	fmt.Printf("\n总计: $%.4f", r.TotalUSD)
	if r.BudgetUSD > 0 {
		fmt.Printf("  | 预算: $%.2f", r.BudgetUSD)
		if r.Breached {
			fmt.Print("  | ⚠ 已熔断")
		}
	}
	fmt.Println()
}

// fetchCostReport 从运行中的服务拉取成本报告。
func fetchCostReport(serverURL, adminKey string) (llm.CostReport, error) {
	var empty llm.CostReport
	url := serverURL
	if len(url) < 4 || url[:4] != "http" {
		url = "http://" + url
	}
	url = url + "/api/v1/admin/cost"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return empty, err
	}
	if adminKey != "" {
		req.Header.Set("X-Admin-Key", adminKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, err
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("服务返回 %d: %s", resp.StatusCode, string(body))
	}
	var report llm.CostReport
	if err := json.Unmarshal(body, &report); err != nil {
		return empty, err
	}
	return report, nil
}

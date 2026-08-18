package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"my-geo/internal/config"
)

// newRulesCmd 规则集命令组：查看/校验外部化评分规则集。
func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "规则集管理（评分权重 + 策略效果系数外部化）",
		Long: `规则集将 GEO 评分权重与策略触发条件从硬编码改为可加载的 JSON 资产，
支持按行业/引擎偏好组合、版本化。对应战略级改进方向 #2。

示例：
  geo rules show                        # 打印当前内置默认规则集
  geo rules validate config/rules/x.json   # 校验规则集文件
  geo rules list                        # 列出可用规则集
  geo optimize -f a.md --rules config/rules/zh-ecom.json  # 优化时套用规则集`,
	}
	cmd.AddCommand(newRulesShowCmd(), newRulesValidateCmd(), newRulesListCmd())
	return cmd
}

// newRulesShowCmd 打印默认（有效）规则集，或指定 --rules 打印某规则集文件。
func newRulesShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "打印当前默认规则集",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("rules")
			var rs *config.RuleSet
			var err error
			if path != "" {
				rs, err = config.LoadRuleSet(path)
			} else {
				rs = config.DefaultRuleSet()
			}
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(rs, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().String("rules", "", "规则集 JSON 路径；省略则打印内置默认规则集")
	return cmd
}

// newRulesValidateCmd 校验规则集文件。
func newRulesValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <path>",
		Short: "校验规则集 JSON 文件",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rs, err := config.LoadRuleSet(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("✓ 规则集校验通过: %s v%s\n", rs.Name, rs.Version)
			fmt.Printf("  权重条目: %d，策略系数条目: %d，触发条件: %d\n",
				len(rs.Weights), len(rs.StrategyEffectiveness), len(rs.StrategyTriggers))
			if rs.Engine != "" || rs.Domain != "" {
				fmt.Printf("  适用: engine=%s domain=%s\n", rs.Engine, rs.Domain)
			}
			return nil
		},
	}
	return cmd
}

// newRulesListCmd 列出可用规则集（内置 + config/rules 下文件）。
func newRulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出可用规则集",
		RunE: func(_ *cobra.Command, args []string) error {
			fmt.Printf("%-20s %-14s %s\n", "NAME", "VERSION", "SOURCE")
			fmt.Println("------------------------------------------------------------")
			fmt.Printf("%-20s %-14s %s\n", "default", "builtin-1.0.0", "内置（代码基线）")
			// 尽力列举 config/rules 目录下可加载的规则集（目录不存在则跳过）。
			dir := "config/rules"
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil // 目录可选，忽略
			}
			var names []string
			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
					continue
				}
				names = append(names, e.Name())
			}
			sort.Strings(names)
			for _, n := range names {
				rs, err := config.LoadRuleSet(filepath.Join(dir, n))
				if err != nil {
					fmt.Printf("%-20s %-14s %s (校验失败: %v)\n", n, "-", "config/rules/"+n, err)
					continue
				}
				fmt.Printf("%-20s %-14s %s\n", rs.Name, rs.Version, "config/rules/"+n)
			}
			return nil
		},
	}
}

// applyRulesFlag 若指定 --rules <path> 或 GEO_RULES 环境变量，加载并应用到引擎。
func applyRulesFlag(cmd *cobra.Command, engine interface {
	ApplyRuleSet(*config.RuleSet)
}) error {
	path, _ := cmd.Flags().GetString("rules")
	if path == "" {
		path = config.Env("GEO_RULES", "")
	}
	if path == "" {
		return nil
	}
	rs, err := config.LoadRuleSet(path)
	if err != nil {
		return err
	}
	engine.ApplyRuleSet(rs)
	fmt.Fprintf(os.Stderr, "已应用规则集: %s v%s\n", rs.Name, rs.Version)
	return nil
}

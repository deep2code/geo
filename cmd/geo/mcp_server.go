package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"my-geo/internal/brand/mcpserver"
	"my-geo/internal/server"
)

// newMCPServerCmd 创建 MCP Server 子命令。
//
// 将 GEO 核心能力暴露为 MCP Server，让 Claude / Cursor / TraeCode
// 等 MCP 客户端可以直接调用品牌审计、内容优化、工商搜索等工具。
//
// 端点：POST /mcp （JSON-RPC 2.0 over HTTP）
// 与 serve 命令的 REST API 独立运行在不同端口。
func newMCPServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "启动 MCP Server，暴露 GEO 能力给 MCP 客户端",
		Long: `MCP Server 模式：将 GEO 核心能力暴露为 MCP（Model Context Protocol）Server。

让 Claude / Cursor / TraeCode 等 MCP 客户端可以直接调用以下工具：
  geo_brand_audit       品牌可见度审计（BVS 评分 + 运营报告）
  geo_optimize_content  内容 GEO 优化
  geo_search_companies  离线工商库搜索
  geo_chinacheck        实时工商核验（GSXT/SAMR）
  geo_readiness_audit   AI 可见度就绪审计

端点：POST /mcp （JSON-RPC 2.0 over HTTP, Streamable HTTP transport）
协议版本：2025-06-18

环境变量（与 serve 命令共用）：
  各引擎 API Key：GEO_GLM_KEY / GEO_DEEPSEEK_KEY / GEO_OPENAI_KEY 等
  LLM 配置：GEO_LLM_KEY / GEO_LLM_BASE / GEO_LLM_MODEL
  工商核验：GEO_CHINACHECK_ENABLED（默认 true）
  离线工商库：GEO_OFFLINE_DB_ENABLED（默认 true）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Flags().GetString("port")
			if port == "" {
				port = "9090"
			}

			// 构建 GEO 内容优化引擎
			geoEngine := buildEngine(cmd)

			// 构建品牌可见度引擎（复用 server 包的环境变量逻辑）
			brandEngine := server.BuildBrandEngineFromEnv()
			if brandEngine == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "[警告] 品牌引擎未初始化（geo_brand_audit / geo_search_companies / geo_chinacheck 工具将不可用）")
				fmt.Fprintln(cmd.ErrOrStderr(), "       请配置引擎 API Key 环境变量（如 GEO_GLM_KEY / GEO_OPENAI_KEY 等）")
			}

			// 启动 MCP Server
			srv := mcpserver.New(brandEngine, geoEngine, ":"+port)
			fmt.Printf("MCP Server listening on http://localhost:%s/mcp\n", port)
			fmt.Println("工具列表：")
			fmt.Println("  geo_brand_audit       品牌可见度审计")
			fmt.Println("  geo_optimize_content  内容 GEO 优化")
			fmt.Println("  geo_search_companies  离线工商库搜索")
			fmt.Println("  geo_chinacheck        实时工商核验")
			fmt.Println("  geo_readiness_audit   AI 可见度就绪审计")
			return srv.Start()
		},
	}
	cmd.Flags().StringP("port", "p", "9090", "MCP Server 端口")
	llmFlags(cmd)
	return cmd
}

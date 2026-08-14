package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"my-geo/internal/brand/chinacheck"
)

// newBrandCacheCmd 创建 brand-cache 子命令：China-Check MCP 本地缓存管理。
//
// 子命令：
//   stats                查看缓存统计（条目数、文件大小、TTL 等）
//   clear                清空缓存
//   compact              压缩/去重缓存文件（剔除过期条目）
//   import -f list.txt   按品牌/公司名清单批量预热缓存（每行一个查询词，支持 # 注释）
//   import --queries "腾讯,阿里,字节"  直接传逗号分隔的查询词
func newBrandCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "brand-cache",
		Short: "China-Check MCP 工商核验本地缓存管理（预热/统计/清空）",
		Long: `管理 China-Check MCP 的 MySQL 持久化缓存（默认使用 GEO_CHINACHECK_MYSQL_DSN 环境变量连接）。

常用场景：
  1. 第一次部署，批量预热自己关心的 Top 1000 品牌：
     geo brand-cache import -f my_brands.txt
  2. 查看缓存占用：
     geo brand-cache stats
  3. 数据大版本更新（如年度）后清缓存：
     geo brand-cache clear

环境变量（与 server 共用同一套）：
  GEO_CHINACHECK_CACHE_PATH=/path/to/file.jsonl  自定义缓存文件
  GEO_CHINACHECK_CACHE_MAX_ITEMS=50000           最大条目（默认 10000）
  GEO_CHINACHECK_CACHE_TTL_HOURS=720             条目 TTL 小时（默认 720=30 天）`,
	}
	cmd.AddCommand(
		newBrandCacheStatsCmd(),
		newBrandCacheClearCmd(),
		newBrandCacheCompactCmd(),
		newBrandCacheImportCmd(),
	)
	return cmd
}

func newBrandCacheStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "查看缓存统计",
		RunE: func(cmd *cobra.Command, args []string) error {
			ca, err := openCacheFromFlags(cmd)
			if err != nil {
				return err
			}
			st := ca.Stats()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println(" China-Check MCP 本地缓存统计")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf(" 文件路径:   %s\n", st.File)
			fmt.Printf(" 条目数量:   %d / %d (%.0f%%)\n", st.Count, st.MaxItems, float64(st.Count)/float64(maxInt(1, st.MaxItems))*100)
			fmt.Printf(" 单条 TTL:   %d 小时 (%.0f 天)\n", st.TTLSeconds/3600, float64(st.TTLSeconds)/86400)
			fmt.Printf(" 文件大小:   %s\n", humanBytes(st.FileSizeByte))
			return nil
		},
	}
}

func newBrandCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "清空缓存（删除缓存文件）",
		RunE: func(cmd *cobra.Command, args []string) error {
			ca, err := openCacheFromFlags(cmd)
			if err != nil {
				return err
			}
			before := ca.Stats()
			if err := ca.Clear(); err != nil {
				return fmt.Errorf("清空失败: %w", err)
			}
			fmt.Printf("✓ 缓存已清空（原 %d 条，原 %s）\n", before.Count, humanBytes(before.FileSizeByte))
			return nil
		},
	}
}

func newBrandCacheCompactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "压缩/去重缓存文件（剔除过期条目，重写为最小文件）",
		RunE: func(cmd *cobra.Command, args []string) error {
			ca, err := openCacheFromFlags(cmd)
			if err != nil {
				return err
			}
			before := ca.Stats()
			start := time.Now()
			if err := ca.Compact(); err != nil {
				return fmt.Errorf("压缩失败: %w", err)
			}
			after := ca.Stats()
			saved := int64(0)
			if before.FileSizeByte > after.FileSizeByte {
				saved = before.FileSizeByte - after.FileSizeByte
			}
			fmt.Printf("✓ 压缩完成（耗时 %v）：%d → %d 条，文件 %s → %s（节省 %s）\n",
				time.Since(start).Round(time.Millisecond),
				before.Count, after.Count,
				humanBytes(before.FileSizeByte), humanBytes(after.FileSizeByte),
				humanBytes(saved),
			)
			return nil
		},
	}
}

func newBrandCacheImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "批量预热缓存（按品牌/公司名清单，逐条 Search + Top1 Snapshot 写入缓存）",
		Long: `从文件或 --queries 参数读取品牌/公司名列表，依次调用 Search+Snapshot 预热缓存。

支持的输入格式：
  -f list.txt  每行一个查询词，# 开头为注释，空行忽略
  --queries "腾讯,阿里巴巴,字节跳动"  逗号分隔直接传

默认对每条查询只对 Top1 命中拉 snapshot（性价比最高），
可用 --limit 调整搜索结果数（缓存的是搜索全量结果，不限于 Top1）。

示例：
  geo brand-cache import -f top_brands.txt --timeout 60s
  geo brand-cache import --queries "海尔,茅台,海底捞"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			queries, err := collectImportQueries(cmd)
			if err != nil {
				return err
			}
			if len(queries) == 0 {
				return fmt.Errorf("没有有效查询词，请通过 -f <文件> 或 --queries \"a,b,c\" 提供")
			}
			limit, _ := cmd.Flags().GetInt("limit")
			timeoutStr, _ := cmd.Flags().GetString("timeout")
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("解析 --timeout 失败: %w", err)
			}
			dry, _ := cmd.Flags().GetBool("dry")

			// 打开缓存 + 构建 client（缓存注入，dry 模式下也同样有缓存路径打印）
			ca, err := openCacheFromFlags(cmd)
			if err != nil {
				return err
			}
			ccOpts := []chinacheck.Option{chinacheck.WithCache(ca)}
			if u, _ := cmd.Flags().GetString("url"); u != "" {
				ccOpts = append(ccOpts, chinacheck.WithURL(u))
			}
			if l, _ := cmd.Flags().GetString("lang"); l != "" {
				ccOpts = append(ccOpts, chinacheck.WithLanguage(l))
			}
			cc := chinacheck.New(ccOpts...)

			fmt.Printf("即将预热 %d 个查询词（limit=%d，单条超时=%v，dry=%v）\n", len(queries), limit, timeout, dry)
			if dry {
				fmt.Println("（dry 模式：不实际打网络，仅打印清单并跳过）")
				for i, q := range queries {
					fmt.Printf("  [%3d] %s\n", i+1, q)
				}
				return nil
			}

			okCount := 0
			errCount := 0
			startAll := time.Now()
			for i, q := range queries {
				label := fmt.Sprintf("[%d/%d] %s", i+1, len(queries), q)
				singleCtx, cancel := context.WithTimeout(context.Background(), timeout)
				sr, err := cc.Search(singleCtx, q, limit)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ %s SEARCH 失败: %v\n", label, err)
					errCount++
					cancel()
					continue
				}
				hits := len(sr.Companies)
				if hits > 0 {
					best := sr.Companies[0]
					if _, err := cc.GetSnapshot(singleCtx, best.CompanyID, ""); err != nil {
						fmt.Fprintf(os.Stderr, "  ⚠ %s SNAPSHOT 失败（搜索已写入缓存）: %v\n", label, err)
						errCount++
					} else {
						fmt.Printf("  ✓ %s 命中 %d 条 → Top1: %s (信用代码 %s)\n", label, hits, best.NameZh, best.RegistrationNo)
						okCount++
					}
				} else {
					fmt.Printf("  · %s 搜索无命中（已写入空结果缓存以避免重复打网）\n", label)
					okCount++
				}
				cancel()
			}
			st := ca.Stats()
			fmt.Println()
			fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Printf(" 完成 %d 个（成功 %d，失败 %d），耗时 %v\n", len(queries), okCount, errCount, time.Since(startAll).Round(time.Millisecond))
			fmt.Printf(" 缓存现状：%d 条，文件 %s\n", st.Count, humanBytes(st.FileSizeByte))
			if errCount > 0 {
				fmt.Printf(" 失败的条目可稍后再跑（相同查询词不会重复打网）。\n")
			}
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "查询词列表文件（每行一个，# 注释，空行忽略）")
	cmd.Flags().String("queries", "", "逗号分隔的查询词列表，如 \"腾讯,阿里巴巴,字节跳动\"")
	cmd.Flags().Int("limit", 5, "每条查询返回的搜索结果数上限（默认 5；snapshot 仅对 Top1 拉取）")
	cmd.Flags().String("timeout", "45s", "单条查询总超时，如 30s / 2m / 1h")
	cmd.Flags().Bool("dry", false, "dry run：只打印清单，不实际打网络请求")
	cmd.Flags().String("cache-path", "", "自定义 MySQL DSN（空值=使用 env GEO_CHINACHECK_MYSQL_DSN；若传入旧路径形如 @tcp(...) 也视为 DSN）")
	cmd.Flags().String("url", "", "自定义 China-Check MCP endpoint（默认官方公共端点）")
	cmd.Flags().String("lang", "zh", "enum/标签字段语言（zh/en/ja/ko 等）")
	return cmd
}

// ---------- 工具函数 ----------

// openCacheFromFlags 根据命令中 --cache-path flag + 环境变量打开缓存。
func openCacheFromFlags(cmd *cobra.Command) (chinacheck.Cache, error) {
	path := ""
	if cmd != nil {
		if p, err := cmd.Flags().GetString("cache-path"); err == nil && p != "" {
			path = p
		}
	}
	opts := []chinacheck.CacheOption{}
	if maxStr, ok := os.LookupEnv("GEO_CHINACHECK_CACHE_MAX_ITEMS"); ok {
		var n int
		if _, err := fmt.Sscanf(maxStr, "%d", &n); err == nil && n > 0 {
			opts = append(opts, chinacheck.WithMaxItems(n))
		}
	}
	if ttlStr, ok := os.LookupEnv("GEO_CHINACHECK_CACHE_TTL_HOURS"); ok {
		var h int
		if _, err := fmt.Sscanf(ttlStr, "%d", &h); err == nil && h > 0 {
			opts = append(opts, chinacheck.WithTTL(time.Duration(h)*time.Hour))
		}
	}
	return chinacheck.NewCache(path, opts...)
}

// collectImportQueries 从 import 命令的 -f 或 --queries 中收集查询词（去重、过滤空行）。
func collectImportQueries(cmd *cobra.Command) ([]string, error) {
	var out []string
	seen := map[string]bool{}

	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			return
		}
		seen[q] = true
		out = append(out, q)
	}

	if qs, err := cmd.Flags().GetString("queries"); err == nil && qs != "" {
		for _, p := range strings.Split(qs, ",") {
			add(p)
		}
	}
	if path, err := cmd.Flags().GetString("file"); err == nil && path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("打开 -f 文件失败: %w", err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// 支持每行 JSON 的格式：{"query":"腾讯"}
			if strings.HasPrefix(line, "{") {
				var obj struct {
					Query string `json:"query"`
					Q     string `json:"q"`
					Brand string `json:"brand"`
					Name  string `json:"name"`
				}
				if err := json.Unmarshal([]byte(line), &obj); err == nil {
					for _, cand := range []string{obj.Query, obj.Q, obj.Brand, obj.Name} {
						add(cand)
					}
					continue
				}
			}
			add(line)
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("读取 -f 文件失败 (line %d): %w", lineNo, err)
		}
	}
	return out, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// humanBytes 把字节数格式化成易读字符串（1 KB / 2.3 MB / 5 GB）。
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	idx := 0
	v := float64(n) / 1024
	for v >= 1024 && idx < len(units)-1 {
		v /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f %s", v, units[idx])
}

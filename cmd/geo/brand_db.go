package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"

	"my-geo/internal/brand/offlinedb"
)

// newBrandDBCmd 创建 brand-db 子命令：离线工商 MySQL 数据库管理。
//
// 数据来源：https://github.com/guichong/-/tree/json （1978-2019 年 31 省 1000万+ 条）
//
// 推荐工作流：
//  1. git clone --depth 1 -b json https://github.com/guichong/- ~/geo-enterprise-json
//     （文件太多很大，建议仅 clone json 分支，且可 --filter=blob:none 或用浅克隆）
//  2. geo brand-db import-file -f ~/geo-enterprise-json/Enterprise-Registration-Data/json/2018/广东省.json
//     或批量：geo brand-db import-file -d ~/geo-enterprise-json/Enterprise-Registration-Data/json/
//  3. 不想自己 clone：用 geo brand-db import-github --years 2019 --provinces 广东,北京 直连 raw 下载
func newBrandDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "brand-db",
		Short: "离线工商注册信息 MySQL 数据库管理（1978-2019，1000万+ 种子数据）",
		Long: `离线工商注册信息数据库管理（MySQL 8.0+；使用 FULLTEXT ngram 中文分词；DSN 用 GEO_OFFLINE_MYSQL_DSN 设置）。

数据源：https://github.com/guichong/-/tree/json
  10 字段：企业名称 / 统一社会信用代码 / 注册日期 / 企业类型 / 法人代表 / 注册资金 / 经营范围 / 省份 / 地市 / 注册地址
  覆盖：中国大陆 31 省份，1978-2019，1000万+ 条（JSON 数组/JSONL 格式自动识别）

快速开始：
  # 方式 A：本地已有仓库（推荐，1000万条整体走 git 更稳）
  git clone --depth 1 -b json https://github.com/guichong/- ~/geo-erddb
  geo brand-db import-file -d ~/geo-erddb/Enterprise-Registration-Data/json/2019

  # 方式 B：直接从 GitHub raw 下载指定年份+省份（适合少量样本）
  geo brand-db import-github --years 2018,2019 --provinces 广东,北京,上海

  # 使用
  geo brand-db stats                          # 统计总数/省分布/文件大小
  geo brand-db search "腾讯" -n 5              # 模糊搜索 Top 5
  geo brand-db init                            # 仅建库不导入（建空表）
  geo brand-db clear                           # 清空整个库并回收空间`,
	}
	cmd.AddCommand(
		newBrandDBInitCmd(),
		newBrandDBStatsCmd(),
		newBrandDBSearchCmd(),
		newBrandDBClearCmd(),
		newBrandDBImportFileCmd(),
		newBrandDBImportGithubCmd(),
	)
	// 所有子命令共享 --db 路径参数；优先级：--db > GEO_OFFLINE_DB_PATH > 默认路径
	defaultDB := ""
	if v := strings.TrimSpace(os.Getenv("GEO_OFFLINE_DB_PATH")); v != "" {
		defaultDB = v
	} else if v := strings.TrimSpace(os.Getenv("GEO_OFFLINE_DB")); v != "" {
		defaultDB = v
	}
	cmd.PersistentFlags().StringP("db", "b", defaultDB, `MySQL DSN（空值=使用 env GEO_OFFLINE_MYSQL_DSN；也可设 GEO_OFFLINE_DB_PATH=DSN 兼容旧变量）`)
	return cmd
}

func openOfflineDB(cmd *cobra.Command) (offlinedb.DB, error) {
	path, _ := cmd.Flags().GetString("db")
	return offlinedb.Open(path)
}

func newBrandDBInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "创建数据库文件并初始化表/索引（空库）",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := signalCtx()
			defer cancel()
			st, err := db.Stats(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("✓ 数据库已就绪：%s（当前 %d 条，文件 %s）\n", st.Path, st.Count, humanBytes(st.FileSize))
			return nil
		},
	}
}

func newBrandDBStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "数据库统计（总数/文件大小/按省 Top10）",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := signalCtx()
			defer cancel()
			st, err := db.Stats(ctx)
			if err != nil {
				return err
			}
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println(" 离线工商注册数据库 统计")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf(" 文件路径:   %s\n", st.Path)
			fmt.Printf(" 总记录数:   %s 条\n", humanize.Comma(st.Count))
			fmt.Printf(" 文件大小:   %s\n", humanBytes(st.FileSize))
			if len(st.Provinces) > 0 {
				type kv struct {
					k string
					v int64
				}
				list := make([]kv, 0, len(st.Provinces))
				for k, v := range st.Provinces {
					list = append(list, kv{k, v})
				}
				slices.SortFunc(list, func(a, b kv) int { return cmp.Compare(b.v, a.v) })
				fmt.Println(" 按省 Top10:")
				total := st.Count
				if total == 0 {
					total = 1
				}
				for _, x := range list {
					pct := float64(x.v) * 100 / float64(total)
					fmt.Printf("   %-10s %10s 条  (%5.1f%%)\n", x.k, humanize.Comma(x.v), pct)
				}
			}
			return nil
		},
	}
}

func newBrandDBSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "按公司名/品牌/法人/地址模糊搜索",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			n, _ := cmd.Flags().GetInt("n")
			prov, _ := cmd.Flags().GetString("province")
			city, _ := cmd.Flags().GetString("city")
			jsonFmt, _ := cmd.Flags().GetBool("json")
			ctx, cancel := signalCtx()
			defer cancel()
			start := time.Now()
			out, err := db.Search(ctx, offlinedb.SearchOptions{
				Query: strings.Join(args, " "), TopN: n, Province: prov, City: city,
			})
			if err != nil {
				return err
			}
			if jsonFmt {
				e := json.NewEncoder(os.Stdout)
				e.SetIndent("", "  ")
				return e.Encode(map[string]interface{}{"query": args, "took_ms": time.Since(start).Milliseconds(), "count": len(out), "result": out})
			}
			fmt.Printf("命中 %d 条（耗时 %v）\n", len(out), time.Since(start).Round(time.Millisecond))
			for i, c := range out {
				fmt.Printf("  [%2d] 🏢 %s  [匹配度 %.0f%%]\n", i+1, c.Name, c.Score)
				fmt.Printf("       信用代码: %s | 法人: %s | 成立: %s\n", dash(c.Code), dash(c.LegalRepresentative), dash(c.RegistrationDay))
				fmt.Printf("       资本: %s | 类型: %s | 地点: %s / %s / %s\n", dash(c.Capital), dash(c.Character), dash(c.Province), dash(c.City), dash(c.Address))
			}
			return nil
		},
	}
	cmd.Flags().IntP("n", "n", 10, "返回条数")
	cmd.Flags().String("province", "", "只在某省搜索（如：广东）")
	cmd.Flags().String("city", "", "只在某市搜索（如：深圳）")
	cmd.Flags().Bool("json", false, "JSON 格式输出")
	return cmd
}

func newBrandDBClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "清空所有公司记录",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := signalCtx()
			defer cancel()
			before, _ := db.Stats(ctx)
			fmt.Printf("⚠ 即将清空数据库 %s（当前 %s 条，%s）\n", before.Path, humanize.Comma(before.Count), humanBytes(before.FileSize))
			fmt.Print("  输入 YES 确认：")
			var confirm string
			if _, err := fmt.Scanln(&confirm); err != nil || confirm != "YES" {
				fmt.Println("已取消。")
				return nil
			}
			start := time.Now()
			if err := db.Clear(ctx); err != nil {
				return fmt.Errorf("清空失败: %w", err)
			}
			after, _ := db.Stats(ctx)
			fmt.Printf("✓ 已清空（耗时 %v）：文件 %s → %s\n", time.Since(start).Round(time.Millisecond), humanBytes(before.FileSize), humanBytes(after.FileSize))
			return nil
		},
	}
}

func newBrandDBImportFileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-file",
		Short: "从本地 JSON 文件或目录导入（支持 JSON 数组和 JSONL 两种格式，目录会递归）",
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			dir, _ := cmd.Flags().GetString("dir")
			batch, _ := cmd.Flags().GetInt("batch")
			if file == "" && dir == "" {
				return fmt.Errorf("请通过 -f <单个 json 文件> 或 -d <目录> 指定导入来源")
			}
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := signalCtx()
			defer cancel()

			beforeAll := time.Now()
			var total offlinedb.ImportResult
			if file != "" {
				fmt.Printf("导入文件: %s（batch=%d）...\n", file, batch)
				r, err := db.ImportJSONFile(ctx, file, batch)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ 失败: %v\n", err)
				}
				total = r
			}
			if dir != "" {
				fmt.Printf("导入目录: %s（递归，batch=%d）...\n", dir, batch)
				start := time.Now()
				lastPrint := start
				count := 0
				// 手动 Walk 打印进度
				err := filepath.WalkDir(dir, func(path string, de os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if de.IsDir() {
						return nil
					}
					if !strings.HasSuffix(strings.ToLower(de.Name()), ".json") {
						return nil
					}
					count++
					r, ferr := db.ImportJSONFile(ctx, path, batch)
					total.Inserted += r.Inserted
					total.Skipped += r.Skipped
					total.Failed += r.Failed
					total.Files += r.Files
					if ferr != nil {
						fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", path, ferr)
					}
					if time.Since(lastPrint) > 3*time.Second {
						fmt.Printf("  · 进度 %d files：新增 %s，跳过 %s，失败 %s（已用时 %v）\n",
							count,
							humanize.Comma(total.Inserted),
							humanize.Comma(total.Skipped),
							humanize.Comma(total.Failed),
							time.Since(start).Round(time.Second))
						lastPrint = time.Now()
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
			st, _ := db.Stats(ctx)
			fmt.Println()
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf(" 导入完成：%d files，新增 %s，跳过 %s（重复信用代码），失败 %s，总耗时 %v\n",
				total.Files,
				humanize.Comma(total.Inserted),
				humanize.Comma(total.Skipped),
				humanize.Comma(total.Failed),
				time.Since(beforeAll).Round(time.Millisecond))
			fmt.Printf(" 数据库现状：%s 条，文件 %s\n", humanize.Comma(st.Count), humanBytes(st.FileSize))
			return nil
		},
	}
	cmd.Flags().StringP("file", "f", "", "单个 JSON 文件路径（JSON 数组 / JSONL 自动识别）")
	cmd.Flags().StringP("dir", "d", "", "目录路径（递归查找 .json 文件）")
	cmd.Flags().Int("batch", 2000, "批量插入的事务批大小")
	return cmd
}

func newBrandDBImportGithubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-github",
		Short: "直接从 GitHub raw 下载指定年份+省份的 JSON 并导入（适合小样本）",
		Long: `按 --years 和 --provinces 指定范围，直连：
  https://raw.githubusercontent.com/guichong/-/json/Enterprise-Registration-Data/json/<year>/<province>.json
下载到 --tmpdir，再批量 import-file。

⚠ 注意：GitHub raw 对大文件（几十 MB 以上）和高频请求有限流。
     如果你需要 1000 万条全量，推荐本地 git clone 后用 import-file -d。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			yearsStr, _ := cmd.Flags().GetString("years")
			provStr, _ := cmd.Flags().GetString("provinces")
			tmpdir, _ := cmd.Flags().GetString("tmpdir")
			baseURL, _ := cmd.Flags().GetString("base-url")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			keepFiles, _ := cmd.Flags().GetBool("keep")

			years := splitCSV(yearsStr)
			provs := splitCSV(provStr)
			if len(years) == 0 || len(provs) == 0 {
				return fmt.Errorf("--years 和 --provinces 都必填（如 --years 2018,2019 --provinces 广东,北京）")
			}
			if tmpdir == "" {
				t, err := os.MkdirTemp("", "geo-chinacheck-import-github-*")
				if err != nil {
					return err
				}
				tmpdir = t
				if !keepFiles {
					defer os.RemoveAll(tmpdir)
				}
			} else {
				if err := os.MkdirAll(tmpdir, 0o755); err != nil {
					return err
				}
			}
			fmt.Printf("临时下载目录: %s\n", tmpdir)
			hc := &http.Client{Timeout: timeout}
			var files []string
			for _, y := range years {
				for _, p := range provs {
					// URL 编码省份名（含中文）
					u := strings.TrimRight(baseURL, "/") + "/" + y + "/" + url.PathEscape(p+".json")
					dst := filepath.Join(tmpdir, y+"_"+sanitizeFileName(p)+".json")
					fmt.Printf("  ↓ %s/%s -> %s ... ", y, p, filepath.Base(dst))
					took := time.Now()
					size, err := download(hc, u, dst)
					if err != nil {
						fmt.Printf("失败: %v\n", err)
						continue
					}
					fmt.Printf("OK (%s, %v)\n", humanBytes(size), time.Since(took).Round(time.Millisecond))
					files = append(files, dst)
				}
			}
			if len(files) == 0 {
				return fmt.Errorf("没有成功下载任何文件")
			}
			fmt.Printf("下载 %d files，开始导入...\n", len(files))
			db, err := openOfflineDB(cmd)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := signalCtx()
			defer cancel()
			var total offlinedb.ImportResult
			for _, f := range files {
				r, err := db.ImportJSONFile(ctx, f, 2000)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ 导入失败 %s: %v\n", f, err)
					continue
				}
				total.Inserted += r.Inserted
				total.Skipped += r.Skipped
				total.Failed += r.Failed
				total.Files += r.Files
			}
			st, _ := db.Stats(ctx)
			fmt.Println()
			fmt.Printf("✓ 下载+导入完成：新增 %s，跳过 %s，失败 %s，共 %d files\n",
				humanize.Comma(total.Inserted), humanize.Comma(total.Skipped), humanize.Comma(total.Failed), total.Files)
			fmt.Printf("  DB 现状：%s 条，文件 %s\n", humanize.Comma(st.Count), humanBytes(st.FileSize))
			if keepFiles {
				fmt.Printf("  下载文件保存在: %s\n", tmpdir)
			}
			return nil
		},
	}
	cmd.Flags().String("years", "2019", "年份列表（逗号分隔），如 2018,2019")
	cmd.Flags().String("provinces", "", "省份列表（逗号分隔），如 广东,北京,上海,浙江省")
	cmd.Flags().String("base-url", "https://raw.githubusercontent.com/guichong/-/json/Enterprise-Registration-Data/json", "GitHub raw 基础 URL")
	cmd.Flags().String("tmpdir", "", "下载临时目录（默认自动创建，完成后删除）")
	cmd.Flags().Duration("timeout", 15*time.Minute, "单个文件下载总超时")
	cmd.Flags().Bool("keep", false, "保留下载到本地的原始 JSON 文件（与 --tmpdir 搭配）")
	return cmd
}

// ---------- 通用工具 ----------

func signalCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		fmt.Fprintln(os.Stderr, "\n收到中断，正在停止...")
		cancel()
	}()
	return ctx, func() { signal.Stop(ch); cancel() }
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func sanitizeFileName(s string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", " ", "_",
		"?", "_", "*", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return replacer.Replace(s)
}

// download 下载 URL 到本地 dst 文件（支持续传？简化版：直接完整下载）
func download(hc *http.Client, u, dst string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, resp.Body)
}

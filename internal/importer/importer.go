// Package importer 提供离线工商注册库的数据装载能力，
// 原 CLI 命令 `geo brand db import-*` 的逻辑迁移至此，供 Web 后端端点复用。
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"my-geo/internal/brand/offlinedb"
)

// sanitizeFileName 将省份名等转成安全的文件名片段（去空格/特殊字符）。
func sanitizeFileName(s string) string {
	r := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return r.Replace(s)
}

// download 将 url 下载到 dst，返回字节数。
func download(ctx context.Context, hc *http.Client, u, dst string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("下载返回 %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// GitHubImport 按 years/provinces 从 baseURL 直连下载并导入工商 JSON。
//
// baseURL 形如：https://raw.githubusercontent.com/guichong/-/json/Enterprise-Registration-Data/json
// 最终拼接：<baseURL>/<year>/<url.PathEscape(province)>.json
func GitHubImport(ctx context.Context, store offlinedb.DB, years, provinces []string, baseURL string, timeout time.Duration, batch int) (offlinedb.ImportResult, error) {
	var total offlinedb.ImportResult
	if len(years) == 0 || len(provinces) == 0 {
		return total, fmt.Errorf("years 和 provinces 均不能为空")
	}
	if baseURL == "" {
		baseURL = "https://raw.githubusercontent.com/guichong/-/json/Enterprise-Registration-Data/json"
	}
	tmpdir, err := os.MkdirTemp("", "geo-chinacheck-import-github-*")
	if err != nil {
		return total, err
	}
	defer os.RemoveAll(tmpdir)

	hc := &http.Client{Timeout: timeout}
	for _, y := range years {
		for _, p := range provinces {
			select {
			case <-ctx.Done():
				return total, ctx.Err()
			default:
			}
			u := strings.TrimRight(baseURL, "/") + "/" + y + "/" + url.PathEscape(p+".json")
			dst := filepath.Join(tmpdir, y+"_"+sanitizeFileName(p)+".json")
			if _, err := download(ctx, hc, u, dst); err != nil {
				return total, fmt.Errorf("下载 %s/%s 失败: %w", y, p, err)
			}
			r, ierr := store.ImportJSONFile(ctx, dst, batch)
			if ierr != nil {
				return total, fmt.Errorf("导入 %s/%s 失败: %w", y, p, ierr)
			}
			total.Inserted += r.Inserted
			total.Skipped += r.Skipped
			total.Failed += r.Failed
			total.Files += r.Files
		}
	}
	return total, nil
}

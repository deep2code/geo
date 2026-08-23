// meili.go: Meilisearch 轻量 HTTP 客户端（仅用标准库，无第三方依赖）。
//
// 工商库中文全文检索已迁移至 Meilisearch：MariaDB 仅作主存储（单一事实来源），
// 搜索经外部 Meilisearch 完成（MariaDB 不支持 MySQL 的 ngram 解析器）。
// 文档主键用字符串字段 meili_id（= Company.ID 的字符串形式），避免数值主键兼容性顾虑。
package offlinedb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const meiliIndex = "companies"

// meiliClient 封装 Meilisearch REST API。
type meiliClient struct {
	host  string
	apiKey string
	index string
	http  *http.Client
}

func newMeiliClient(host, apiKey string) *meiliClient {
	return &meiliClient{
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
		index:  meiliIndex,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (m *meiliClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.host+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("meilisearch %s %s -> %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ensureIndex 幂等创建索引并配置中文可搜索/可过滤字段；失败仅告警（搜索降级 LIKE）。
func (m *meiliClient) ensureIndex(ctx context.Context) error {
	var exist struct {
		UID string `json:"uid"`
	}
	if err := m.do(ctx, http.MethodGet, "/indexes/"+m.index, nil, &exist); err != nil {
		create := map[string]any{"uid": m.index, "primaryKey": "meili_id"}
		if cerr := m.do(ctx, http.MethodPost, "/indexes", create, nil); cerr != nil {
			return fmt.Errorf("创建 Meilisearch 索引失败: %w", cerr)
		}
	}
	settings := map[string]any{
		"searchableAttributes": []string{"name", "business_scope", "legal_representative", "address"},
		"filterableAttributes": []string{"province", "city"},
	}
	if err := m.do(ctx, http.MethodPatch, "/indexes/"+m.index+"/settings", settings, nil); err != nil {
		slog.Warn("配置 Meilisearch 索引设置失败（可搜索/可过滤属性可能未生效）", slog.Any("error", err))
	}
	return nil
}

// companyDoc 将 Company 转为 Meilisearch 文档（含字符串主键 meili_id）。
func companyDoc(c Company) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["meili_id"] = strconv.FormatInt(c.ID, 10)
	return m
}

// AddDocuments 批量索引公司文档（内部自动分片，避免单次请求体过大）。
func (m *meiliClient) AddDocuments(ctx context.Context, companies []Company) error {
	const shard = 1000
	for i := 0; i < len(companies); i += shard {
		end := i + shard
		if end > len(companies) {
			end = len(companies)
		}
		docs := make([]map[string]any, 0, end-i)
		for _, c := range companies[i:end] {
			docs = append(docs, companyDoc(c))
		}
		if err := m.do(ctx, http.MethodPost, "/indexes/"+m.index+"/documents", docs, nil); err != nil {
			return err
		}
	}
	return nil
}

// Search 中文全文检索，返回按相关性排序的 Company（Score 归一化到 0-100）。
func (m *meiliClient) Search(ctx context.Context, query, province, city string, topN int) ([]Company, error) {
	filter := []string{}
	if province != "" {
		filter = append(filter, "province = "+quoteMeili(province))
	}
	if city != "" {
		filter = append(filter, "city = "+quoteMeili(city))
	}
	reqBody := map[string]any{"q": query, "limit": topN}
	if len(filter) > 0 {
		reqBody["filter"] = strings.Join(filter, " AND ")
	}
	var resp struct {
		Hits []struct {
			Company
			Score float64 `json:"_score"`
		} `json:"hits"`
	}
	if err := m.do(ctx, http.MethodPost, "/indexes/"+m.index+"/search", reqBody, &resp); err != nil {
		return nil, err
	}
	out := make([]Company, len(resp.Hits))
	var maxS float64
	for i, h := range resp.Hits {
		out[i] = h.Company
		if h.Score > maxS {
			maxS = h.Score
		}
	}
	if maxS > 0 {
		for i := range out {
			out[i].Score = 100 * (resp.Hits[i].Score / maxS)
		}
	}
	return out, nil
}

// DeleteAll 清空索引内全部文档。
func (m *meiliClient) DeleteAll(ctx context.Context) error {
	return m.do(ctx, http.MethodDelete, "/indexes/"+m.index+"/documents", nil, nil)
}

// quoteMeili 对 Meilisearch filter 值做安全的字符串字面量包裹。
//
// Meilisearch filter 语法中属性值可用单引号字符串字面量包裹，内部单引号用
// 两个单引号转义（''）。单引号字符串字面量对空格、保留字（AND/OR/TO/IN 等）
// 均安全，比裸双引号更稳健。
func quoteMeili(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

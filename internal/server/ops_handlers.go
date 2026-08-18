package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"my-geo/internal/brand/offlinedb"
	"my-geo/internal/config"
	"my-geo/internal/eval"
	"my-geo/internal/importer"
	"my-geo/pkg/geo"
)

// newGeoEngineFromEnv 从环境变量构建内容优化引擎（供评测等需要独立引擎的场景复用）。
func newGeoEngineFromEnv() *geo.Engine {
	key := config.Env("GEO_LLM_KEY", "")
	base := config.Env("GEO_LLM_BASE", "")
	model := config.Env("GEO_LLM_MODEL", "")
	budget := 0.0
	if v := config.Env("GEO_LLM_BUDGET_USD", ""); v != "" {
		if f, err := parseFloat(v); err == nil {
			budget = f
		}
	}
	e := geo.New(geo.WithOpenAI(key, base, model), geo.WithBudgetUSD(budget))
	if rsPath := config.Env("GEO_RULES", ""); rsPath != "" {
		if rs, err := config.LoadRuleSet(rsPath); err == nil {
			e.ApplyRuleSet(rs)
		}
	}
	return e
}

// ───────────────────────── 规则集管理（替代 geo rules） ─────────────────────────

// handleRulesList GET /api/v1/rules 列出可用规则集（内置默认 + config/rules/*.json）。
func (s *Server) handleRulesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	items := []map[string]interface{}{
		{
			"name":    "default",
			"version": "builtin-1.0.0",
			"source":  "内置（代码基线）",
			"valid":   true,
		},
	}
	dir := "config/rules"
	entries, err := os.ReadDir(dir)
	if err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".json" {
				continue
			}
			names = append(names, e.Name())
		}
		for _, n := range names {
			rs, lerr := config.LoadRuleSet(filepath.Join(dir, n))
			if lerr != nil {
				items = append(items, map[string]interface{}{
					"name": n, "version": "-", "source": "config/rules/" + n,
					"valid": false, "error": lerr.Error(),
				})
				continue
			}
			items = append(items, map[string]interface{}{
				"name":    rs.Name,
				"version": rs.Version,
				"source":  "config/rules/" + n,
				"valid":   true,
				"engine":  rs.Engine,
				"domain":  rs.Domain,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"rulesets": items})
}

// handleRulesDefault GET /api/v1/rules/default 返回内置默认规则集 JSON。
func (s *Server) handleRulesDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	rs := config.DefaultRuleSet()
	writeJSON(w, http.StatusOK, rs)
}

// handleRulesValidate POST /api/v1/rules/validate 校验规则集 JSON。
// 请求体：{"content": "<ruleset json 字符串>"} 或 {"path": "<文件或 config/rules 下路径>"}。
func (s *Server) handleRulesValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var body struct {
		Content string `json:"content"`
		Path    string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请求体解析失败: " + err.Error()})
		return
	}
	var rs *config.RuleSet
	var err error
	switch {
	case body.Path != "":
		rs, err = config.LoadRuleSet(body.Path)
	case body.Content != "":
		rs = &config.RuleSet{}
		err = json.Unmarshal([]byte(body.Content), rs)
	default:
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请提供 content（规则集 JSON）或 path（文件路径）"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "error": err.Error(),
		})
		return
	}
	if verr := rs.Validate(); verr != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "error": verr.Error(),
			"name": rs.Name, "version": rs.Version,
			"weights": len(rs.Weights),
			"strategy_effectiveness": len(rs.StrategyEffectiveness),
			"strategy_triggers": len(rs.StrategyTriggers),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid": true,
		"name": rs.Name, "version": rs.Version,
		"engine": rs.Engine, "domain": rs.Domain,
		"weights": len(rs.Weights),
		"strategy_effectiveness": len(rs.StrategyEffectiveness),
		"strategy_triggers": len(rs.StrategyTriggers),
	})
}

// ───────────────────────── GEO 评测（替代 geo evaluate） ─────────────────────────

type evaluateRequest struct {
	Dataset  string `json:"dataset"`  // 评测集 JSON 字符串（必填）
	Format   string `json:"format"`   // md | json，默认 md
	Live     bool   `json:"live"`     // 是否接入真实生成式引擎实测引用
	LLMKey   string `json:"llm_key"`  // live 模式 API Key（缺省读 GEO_LLM_KEY）
	LLMBase  string `json:"llm_base"` // live 模式 Base URL（缺省 https://api.openai.com/v1）
	LLMModel string `json:"llm_model"`// live 模式模型（缺省 gpt-4o-mini）
	Rules    string `json:"rules"`    // 规则集路径或 JSON 内容（可选）
}

// handleEvaluate POST /api/v1/evaluate 运行评测集，返回 md 或 json 报告。
func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	var req evaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请求体解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Dataset) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请提供 dataset（评测集 JSON）"})
		return
	}
	var bench eval.Benchmark
	if err := json.Unmarshal([]byte(req.Dataset), &bench); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "评测集解析失败: " + err.Error()})
		return
	}

	engine := newGeoEngineFromEnv()
	if req.Rules != "" {
		rs, rerr := loadRuleSetFlexible(req.Rules)
		if rerr != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "规则集加载失败: " + rerr.Error()})
			return
		}
		engine.ApplyRuleSet(rs)
	}

	var opts []eval.EvalOption
	if req.Live {
		key := req.LLMKey
		if key == "" {
			key = config.Env("GEO_LLM_KEY", "")
		}
		base := req.LLMBase
		if base == "" {
			base = config.Env("GEO_LLM_BASE", "https://api.openai.com/v1")
		}
		model := req.LLMModel
		if model == "" {
			model = config.Env("GEO_LLM_MODEL", "gpt-4o-mini")
		}
		if key == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "live 模式需要 LLM API Key（请求 llm_key 或环境变量 GEO_LLM_KEY）"})
			return
		}
		opts = append(opts, eval.WithLiveChecker(eval.NewHTTPLiveChecker(base, model, key)))
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	report, err := eval.Evaluate(ctx, engine, &bench, opts...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "评测运行失败: " + err.Error()})
		return
	}

	format := req.Format
	if format != "json" {
		format = "md"
	}
	var rendered string
	if format == "json" {
		b, jerr := eval.RenderJSON(report)
		if jerr != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "报告渲染失败: " + jerr.Error()})
			return
		}
		rendered = string(b)
	} else {
		rendered = eval.RenderMarkdown(report)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"report": rendered,
		"format": format,
		"mode":   report.Mode,
	})
}

// ───────────────── 离线工商库导入（替代 geo brand db import-*） ─────────────────

// handleOfflineDBImport POST /api/v1/brand/offlinedb/import 上传 JSON 文件导入。
func (s *Server) handleOfflineDBImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	odb := s.brandEngineOfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "离线工商库未启用（请配置 GEO_OFFLINE_MYSQL_DSN）"})
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "表单解析失败: " + err.Error()})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请上传 file 字段（JSON 文件）"})
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "geo-offlinedb-import-*.json")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "创建临时文件失败: " + err.Error()})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "写入临时文件失败: " + err.Error()})
		return
	}
	tmp.Close()

	batch := 2000
	if v := r.FormValue("batch"); v != "" {
		if n, perr := parseInt(v); perr == nil && n > 0 {
			batch = n
		}
	}
	res, err := odb.ImportJSONFile(r.Context(), tmpPath, batch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "导入失败: " + err.Error()})
		return
	}
	st, _ := odb.Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": res.Inserted, "skipped": res.Skipped, "failed": res.Failed, "files": res.Files,
		"db_count": st.Count, "db_file_size_bytes": st.FileSize,
	})
}

// handleOfflineDBImportGitHub POST /api/v1/brand/offlinedb/import-github 直连下载并导入。
func (s *Server) handleOfflineDBImportGitHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 POST"})
		return
	}
	odb := s.brandEngineOfflineDB()
	if odb == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "离线工商库未启用（请配置 GEO_OFFLINE_MYSQL_DSN）"})
		return
	}
	var body struct {
		Years    string `json:"years"`    // 逗号分隔，如 2018,2019
		Provinces string `json:"provinces"` // 逗号分隔，如 广东,北京
		BaseURL  string `json:"base_url"`
		Timeout  int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "请求体解析失败: " + err.Error()})
		return
	}
	years := splitCSV(body.Years)
	provs := splitCSV(body.Provinces)
	if len(years) == 0 || len(provs) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "years 与 provinces 均必填（逗号分隔）"})
		return
	}
	timeout := 15 * time.Minute
	if body.Timeout > 0 {
		timeout = time.Duration(body.Timeout) * time.Second
	}
	res, err := importer.GitHubImport(r.Context(), odb, years, provs, body.BaseURL, timeout, 2000)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "导入失败: " + err.Error()})
		return
	}
	st, _ := odb.Stats(r.Context())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": res.Inserted, "skipped": res.Skipped, "failed": res.Failed, "files": res.Files,
		"db_count": st.Count, "db_file_size_bytes": st.FileSize,
	})
}

// brandEngineOfflineDB 安全获取离线工商库存储（未启用时为 nil）。
func (s *Server) brandEngineOfflineDB() offlinedb.DB {
	if s.brandEngine == nil {
		return nil
	}
	return s.brandEngine.OfflineDB()
}

// ───────────────────────── 通用小工具 ─────────────────────────

// loadRuleSetFlexible 先按文件路径加载，失败则当作内联 JSON 内容解析。
func loadRuleSetFlexible(in string) (*config.RuleSet, error) {
	if rs, err := config.LoadRuleSet(in); err == nil {
		return rs, nil
	}
	rs := &config.RuleSet{}
	if err := json.Unmarshal([]byte(in), rs); err != nil {
		return nil, fmt.Errorf("既非有效路径也非合法规则集 JSON: %w", err)
	}
	return rs, nil
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

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%g", &f)
	return f, err
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

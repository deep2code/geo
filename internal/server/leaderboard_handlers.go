package server

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"my-geo/internal/brand"
	"my-geo/internal/httputil"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// handleBrandCompare 竞品对标矩阵接口。
//
// GET /api/v1/brand/compare?brands=A,B,C,D（最多 5 个品牌）
//
// 从 HistoryDB 取各品牌最新审计，返回 JSON：
//
//	{
//	  "brands":     [{name, score, grade, tier, entity_completeness, created_at, dimension_scores:{维度:分数|null}}],
//	  "dimensions": [维度名数组（按优先级排序）],
//	  "diffs":      [{brand_a, brand_b, delta_score, by_dimension:{维度:差值|"n/a"}}],
//	  "errors":     {品牌名: 错误说明}   // 仅在有缺失/失败时存在
//	}
//
// 维度缺失标注：某品牌缺少某维度时 dimension_scores 中对应值为 null（区别于 0 分）。
// diffs 中当任一品牌某维度无数据时，该维度标注 "n/a" 而非计算差值。
// 缺少审计记录的品牌在 brands 数组中对应位置为 null，并在 errors 中说明。
func (s *Server) handleBrandCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}

	brandsRaw := r.URL.Query().Get("brands")
	if strings.TrimSpace(brandsRaw) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brands 参数"})
		return
	}
	parts := strings.Split(brandsRaw, ",")
	brandList := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		brandList = append(brandList, name)
	}
	if len(brandList) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brands 参数为空"})
		return
	}
	if len(brandList) > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("最多支持 5 个品牌同时对比，当前 %d 个", len(brandList)),
		})
		return
	}

	ctx := r.Context()
	data, errorsMap := s.buildBrandCompareData(ctx, brandList)
	resp := map[string]interface{}{
		"brands":     data.Brands,
		"dimensions": data.Dimensions,
		"diffs":      data.Diffs,
	}
	if len(errorsMap) > 0 {
		resp["errors"] = errorsMap
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildBrandCompareData 构建竞品对标数据。
//
// 逻辑要点：
//   - 维度缺失标注：某品牌缺少某维度时 DimensionScores 中对应值为 nil（JSON null），
//     前端可区分"0 分"与"无数据"。
//   - 维度排序稳定化：按 dimensionPriority 优先级排序，未在列表中的按字母序追加。
//   - diffs 中 null 维度跳过：当任一品牌的某维度为 nil 时，diff 中该维度标注 "n/a"。
//   - 透传 tier / entity_completeness。
func (s *Server) buildBrandCompareData(ctx context.Context, brandList []string) (brandCompareData, map[string]string) {
	entries := make([]*brandCompareEntry, len(brandList))
	errorsMap := map[string]string{}

	for i, name := range brandList {
		rec, err := s.brandEngine.HistoryDB().Latest(ctx, name)
		if err != nil {
			entries[i] = &brandCompareEntry{Name: name, ErrMsg: "读取审计历史失败: " + err.Error()}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		if rec == nil || strings.TrimSpace(rec.ReportJSON) == "" {
			entries[i] = &brandCompareEntry{Name: name, ErrMsg: "未找到该品牌的审计记录（请先执行一次品牌审计）"}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		var vr brand.VisibilityReport
		if err := json.Unmarshal([]byte(rec.ReportJSON), &vr); err != nil {
			entries[i] = &brandCompareEntry{
				Name:   name,
				ErrMsg: "解析审计报告失败: " + err.Error(),
			}
			errorsMap[name] = entries[i].ErrMsg
			continue
		}
		entries[i] = &brandCompareEntry{Name: name, Record: rec, Report: &vr}
	}

	// 收集所有品牌出现过的维度（去重），并按优先级排序
	unifiedDims := []string{}
	dimSet := map[string]bool{}
	for _, e := range entries {
		if e == nil || e.Report == nil {
			continue
		}
		dims, _ := extractDimensionScores(e.Report)
		for _, d := range dims {
			if !dimSet[d] {
				dimSet[d] = true
				unifiedDims = append(unifiedDims, d)
			}
		}
	}
	if len(unifiedDims) == 0 {
		unifiedDims = make([]string, len(defaultCompareDimensions))
		copy(unifiedDims, defaultCompareDimensions)
	}
	// 维度排序稳定化：按优先级列表排序
	sortDimensionsByPriority(unifiedDims)

	// 构建每个品牌的对标结果（缺失维度填 nil，非缺失填实际分数指针）
	brandsOut := make([]*compareBrandResult, len(entries))
	for i, e := range entries {
		if e == nil || e.Report == nil {
			brandsOut[i] = nil
			continue
		}
		_, dimScores := extractDimensionScores(e.Report)
		finalScores := make(map[string]*float64, len(unifiedDims))
		for _, d := range unifiedDims {
			if v, ok := dimScores[d]; ok {
				// 品牌有该维度，使用实际分数（拷贝避免指针共享）
				vv := v
				finalScores[d] = &vv
			} else {
				// 品牌缺少该维度，nil 表示无数据
				finalScores[d] = nil
			}
		}
		createdAtStr := ""
		if e.Record != nil && e.Record.Generated > 0 {
			createdAtStr = time.Unix(e.Record.Generated, 0).Format(time.RFC3339)
		} else if !e.Report.GeneratedAt.IsZero() {
			createdAtStr = e.Report.GeneratedAt.Format(time.RFC3339)
		}
		var createdAtPtr *string
		if createdAtStr != "" {
			createdAtPtr = &createdAtStr
		}
		brandsOut[i] = &compareBrandResult{
			Name:               e.Name,
			Score:              e.Report.Score,
			Grade:              e.Report.Grade,
			Tier:               e.Report.Tier,
			EntityCompleteness: e.Report.EntityCompletenessScore,
			CreatedAt:          createdAtPtr,
			DimensionScores:    finalScores,
		}
	}

	// 构建两两品牌差异（任一品牌该维度为 nil 时标注 "n/a"）
	diffs := []compareDiffResult{}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a == nil || b == nil || a.Report == nil || b.Report == nil {
				continue
			}
			_, aScores := extractDimensionScores(a.Report)
			_, bScores := extractDimensionScores(b.Report)
			byDim := map[string]interface{}{}
			for _, d := range unifiedDims {
				aVal, aOk := aScores[d]
				bVal, bOk := bScores[d]
				if !aOk || !bOk {
					// 任一品牌该维度无数据，标注 n/a
					byDim[d] = "n/a"
				} else {
					byDim[d] = aVal - bVal
				}
			}
			diffs = append(diffs, compareDiffResult{
				BrandA:      a.Name,
				BrandB:      b.Name,
				DeltaScore:  a.Report.Score - b.Report.Score,
				ByDimension: byDim,
			})
		}
	}

	return brandCompareData{
		Brands:     brandsOut,
		Dimensions: unifiedDims,
		Diffs:      diffs,
	}, errorsMap
}

// handleBrandCompareExport 竞品对比报告导出 API。
//
// GET /api/v1/brand/compare/export?brands=A,B,C&format=html|json
//
// format=html：生成自包含 HTML 报告（内联 CSS + SVG 雷达图 + 对比表格 + 差异分析）
// format=json：直接返回 compare 数据（与 /api/v1/brand/compare 一致）
func (s *Server) handleBrandCompareExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	brandsRaw := r.URL.Query().Get("brands")
	if strings.TrimSpace(brandsRaw) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少 brands 参数"})
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "html"
	}
	if format != "html" && format != "json" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "format 仅支持 html 或 json"})
		return
	}
	parts := strings.Split(brandsRaw, ",")
	brandList := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		brandList = append(brandList, name)
	}
	if len(brandList) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "brands 参数为空"})
		return
	}
	if len(brandList) > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("最多支持 5 个品牌同时对比，当前 %d 个", len(brandList)),
		})
		return
	}

	data, errorsMap := s.buildBrandCompareData(r.Context(), brandList)

	if format == "json" {
		resp := map[string]interface{}{
			"brands":     data.Brands,
			"dimensions": data.Dimensions,
			"diffs":      data.Diffs,
		}
		if len(errorsMap) > 0 {
			resp["errors"] = errorsMap
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// format == html
	htmlOut := generateCompareHTML(data, errorsMap)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, htmlOut)
}

// getAllBrandLatestRecords 获取所有品牌的最新审计记录（每个品牌只保留最新一条），
// 并从 report_json 推断 category。返回的条目按 score 降序排序。
func (s *Server) getAllBrandLatestRecords(ctx context.Context) ([]leaderboardItem, error) {
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		return nil, fmt.Errorf("审计历史库未启用")
	}
	brands, err := s.brandEngine.HistoryDB().Brands(ctx)
	if err != nil {
		return nil, err
	}
	if len(brands) == 0 {
		return []leaderboardItem{}, nil
	}
	// 单轮批量查询（LatestForBrands 内部用 JOIN 下推聚合），避免逐品牌 N+1（P1-5）。
	recs, err := s.brandEngine.HistoryDB().LatestForBrands(ctx, brands)
	if err != nil {
		return nil, err
	}
	items := make([]leaderboardItem, 0, len(recs))
	for i := range recs {
		rec := &recs[i]
		cat, ind := inferCategoryFromReportJSON(rec.ReportJSON)
		items = append(items, leaderboardItem{
			BrandName: rec.BrandName,
			Score:     rec.Score,
			Grade:     rec.Grade,
			Tier:      rec.Tier,
			Category:  cat,
			Industry:  ind,
			Generated: rec.Generated,
		})
	}
	slices.SortFunc(items, func(a, b leaderboardItem) int { return cmp.Compare(b.Score, a.Score) })
	for i := range items {
		items[i].Rank = i + 1
	}
	return items, nil
}

// handleLeaderboardCategories 返回已有类目列表（去重排序）。
//
// GET /api/v1/leaderboard/categories
func (s *Server) handleLeaderboardCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	items, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	catSet := map[string]int{}
	for _, it := range items {
		catSet[it.Category]++
	}
	categories := make([]map[string]interface{}, 0, len(catSet))
	for cat, cnt := range catSet {
		categories = append(categories, map[string]interface{}{
			"category": cat,
			"count":    cnt,
		})
	}
	slices.SortFunc(categories, func(a, b map[string]interface{}) int {
		if c := cmp.Compare(b["count"].(int), a["count"].(int)); c != 0 {
			return c
		}
		return cmp.Compare(a["category"].(string), b["category"].(string))
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":      len(categories),
		"categories": categories,
	})
}

// handleLeaderboard 排行榜主接口。
//
// GET /api/v1/leaderboard?category=xxx&limit=100
//   - category 可选：空或 "全部" 返回所有类目；否则按类目过滤
//   - limit 可选：默认 50，最大 500
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	_, limit := httputil.OffsetLimit(r, 50, 500)
	items, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	var filtered []leaderboardItem
	if category == "" || strings.EqualFold(category, "all") || strings.EqualFold(category, "全部") {
		filtered = items
	} else {
		filtered = make([]leaderboardItem, 0, len(items))
		for _, it := range items {
			if strings.EqualFold(it.Category, category) {
				it.Rank = len(filtered) + 1
				filtered = append(filtered, it)
			}
		}
	}
	if limit < len(filtered) {
		filtered = filtered[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"category":    ternary(category == "", "全部", category),
		"limit":       limit,
		"count":       len(filtered),
		"total":       len(items),
		"leaderboard": filtered,
	})
}

// handleLeaderboardBrand 单品牌历史走势与排名。
//
// GET /api/v1/leaderboard/brand/:brand
// 路径示例：/api/v1/leaderboard/brand/腾讯
//   - 从 URL Path 提取品牌名（去掉 /api/v1/leaderboard/brand/ 前缀）
//   - 返回当前排名 + 该品牌所有历史审计记录（时间序列）
//   - 可选参数 history_limit：历史记录条数，默认 50，最大 500
func (s *Server) handleLeaderboardBrand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "仅支持 GET"})
		return
	}
	if s.brandEngine == nil || s.brandEngine.HistoryDB() == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "审计历史库未启用"})
		return
	}
	prefix := "/api/v1/leaderboard/brand/"
	brandName := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
	brandName, _ = url.PathUnescape(brandName)
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "缺少品牌名参数（路径：/api/v1/leaderboard/brand/:brand）"})
		return
	}
	historyLimit := 50
	if l := r.URL.Query().Get("history_limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			historyLimit = n
		}
	}
	if historyLimit > 500 {
		historyLimit = 500
	}
	// 1. 获取该品牌最新记录（用于 category / 当前排名计算）
	latestRec, err := s.brandEngine.HistoryDB().Latest(r.Context(), brandName)
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	if latestRec == nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "未找到该品牌的审计记录"})
		return
	}
	category, industry := inferCategoryFromReportJSON(latestRec.ReportJSON)
	// 2. 获取所有品牌最新记录，找到该品牌的当前排名
	allItems, err := s.getAllBrandLatestRecords(r.Context())
	if err != nil {
		writeInternalError(w, err, "")
		return
	}
	currentRank := 0
	for _, it := range allItems {
		if strings.EqualFold(it.BrandName, brandName) {
			currentRank = it.Rank
			break
		}
	}
	// 3. 获取该品牌完整历史（时间序列，按时间降序）
	hist, err := s.brandEngine.HistoryDB().List(r.Context(), brandName, historyLimit, 0)
	if err != nil {
		writeInternalError(w, err, "读取排行榜历史")
		return
	}
	// 4. 构造 rank_history：基于同 category 内历史记录的相对排名
	rankHistory := make([]rankPoint, 0, len(hist))
	if len(hist) > 0 {
		// 为了合理估算历史排名，拿所有品牌的"全量历史"做各时间点快照太重；
		// 这里以该 category 内当前排行榜条目为基准做简单近似：
		// 对每条历史记录，估算它在当前排行榜中的位置（按 score 排序）。
		catItems := make([]leaderboardItem, 0, len(allItems))
		for _, it := range allItems {
			if strings.EqualFold(it.Category, category) {
				catItems = append(catItems, it)
			}
		}
		// 将该品牌的历史分数逐条插入当前分类排行榜，估算历史排名
		for _, h := range hist {
			estRank := 1
			for _, ci := range catItems {
				if !strings.EqualFold(ci.BrandName, brandName) && ci.Score > h.Score {
					estRank++
				}
			}
			rankHistory = append(rankHistory, rankPoint{
				Generated: h.Generated,
				Rank:      estRank,
				Score:     h.Score,
			})
		}
	}
	writeJSON(w, http.StatusOK, leaderboardBrandHistory{
		BrandName:   brandName,
		Category:    category,
		Industry:    industry,
		CurrentRank: currentRank,
		History:     hist,
		RankHistory: rankHistory,
	})
}

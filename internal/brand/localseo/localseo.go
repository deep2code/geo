// Package localseo 提供本地 SEO / GMB（Google Business Profile）审计能力。
//
// 检查一个品牌在本地搜索生态中的可见度与一致性，覆盖 4 个维度：
//   - NAP（名称-地址-电话）一致性：跨 Google / 高德 / 百度地图 / Yelp / 大众点评核验
//   - GMB（Google Business Profile）资料完整度：认领状态、照片、评价、营业时间等
//   - 本地引用（Citations）：在国内外主流商家目录的收录情况
//   - 综合评分与可执行建议（A-F 等级 + 中文推荐清单）
//
// 灵感来源于 Claude SEO 与 OpenSEO 的 Local SEO 模块。
//
// 注意：所有检查均为**模拟/启发式**实现。真实抓取 Google / 高德 / 百度等平台需要
// 对应 API Key 与商业授权，本模块不发起任何对外网络请求，仅依据传入的 NAP 信息
// 给出一致性、完整度与收录情况的估算，适合作为审计框架与建议生成器使用。
//
// 仅依赖标准库（context / fmt / strings / time），零第三方依赖。
package localseo

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ---------- 数据类型 ----------

// NAPInfo 商家的标准 NAP（Name-Address-Phone）信息，作为一致性核验的基准。
type NAPInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
	Website string `json:"website"`
}

// NAPEntry 单个目录上"找到的" NAP 记录及其与基准的匹配结果。
type NAPEntry struct {
	Source  string `json:"source"` // google/gaode/baidu/yelp/dianping
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
	Match   bool   `json:"match"`
}

// NAPConsistency NAP 跨目录一致性核验结果。
type NAPConsistency struct {
	Expected   NAPInfo    `json:"expected"`
	Found      []NAPEntry `json:"found"`
	Consistent bool       `json:"consistent"`
	Issues     []string   `json:"issues,omitempty"`
	Score      float64    `json:"score"` // 0-100
}

// GMBProfile Google Business Profile 资料完整度审计结果。
type GMBProfile struct {
	Claimed       bool     `json:"claimed"`
	Completeness  float64  `json:"completeness"` // 0-100
	Photos        int      `json:"photos"`
	Reviews       int      `json:"reviews"`
	AvgRating     float64  `json:"avg_rating"`
	Posts         int      `json:"posts"` // 近期发帖数
	QA            int      `json:"qa"`    // 已回答的 Q&A 数
	BusinessHours bool     `json:"business_hours"`
	Description   bool     `json:"description"`
	Categories    []string `json:"categories"`
	MissingFields []string `json:"missing_fields,omitempty"`
	Score         float64  `json:"score"` // 0-100
}

// LocalCitation 单个商家目录的收录与 NAP 匹配情况。
type LocalCitation struct {
	Directory string  `json:"directory"` // google/gaode/baidu/yelp/dianping/meituan
	Listed    bool    `json:"listed"`
	URL       string  `json:"url,omitempty"`
	NAPMatch  bool    `json:"nap_match"`
	Score     float64 `json:"score"`
}

// LocalSEOReport 一次本地 SEO 审计的完整报告。
type LocalSEOReport struct {
	BrandName       string          `json:"brand_name"`
	NAPConsistency  NAPConsistency  `json:"nap_consistency"`
	GMBProfile      GMBProfile      `json:"gmb_profile"`
	Citations       []LocalCitation `json:"citations"`
	OverallScore    float64         `json:"overall_score"` // 0-100
	Grade           string          `json:"grade"`         // A-F
	Recommendations []string        `json:"recommendations"`
	AuditedAt       time.Time       `json:"audited_at"`
}

// ---------- 常量与目录清单 ----------

// napDirectories NAP 一致性核验覆盖的目录标识。
var napDirectories = []string{"google", "gaode", "baidu", "yelp", "dianping"}

// citationDirectory 本地引用收录检查的目录及其入口 URL。
var citationDirectories = []struct {
	name string
	url  string
}{
	{"google", "https://www.google.com/maps"},
	{"gaode", "https://www.amap.com"},
	{"baidu", "https://map.baidu.com"},
	{"yelp", "https://www.yelp.com"},
	{"dianping", "https://www.dianping.com"},
	{"meituan", "https://www.meituan.com"},
}

// 综合评分权重（合计 1.0）：NAP 30% + GMB 40% + Citations 30%。
const (
	weightNAP       = 0.30
	weightGMB       = 0.40
	weightCitations = 0.30
)

// 模拟一致性置信度上限：实际抓取需 API Key，留 5% 不确定余量。
const napConfidenceCeiling = 95.0

// gmbCoreFieldCount GMB 完整度核心字段总数（用于计算 Completeness 百分比）。
const gmbCoreFieldCount = 5

// ---------- 核心入口 ----------

// Audit 对指定品牌执行本地 SEO / GMB 综合审计。
//
// 流程：
//  1. 基于 expectedNAP 核验 NAP 跨目录一致性（模拟）
//  2. 检查 Google Business Profile 资料完整度（模拟）
//  3. 检查国内外主流商家目录的收录情况（模拟）
//  4. 综合评分：NAP 30% + GMB 40% + Citations 30%，映射 A-F 等级
//  5. 依据各维度短板生成中文可执行建议
func Audit(ctx context.Context, brandName string, expectedNAP NAPInfo) (*LocalSEOReport, error) {
	if strings.TrimSpace(brandName) == "" {
		return nil, fmt.Errorf("localseo: brand_name 不能为空")
	}

	report := &LocalSEOReport{
		BrandName: brandName,
		AuditedAt: time.Now(),
	}

	// 1. NAP 一致性（30%）
	report.NAPConsistency = checkNAPConsistency(ctx, brandName, expectedNAP)
	// 2. GMB 资料完整度（40%）
	report.GMBProfile = checkGMBProfile(ctx, brandName)
	// 3. 本地引用（30%）
	report.Citations = checkCitations(ctx, brandName, expectedNAP)

	// 综合评分
	citationsScore := aggregateCitationsScore(report.Citations)
	report.OverallScore = report.NAPConsistency.Score*weightNAP +
		report.GMBProfile.Score*weightGMB +
		citationsScore*weightCitations
	report.Grade = scoreToGrade(report.OverallScore)

	// 生成建议
	report.Recommendations = generateRecommendations(report)

	return report, nil
}

// ---------- NAP 一致性 ----------

// checkNAPConsistency 跨目录核验 NAP 一致性（模拟）。
//
// 模拟逻辑：
//   - NAP 完整（name/address/phone 三项均填写）→ 假定各目录信息一致，match=true
//   - NAP 不完整 → 无法核验一致性，match=false，并按缺失字段生成 issues
//   - 评分 = 完整度（已填字段占比）× 模拟置信度上限（95%）
//
// 真实场景需调用各平台 API 抓取实际商家信息后逐字段比对。
func checkNAPConsistency(ctx context.Context, brandName string, expected NAPInfo) NAPConsistency {
	result := NAPConsistency{
		Expected: expected,
		Found:    make([]NAPEntry, 0, len(napDirectories)),
	}
	if err := ctx.Err(); err != nil {
		result.Issues = []string{"审计已取消: " + err.Error()}
		return result
	}

	// 核心字段完整度评估
	nameFilled := strings.TrimSpace(expected.Name) != ""
	addrFilled := strings.TrimSpace(expected.Address) != ""
	phoneFilled := strings.TrimSpace(expected.Phone) != ""
	filledCount := 0
	if nameFilled {
		filledCount++
	}
	if addrFilled {
		filledCount++
	}
	if phoneFilled {
		filledCount++
	}

	// 收集问题
	var issues []string
	if strings.TrimSpace(brandName) == "" {
		issues = append(issues, "缺少品牌名称，无法定位商家")
	}
	if !nameFilled {
		issues = append(issues, "NAP 缺少名称（Name）字段")
	}
	if !addrFilled {
		issues = append(issues, "NAP 缺少地址（Address）字段")
	}
	if !phoneFilled {
		issues = append(issues, "NAP 缺少电话（Phone）字段")
	}

	// 模拟各目录的 NAP 核验：将基准 NAP 投射到每个目录
	napComplete := filledCount == 3
	for _, dir := range napDirectories {
		entry := NAPEntry{
			Source:  dir,
			Name:    expected.Name,
			Address: expected.Address,
			Phone:   expected.Phone,
			// NAP 完整方可核验一致性；否则视为无法匹配
			Match: napComplete,
		}
		result.Found = append(result.Found, entry)
	}

	// 一致性判定：全部目录匹配且 NAP 完整
	matchCount := 0
	for _, e := range result.Found {
		if e.Match {
			matchCount++
		}
	}
	result.Consistent = napComplete && matchCount == len(result.Found)
	result.Issues = issues

	// 评分：完整度 × 模拟置信度上限
	completeness := float64(filledCount) / 3.0
	result.Score = completeness * napConfidenceCeiling
	return result
}

// ---------- GMB 资料完整度 ----------

// checkGMBProfile 检查 Google Business Profile 资料完整度（模拟）。
//
// 由于无 GBP API 访问能力，模拟一个"已认领但未充分优化"的典型资料状态，
// 据此计算完整度与综合评分，并暴露常见缺失字段以驱动建议生成。
// 真实场景需接入 Google Business Profile API 获取实际资料。
func checkGMBProfile(ctx context.Context, brandName string) GMBProfile {
	profile := GMBProfile{}
	if err := ctx.Err(); err != nil {
		profile.MissingFields = []string{"审计已取消: " + err.Error()}
		return profile
	}
	if strings.TrimSpace(brandName) == "" {
		profile.MissingFields = []string{"品牌名称缺失，无法定位 Google Business Profile"}
		profile.Score = 0
		return profile
	}

	// 模拟：假定品牌已认领 GBP，但资料处于"未优化"的典型状态
	profile.Claimed = true
	profile.Description = false     // 商家描述常见缺失
	profile.BusinessHours = true    // 营业时间通常已填
	profile.Categories = []string{} // 模拟未设置业务类别
	profile.Photos = 3              // 照片数量不足
	profile.Reviews = 5             // 评价数量不足
	profile.AvgRating = 4.2         // 模拟平均评分
	profile.Posts = 0               // 近期无发帖
	profile.QA = 0                  // 无 Q&A 互动

	// 收集缺失字段
	var missing []string
	if !profile.Claimed {
		missing = append(missing, "claimed")
	}
	if !profile.Description {
		missing = append(missing, "description")
	}
	if !profile.BusinessHours {
		missing = append(missing, "business_hours")
	}
	if len(profile.Categories) == 0 {
		missing = append(missing, "categories")
	}
	if profile.Photos == 0 {
		missing = append(missing, "photos")
	}
	if profile.Posts == 0 {
		missing = append(missing, "posts")
	}
	if profile.QA == 0 {
		missing = append(missing, "qa")
	}
	profile.MissingFields = missing

	// 完整度：核心字段填充比例（认领/描述/营业时间/类别/照片）
	coreFilled := 0
	if profile.Claimed {
		coreFilled++
	}
	if profile.Description {
		coreFilled++
	}
	if profile.BusinessHours {
		coreFilled++
	}
	if len(profile.Categories) > 0 {
		coreFilled++
	}
	if profile.Photos > 0 {
		coreFilled++
	}
	profile.Completeness = float64(coreFilled) / float64(gmbCoreFieldCount) * 100

	// 综合评分
	if !profile.Claimed {
		// 未认领属严重问题，评分上限压低
		profile.Score = profile.Completeness * 0.2
	} else {
		photoScore := min(float64(profile.Photos)/10.0, 1.0) * 100
		reviewScore := min(float64(profile.Reviews)/10.0, 1.0) * 100
		ratingScore := 0.0
		if profile.Reviews > 0 {
			ratingScore = profile.AvgRating / 5.0 * 100
		}
		postScore := 0.0
		if profile.Posts > 0 {
			postScore = 100
		}
		qaScore := 0.0
		if profile.QA > 0 {
			qaScore = 100
		}
		// 完整度 35% + 照片 15% + 评价 15% + 评分 15% + 发帖 10% + Q&A 10%
		profile.Score = profile.Completeness*0.35 +
			photoScore*0.15 +
			reviewScore*0.15 +
			ratingScore*0.15 +
			postScore*0.10 +
			qaScore*0.10
	}
	return profile
}

// ---------- 本地引用 ----------

// checkCitations 检查国内外主流商家目录的收录情况（模拟）。
//
// 模拟逻辑：
//   - NAP 完整 → 假定全部目录已收录，NAP 匹配
//   - NAP 部分完整（2/3）→ 按品牌属性判定：中文品牌更可能被中文目录收录，
//     国际品牌更可能被国际目录收录
//   - NAP 严重不完整（≤1/3）→ 假定多数目录未收录
//   - 单条引用评分：已收录且 NAP 匹配=100，已收录但不匹配=60，未收录=0
func checkCitations(ctx context.Context, brandName string, nap NAPInfo) []LocalCitation {
	citations := make([]LocalCitation, 0, len(citationDirectories))
	if err := ctx.Err(); err != nil {
		return citations
	}

	// NAP 完整度
	nameFilled := strings.TrimSpace(nap.Name) != ""
	addrFilled := strings.TrimSpace(nap.Address) != ""
	phoneFilled := strings.TrimSpace(nap.Phone) != ""
	filledCount := 0
	if nameFilled {
		filledCount++
	}
	if addrFilled {
		filledCount++
	}
	if phoneFilled {
		filledCount++
	}
	napComplete := filledCount == 3
	isCJK := containsCJK(brandName)

	for _, d := range citationDirectories {
		c := LocalCitation{
			Directory: d.name,
			URL:       d.url,
		}

		// 模拟收录判定
		listed := false
		switch {
		case napComplete:
			// NAP 完整 → 假定全收录
			listed = true
		case filledCount == 2:
			// 部分完整：按品牌属性匹配目录属性
			chineseDir := isChineseDirectory(d.name)
			listed = (isCJK && chineseDir) || (!isCJK && !chineseDir)
		default:
			// 严重不完整 → 假定未收录
			listed = false
		}
		c.Listed = listed
		c.NAPMatch = napComplete && listed

		// 单条评分
		switch {
		case c.Listed && c.NAPMatch:
			c.Score = 100
		case c.Listed:
			c.Score = 60
		default:
			c.Score = 0
		}
		citations = append(citations, c)
	}
	return citations
}

// ---------- 建议生成 ----------

// generateRecommendations 依据审计报告各维度短板生成中文可执行建议。
//
// 规则：
//   - GMB 未认领 → 认领并验证 GBP
//   - NAP 不一致 → 修复 NAP 一致性
//   - 照片 < 10 → 上传高质量照片
//   - 评价 < 10 → 鼓励客户评价
//   - 无营业时间 → 完善营业时间
//   - 缺业务类别 → 选择业务类别
//   - 中文品牌 → 在高德/百度认领商家
//   - 引用得分 < 50 → 在更多本地目录建立商家信息
func generateRecommendations(report *LocalSEOReport) []string {
	var recs []string

	if !report.GMBProfile.Claimed {
		recs = append(recs, "认领并验证 Google Business Profile（GBP）页面")
	}
	if !report.NAPConsistency.Consistent {
		recs = append(recs, "修复 NAP（名称-地址-电话）不一致问题，确保所有平台信息统一")
	}
	if report.GMBProfile.Photos < 10 {
		recs = append(recs, "上传至少 10 张高质量照片（外观、内部、产品、团队）")
	}
	if report.GMBProfile.Reviews < 10 {
		recs = append(recs, "鼓励满意客户留下评价，目标至少 10 条评价")
	}
	if !report.GMBProfile.BusinessHours {
		recs = append(recs, "完善营业时间信息，包括节假日特殊时间")
	}
	if len(report.GMBProfile.Categories) == 0 {
		recs = append(recs, "选择准确的主要和次要业务类别")
	}
	// 中文本地商家：强调国内地图平台认领
	if containsCJK(report.BrandName) {
		recs = append(recs, "在高德商家和百度地图上认领商家页面")
	}
	// 引用整体偏弱
	if aggregateCitationsScore(report.Citations) < 50 {
		recs = append(recs, "在更多本地目录网站建立商家信息（大众点评、美团等）")
	}

	return recs
}

// ---------- 工具函数 ----------

// scoreToGrade 将综合分数转为等级。
// 映射：>=80=A, >=60=B, >=40=C, >=20=D, <20=F。
func scoreToGrade(s float64) string {
	switch {
	case s >= 80:
		return "A"
	case s >= 60:
		return "B"
	case s >= 40:
		return "C"
	case s >= 20:
		return "D"
	default:
		return "F"
	}
}

// aggregateCitationsScore 计算本地引用列表的平均得分（0-100）。
func aggregateCitationsScore(citations []LocalCitation) float64 {
	if len(citations) == 0 {
		return 0
	}
	sum := 0.0
	for _, c := range citations {
		sum += c.Score
	}
	return sum / float64(len(citations))
}

// isChineseDirectory 判断目录标识是否为国内平台。
func isChineseDirectory(dir string) bool {
	switch dir {
	case "gaode", "baidu", "dianping", "meituan":
		return true
	}
	return false
}

// containsCJK 判断字符串是否包含 CJK 字符（用于识别中文品牌）。
// 覆盖 CJK 统一表意文字、扩展 A 区及 CJK 符号标点区。
func containsCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一表意文字
			(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
			(r >= 0x3000 && r <= 0x303F) { // CJK 符号和标点
			return true
		}
	}
	return false
}

package brand

import (
	"encoding/json"
	"strings"
)

// SchemaGenInput JSON-LD 生成器输入。
//
// profile 为品牌画像，verticalType 为业务垂直行业（来自 vertical.Detect，可为空），
// existingTypes 为已检测到的 schema @type 列表（来自 crawlability 审计，用于避免重复生成）。
type SchemaGenInput struct {
	Profile       BrandProfile
	VerticalType  string   // 业务垂直行业（saas/local_service/ecommerce/publisher/agency/unknown/空）
	ExistingTypes []string // 已有的 schema @type，命中则跳过该类型生成
}

// GenerateJSONLD 基于品牌画像生成品牌级 JSON-LD 结构化数据。
//
// 生成以下基础 schema（已存在则跳过）：
//   - Organization：组织信息（公司名、域名、行业、成立年份、地址）
//   - WebSite：网站信息（域名、名称）
//   - Brand：品牌信息（关联 parentOrganization → Organization）
//   - BreadcrumbList：面包屑导航基础结构
//
// 并根据 verticalType 生成行业特定 schema：
//   - saas → SoftwareApplication（含 applicationCategory/operatingSystem）
//   - local_service → LocalBusiness（含 address/telephone/openingHours）
//   - ecommerce → Product（含 brand/offer）
//   - publisher → WebSite + Article 基础模板
//   - agency → Service（含 serviceType/areaServed）
//
// 返回 JSON-LD 字符串（@graph 数组形式），可直接嵌入 <script type="application/ld+json">。
func GenerateJSONLD(input SchemaGenInput) string {
	if input.Profile.Name == "" {
		return ""
	}
	existing := map[string]bool{}
	for _, t := range input.ExistingTypes {
		existing[strings.TrimSpace(t)] = true
	}

	var graph []map[string]any

	// 1. Organization schema
	if !existing["Organization"] && input.Profile.Company != nil && input.Profile.Company.Name != "" {
		org := buildOrganizationSchema(input.Profile)
		graph = append(graph, org)
	}

	// 2. WebSite schema
	if !existing["WebSite"] && input.Profile.Domain != "" {
		site := map[string]any{
			"@context": "https://schema.org",
			"@type":    "WebSite",
			"name":     input.Profile.Name,
			"url":      normalizeURL(input.Profile.Domain),
		}
		if input.Profile.Company != nil && input.Profile.Company.Name != "" {
			site["publisher"] = map[string]any{
				"@type": "Organization",
				"name":  input.Profile.Company.Name,
			}
		}
		graph = append(graph, site)
	}

	// 3. Brand schema
	if !existing["Brand"] {
		brand := map[string]any{
			"@context": "https://schema.org",
			"@type":    "Brand",
			"name":     input.Profile.Name,
		}
		if input.Profile.Domain != "" {
			brand["url"] = normalizeURL(input.Profile.Domain)
		}
		if len(input.Profile.Aliases) > 0 {
			brand["alternateName"] = input.Profile.Aliases
		}
		if input.Profile.Company != nil && input.Profile.Company.Name != "" {
			brand["parentOrganization"] = map[string]any{
				"@type": "Organization",
				"name":  input.Profile.Company.Name,
			}
		}
		if input.Profile.Category != "" {
			brand["category"] = input.Profile.Category
		}
		graph = append(graph, brand)
	}

	// 4. 行业特定 schema
	if vs := buildVerticalSchema(input, existing); vs != nil {
		graph = append(graph, vs...)
	}

	// 5. BreadcrumbList 基础模板
	if !existing["BreadcrumbList"] && input.Profile.Domain != "" {
		breadcrumb := map[string]any{
			"@context": "https://schema.org",
			"@type":    "BreadcrumbList",
			"itemListElement": []map[string]any{
				{
					"@type":    "ListItem",
					"position": 1,
					"name":     "首页",
					"item":     normalizeURL(input.Profile.Domain),
				},
			},
		}
		graph = append(graph, breadcrumb)
	}

	if len(graph) == 0 {
		return ""
	}

	// 使用 @graph 数组形式合并多个 schema
	combined := map[string]any{
		"@context": "https://schema.org",
		"@graph":   graph,
	}
	b, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// buildOrganizationSchema 构建 Organization schema。
func buildOrganizationSchema(profile BrandProfile) map[string]any {
	co := profile.Company
	org := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Organization",
		"name":     co.Name,
	}
	if profile.Domain != "" {
		org["url"] = normalizeURL(profile.Domain)
	}
	if co.Domain != "" {
		org["url"] = normalizeURL(co.Domain)
	}
	if co.Description != "" {
		org["description"] = co.Description
	}
	if co.Industry != "" {
		org["knowsAbout"] = co.Industry
	}
	if co.FoundedYear > 0 {
		org["foundingDate"] = itoa(co.FoundedYear)
	}
	if co.Headquarters != "" {
		org["address"] = map[string]any{
			"@type": "PostalAddress",
			"name":  co.Headquarters,
		}
	}
	if len(co.Aliases) > 0 {
		org["alternateName"] = co.Aliases
	}
	// 工商核验字段（增强可信度）
	if co.CreditCode != "" {
		org["identifier"] = co.CreditCode
	}
	if co.LegalRepresentative != "" {
		org["founder"] = map[string]any{
			"@type": "Person",
			"name":  co.LegalRepresentative,
		}
	}
	return org
}

// buildVerticalSchema 根据业务垂直行业生成特定 schema。
func buildVerticalSchema(input SchemaGenInput, existing map[string]bool) []map[string]any {
	var result []map[string]any
	profile := input.Profile
	switch input.VerticalType {
	case "saas":
		if existing["SoftwareApplication"] {
			break
		}
		app := map[string]any{
			"@context":            "https://schema.org",
			"@type":               "SoftwareApplication",
			"name":                profile.Name,
			"applicationCategory": "BusinessApplication",
			"operatingSystem":     "Web",
		}
		if profile.Domain != "" {
			app["url"] = normalizeURL(profile.Domain)
		}
		if profile.Company != nil && profile.Company.Name != "" {
			app["publisher"] = map[string]any{
				"@type": "Organization",
				"name":  profile.Company.Name,
			}
		}
		result = append(result, app)
	case "local_service":
		if existing["LocalBusiness"] {
			break
		}
		lb := map[string]any{
			"@context": "https://schema.org",
			"@type":    "LocalBusiness",
			"name":     profile.Name,
		}
		if profile.Domain != "" {
			lb["url"] = normalizeURL(profile.Domain)
		}
		if profile.Company != nil {
			if profile.Company.Headquarters != "" {
				lb["address"] = map[string]any{
					"@type": "PostalAddress",
					"name":  profile.Company.Headquarters,
				}
			}
			if profile.Company.RegisteredAddress != "" {
				lb["address"] = map[string]any{
					"@type":         "PostalAddress",
					"streetAddress": profile.Company.RegisteredAddress,
					"addressRegion": profile.Company.Province,
				}
			}
		}
		result = append(result, lb)
	case "ecommerce":
		if existing["Product"] {
			break
		}
		for _, p := range profile.Products {
			prod := map[string]any{
				"@context": "https://schema.org",
				"@type":    "Product",
				"name":     p,
			}
			if profile.Domain != "" {
				prod["url"] = normalizeURL(profile.Domain)
			}
			prod["brand"] = map[string]any{
				"@type": "Brand",
				"name":  profile.Name,
			}
			result = append(result, prod)
		}
	case "publisher":
		// 出版行业：WebSite 已生成，这里补充 Article 基础模板提示
		if !existing["Article"] {
			result = append(result, map[string]any{
				"@context":  "https://schema.org",
				"@type":     "Article",
				"headline":  profile.Name + " - 内容模板",
				"author":    map[string]any{"@type": "Organization", "name": profile.Name},
				"publisher": map[string]any{"@type": "Organization", "name": profile.Name},
			})
		}
	case "agency":
		if existing["Service"] {
			break
		}
		svc := map[string]any{
			"@context":    "https://schema.org",
			"@type":       "Service",
			"name":        profile.Name,
			"serviceType": profile.Industry,
		}
		if profile.Domain != "" {
			svc["url"] = normalizeURL(profile.Domain)
		}
		if profile.Company != nil && profile.Company.Name != "" {
			svc["provider"] = map[string]any{
				"@type": "Organization",
				"name":  profile.Company.Name,
			}
		}
		result = append(result, svc)
	}
	return result
}

// normalizeURL 将域名规范化为完整 URL（补 https:// 前缀）。
func normalizeURL(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
		return domain
	}
	return "https://" + domain
}

// itoa 将 int 转为字符串（避免引入 strconv 仅为一个调用）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

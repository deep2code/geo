export interface HealthResponse {
  status: string
  service: string
  version: string
}

export interface ReadyCheck {
  status: 'ready' | 'not_ready'
  checks: Record<string, string>
  service: string
}

export interface StrategyInfo {
  type: string
  name: string
  effectiveness: number
  pwc_boost: number
}

export interface StrategiesResponse {
  strategies: StrategyInfo[]
  count: number
}

export interface ContentAnalysis {
  url?: string
  raw_text: string
  word_count: number
  citability_signals: Record<string, boolean>
  structure_signals: Record<string, boolean>
  negative_signals?: string[]
  evergreen_score: number
  analyzed_at: string
}

export interface ScoreBreakdown {
  category: string
  score: number
  max_score: number
  detail?: string
}

export interface ScoreResponse {
  score: number
  breakdowns: ScoreBreakdown[]
  grade: string
}

export interface VisibilityMetrics {
  citation_frequency: number
  citation_order: number
  position_score: number
  token_count: number
  semantic_similarity: number
  relative_citation_score: number
  overall_score: number
}

export interface UtilityMetrics {
  citation_quality: number
  keypoint_coverage: number
  response_quality: number
  overall_score: number
}

export interface StrategyResult {
  strategy: string
  applied: boolean
  improvement: number
  pwc_boost: number
  detail?: string
}

export interface Recommendation {
  category: string
  priority: string
  message: string
}

export interface GeneratedAssets {
  json_ld?: string
  llms_txt?: string
  llms_full_txt?: string
}

export interface OptimizationResponse {
  optimized_content: string
  score_before: number
  score_after: number
  geo_score: VisibilityMetrics
  utility_score: UtilityMetrics
  applied_strategies: StrategyResult[]
  recommendations?: Recommendation[]
  generated_assets?: GeneratedAssets
}

export interface Company {
  name: string
  aliases?: string[]
  domain?: string
  description?: string
  industry?: string
  headquarters?: string
  founded_year?: number
  credit_code?: string
  registered_capital?: string
  paid_in_capital?: string
  legal_representative?: string
  registration_status?: string
  established_date?: string
  company_type?: string
  registered_address?: string
  business_scope?: string
  province?: string
  staff_size?: string
}

export interface Competitor {
  name: string
  aliases?: string[]
  domain?: string
  company?: Company
}

export interface BrandProfile {
  name: string
  aliases?: string[]
  domain?: string
  products?: string[]
  company?: Company
  competitors?: Competitor[]
  prompts: string[]
  target_engines?: string[]
  industry?: string
  category?: string
  market?: string
  language?: string
}

export interface Citation {
  url: string
  title?: string
  snippet?: string
  position: number
}

export interface CompetitorMention {
  name: string
  position: number
  cited: boolean
}

export interface PromptResult {
  prompt: string
  engine: string
  answer: string
  citations?: Citation[]
  brand_mentioned: boolean
  brand_position: number
  brand_cited: boolean
  ghost_citation: boolean
  sentiment: 'positive' | 'neutral' | 'negative'
  competitor_mentions?: CompetitorMention[]
  duration?: number
  error?: string
  samples?: number
  consistency?: number
  correction?: ResultCorrection
}

// 人工修正留痕（后端 brand.ResultCorrection 的镜像）。
export interface ResultCorrection {
  corrected_by: string
  corrected_at: string
  reason: string
  prev_mentioned: boolean
  prev_cited: boolean
  prev_sentiment: string
  prev_position: number
  mentioned: boolean
  cited: boolean
  sentiment: string
  position: number
}

// 报告级人工修正元信息。
export interface CorrectionInfo {
  corrected_count: number
  last_corrected_by?: string
  last_corrected_at?: string
  last_reason?: string
}

export interface EngineStats {
  engine: string
  total_prompts: number
  mention_count: number
  citation_count: number
  ghost_citation_count: number
  mention_rate: number
  citation_rate: number
  avg_position: number
  sentiment_positive: number
  sentiment_neutral: number
  sentiment_negative: number
  positive_rate: number
  share_of_voice: number
  configured: boolean
}

export interface ContentGap {
  prompt: string
  engine: string
  competitor_named: string[]
  suggested_topic: string
}

export interface CompetitorSOV {
  name: string
  mention_count: number
  sov: number
}

export interface NegativeMention {
  prompt: string
  engine: string
  snippet: string
}

export interface ActionItem {
  priority: 'high' | 'medium' | 'low'
  category: 'content' | 'engine' | 'reputation' | 'entity'
  title: string
  detail: string
  tasks?: string[]
  expected_impact?: string
}

export interface ScoreBreakdownReport {
  mention_rate: number
  citation_rate: number
  share_of_voice: number
  citation_position: number
  sentiment: number
  entity_recognition: number
  content_quality: number
  technical_seo: number
  on_page_seo: number
  schema: number
  performance: number
  ai_readiness: number
  image_optimization: number
}

export interface VisibilityReport {
  brand_name: string
  industry?: string
  category?: string
  company?: Company
  entity_completeness_score?: number
  generated_at: string
  score: number
  grade: string
  tier: 'household' | 'midmarket' | 'niche'
  score_breakdown: ScoreBreakdownReport
  engine_stats: EngineStats[]
  content_gaps?: ContentGap[]
  competitor_sov?: CompetitorSOV[]
  negative_mentions?: NegativeMention[]
  actions: ActionItem[]
  results?: PromptResult[]
  // 人工修正：报告级元信息 + 历史记录 ID（定位修正目标）。
  corrected?: CorrectionInfo
  record_id?: number
}

export interface AutocompleteCandidate {
  brand_name: string
  brand_aliases?: string[]
  brand_domain?: string
  industry?: string
  category?: string
  products?: string[]
  prompts?: string[]
  competitors?: Competitor[]
  company?: Company
}

export interface KnowledgeSearchItem {
  brand_name: string
  brand_domain?: string
  brand_aliases?: string[]
  industry?: string
  category?: string
  products?: string[]
  company_name?: string
  company_domain?: string
  hq?: string
  founded_year?: number
  description?: string
  source: 'sinofacts' | 'offlinedb'
  source_label: string
  score: number
  credit_code?: string
  legal_person?: string
  registered_date?: string
  capital?: string
  province?: string
  city?: string
  address?: string
  company_type?: string
  business_scope?: string
}

export interface KnowledgeSearchResponse {
  total: number
  query: string
  result: KnowledgeSearchItem[]
  sinofacts_count: number
  offlinedb_count: number
  license: string
}

export interface MarketInfo {
  code: string
  name: string
  label: string
}

export interface MarketsResponse {
  markets: MarketInfo[]
  count: number
}

export interface MailStatus {
  enabled: boolean
  host: string
  port: number
  from: string
}

export interface MailSendRequest {
  to: string[]
  cc?: string[]
  subject?: string
  text?: string
  html?: string
  template?: 'alert' | 'weekly'
  template_data?: Record<string, any>
}

export interface MailSendResponse {
  ok: boolean
  to: string[]
  subject: string
}

export interface ReportEmailRequest {
  brand: string
  to: string[]
  cc?: string[]
  format: 'pdf' | 'html' | 'both'
}

export interface ReportEmailResponse {
  ok: boolean
  to: string[]
  format: string
  subject: string
}

export interface SchedulerStatus {
  enabled: boolean
  config_count: number
  webhook_configured: boolean
}

export interface HistoryRecord {
  id: string
  brand_name: string
  generated_at: string
  score: number
  grade: string
  tier: string
  market?: string
  report_json?: string
}

export interface HistoryStats {
  total_records: number
  unique_brands: number
  date_range: { from: string; to: string } | null
}

export interface HistoryDailyBucket {
  date: string // YYYY-MM-DD，本地时区
  count: number // 当天审计记录条数
  avg_score: number // 当日平均分；-1 表示当天无记录
}

export interface HistoryDailyResponse {
  days: number
  records: HistoryDailyBucket[]
  summary: {
    total_records: number
    score_days: number
    avg_score_daily: number | null
    daily_avg_records: number
  }
}

export interface DiscoverKeyword {
  keyword: string
  search_volume: number
  difficulty: number
  relevance: number
  intent: 'informational' | 'navigational' | 'transactional' | 'commercial'
  cpc: number
  trend: number[]
  priority: 'high' | 'medium' | 'low'
  cluster?: string
}

export interface DiscoverResponse {
  seed_keywords: string[]
  keywords: DiscoverKeyword[]
  clusters: { name: string; keywords: string[]; geo_potential: number }[]
  market: string
  language: string
}

export interface HistoryListResponse {
  records: HistoryRecord[]
  total: number
  brand?: string
  limit?: number
  offset?: number
}

export interface DriftReport {
  brand_name: string
  from_time: string
  to_time: string
  score_delta: number
  grade_from: string
  grade_to: string
  dimension_deltas: Record<string, number>
  engine_deltas: Record<string, { mention_rate: number; citation_rate: number; sov: number }>
  top_gains: string[]
  top_losses: string[]
}

export interface ReadinessReport {
  brand_name: string
  overall_score: number
  overall_grade: string
  dimensions: {
    name: string
    score: number
    status: 'pass' | 'warn' | 'fail'
    detail: string
  }[]
  ci_gate_passed: boolean
  threshold: number
  action_items: ActionItem[]
}

export interface CrawlabilityReport {
  brand_name: string
  domain: string
  robots_txt: {
    present: boolean
    ai_allow_rules: boolean
    issues: string[]
  }
  llms_txt: {
    present: boolean
    issues: string[]
  }
  json_ld: {
    present: boolean
    types_found: string[]
    issues: string[]
  }
  knowledge_graph: {
    entity_present: boolean
    sources_found: string[]
  }
  overall_score: number
  recommendations: string[]
}

export interface LocalSEOReport {
  brand_name: string
  gmb_found: boolean
  name_consistency: number
  address_consistency: number
  phone_consistency: number
  avg_rating: number
  review_count: number
  local_pack_presence: number
  citations_count: number
  overall_score: number
  action_items: ActionItem[]
}

export interface SocialMonitorReport {
  brand_name: string
  window_days: number
  platforms: {
    platform: string
    mention_count: number
    positive: number
    neutral: number
    negative: number
    top_posts: { text: string; author: string; time: string; sentiment: string }[]
  }[]
  overall_sentiment: string
  alert_keywords: string[]
  trend_7d: number[]
}

export interface KOLAnalyzeReport {
  brand_name: string
  top_media: { domain: string; mention_count: number; authority_score: number }[]
  top_authors: { name: string; domain: string; mention_count: number }[]
  content_themes: { theme: string; count: number; keywords: string[] }[]
}

export interface TopSourceReport {
  brand_name: string
  top_sources: { domain: string; citation_count: number; share: number; authority: string }[]
  vs_competitors: { competitor: string; top_domain: string; count: number }[]
  recommendations: string[]
}

export interface VerticalDetectResult {
  brand_name: string
  primary: string
  confidence: number
  candidates: { vertical: string; confidence: number; keywords: string[] }[]
}

export interface ExternalSignalsReport {
  brand_name: string
  backlinks_count: number
  referring_domains: number
  authority_score: number
  organic_traffic_estimate: number
  top_pages: { url: string; traffic_estimate: number; keywords_count: number }[]
  brand_mentions_web: number
}

export interface WhitelabelMeta {
  brand_name: string
  logo_url?: string
  primary_color: string
  domain?: string
  favicon_url?: string
  support_email?: string
}

export interface CompareDimension {
  key: string
  label: string
  weight: number
  values: Record<string, number>
}

export interface CompareDiffRow {
  key: string
  label: string
  category: string
  values: Record<string, number | string>
  winner: string | null
  delta: Record<string, number>
}

export interface BrandCompareResponse {
  brands: string[]
  generated_at: string
  dimensions: CompareDimension[]
  radar_axes: { key: string; label: string; max: number }[]
  radar_values: { brand: string; values: Record<string, number> }[]
  diff_table: CompareDiffRow[]
  summary: {
    overall_winner: string | null
    strengths: Record<string, string[]>
    weaknesses: Record<string, string[]>
  }
}

export interface LeaderboardCategory {
  code: string
  name: string
  count: number
}

export interface LeaderboardRow {
  rank: number
  brand_name: string
  brand_domain?: string
  category: string
  score: number
  grade: string
  tier: string
  sov: number
  mention_rate: number
  citation_rate: number
  trend_7d: number[]
  updated_at: string
}

export interface LeaderboardResponse {
  category: string
  limit: number
  generated_at: string
  categories: LeaderboardCategory[]
  rows: LeaderboardRow[]
  total: number
}

// ── 系统自检（/api/v1/admin/selfcheck） ──
export type SelfCheckSeverity = 'ok' | 'info' | 'warn' | 'error'

export interface SelfCheckCheck {
  name: string
  category: 'business' | 'config' | 'infra'
  status: SelfCheckSeverity
  message: string
  detail?: string
  duration_ms?: number
}

export interface SelfCheckRuntime {
  go_version: string
  os: string
  arch: string
  num_cpu: number
  goroutines: number
  alloc_mb: number
}

export interface SelfCheckSummary {
  ok: number
  info: number
  warn: number
  error: number
}

export interface SelfCheckReport {
  generated_at: string
  overall: SelfCheckSeverity
  runtime: SelfCheckRuntime
  business: SelfCheckCheck[]
  config: SelfCheckCheck[]
  summary: SelfCheckSummary
}

// ── 规则集管理（/api/v1/rules*，替代原 `geo rules` CLI） ──
export interface RuleSetListItem {
  name: string
  version: string
  source: string
  valid: boolean
  error?: string
  engine?: string
  domain?: string
}

export interface RulesListResponse {
  rulesets: RuleSetListItem[]
}

export interface RuleSet {
  name?: string
  version?: string
  engine?: string
  domain?: string
  weights?: Record<string, number>
  strategy_effectiveness?: Record<string, number>
  strategy_triggers?: Record<string, unknown>
  [key: string]: unknown
}

export interface RuleSetValidateResponse {
  valid: boolean
  error?: string
  name?: string
  version?: string
  engine?: string
  domain?: string
  weights?: number
  strategy_effectiveness?: number
  strategy_triggers?: number
}

// ── GEO 评测（/api/v1/evaluate，替代原 `geo evaluate` CLI） ──
export interface EvaluateRequest {
  dataset: string
  format?: 'md' | 'json'
  live?: boolean
  llm_key?: string
  llm_base?: string
  llm_model?: string
  rules?: string
}

export interface EvaluateResponse {
  report: string
  format: 'md' | 'json'
  mode: string
}

// ── 离线工商库导入（/api/v1/brand/offlinedb/import*，替代原 `geo brand db import-*` CLI） ──
export interface OfflineDBStats {
  path?: string
  backend?: string
  count: number
  file_size_bytes?: number
  schema_created_at?: string
  provinces?: Record<string, number>
}

export interface OfflineDBImportResult {
  imported: number
  skipped: number
  failed: number
  files: number
  db_count: number
  db_file_size_bytes?: number
}

export interface OfflineDBImportGitHubRequest {
  years: string
  provinces: string
  base_url?: string
  timeout_seconds?: number
}

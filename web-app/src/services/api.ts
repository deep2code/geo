import type {
  HealthResponse,
  ReadyCheck,
  StrategiesResponse,
  ContentAnalysis,
  ScoreResponse,
  OptimizationResponse,
  VisibilityReport,
  BrandProfile,
  AutocompleteCandidate,
  KnowledgeSearchResponse,
  MarketsResponse,
  MailStatus,
  MailSendRequest,
  MailSendResponse,
  ReportEmailRequest,
  ReportEmailResponse,
  SchedulerStatus,
  HistoryListResponse,
  HistoryStats,
  HistoryDailyResponse,
  DriftReport,
  ReadinessReport,
  CrawlabilityReport,
  LocalSEOReport,
  SocialMonitorReport,
  KOLAnalyzeReport,
  TopSourceReport,
  VerticalDetectResult,
  SourceStat,
  TrendPoint,
  EngineSource,
  ExternalSignalsReport,
  DiscoverResponse,
  WhitelabelMeta,
  MetaSystem,
  BrandCompareResponse,
  LeaderboardResponse,
  SelfCheckReport,
  RulesListResponse,
  RuleSetValidateResponse,
  RuleSet,
  EvaluateResponse,
  OfflineDBStats,
  OfflineDBImportResult,
  OfflineDBImportGitHubRequest,
  ExternalSubmission,
  ExternalSubmissionsResponse,
  AuthLoginResponse,
  AIBotVisitsReport,
  DBExecResult,
  AdminAuditLogEntry
} from '@/types/api'

// API 基础前缀：优先显式注入 VITE_GEO_API_BASE，否则使用同源 /api/v1。
// 部署反向代理、子域拆分或本地 Vite 代理时，可通过 .env 设置：
//   VITE_GEO_API_BASE=https://geo.example.com/api/v1
export const API_BASE: string = (
  (import.meta.env?.VITE_GEO_API_BASE as string | undefined) ||
  '/api/v1'
).replace(/\/+$/, '')

// ---- 运行时 API 配置（Settings 页保存后即时生效，不再只是"死配置"）----
let runtimeBaseOverride: string | null = null
let runtimeTimeoutMs: number | null = null

/** 设置运行时 API 基址/超时（秒）；传 null/0 还原默认。 */
export function applyApiRuntimeConfig(baseUrl?: string | null, timeoutSec?: number | null): void {
  runtimeBaseOverride = baseUrl && baseUrl.trim() ? baseUrl.trim().replace(/\/+$/, '') : null
  runtimeTimeoutMs = timeoutSec && timeoutSec > 0 ? timeoutSec * 1000 : null
}

// 启动时从持久化 store 恢复（避免刷新后 Settings 里保存的配置失效）
try {
  const rawState = JSON.parse(localStorage.getItem('geo-app-storage') || '{}')?.state
  if (rawState?.apiBaseUrl) runtimeBaseOverride = String(rawState.apiBaseUrl).trim().replace(/\/+$/, '') || null
  if (rawState?.apiTimeout) runtimeTimeoutMs = Number(rawState.apiTimeout) > 0 ? Number(rawState.apiTimeout) * 1000 : null
} catch {
  /* ignore */
}

// 前端鉴权 Token 存储 Key（JWT access token，对应 Authorization: Bearer）。
const AUTH_TOKEN_KEY = 'geo_api_token'

/** 401/403 全局拦截回调。UI 层（App/AppRoutes）注册统一跳转逻辑。 */
type AuthErrorHandler = (info: { status: 401 | 403; path: string }) => void
const authErrorHandlers = new Set<AuthErrorHandler>()
export const onAuthError = (fn: AuthErrorHandler): (() => void) => {
  authErrorHandlers.add(fn)
  return () => {
    authErrorHandlers.delete(fn)
  }
}
function emitAuthError(status: 401 | 403, path: string): void {
  for (const fn of Array.from(authErrorHandlers)) {
    try {
      fn({ status, path })
    } catch {
      /* noop */
    }
  }
}

/**
 * 统一设置/清除 API Bearer Token（登录/登出时调用）。
 * P0-2：存于 sessionStorage——敏感凭据不落 localStorage，
 * 关闭标签页即清除，降低 XSS 长期窃取与凭据残留风险。
 * （刷新页面仍有效，因为刷新不结束会话。）
 */
const TOKEN_EXP_KEY = 'geo_api_token_exp'

/** 解码 JWT payload 的 exp 声明（不验证签名，仅客户端过期预检）。 */
function parseJwtExp(token: string): number | null {
  try {
    const part = token.split('.')[1]
    if (!part) return null
    // 后端用 base64url（RawURLEncoding，无 padding）：atob 前需转标准 base64 并补齐
    const b64 = part.replace(/-/g, '+').replace(/_/g, '/')
    const payload = JSON.parse(atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4)))
    return typeof payload.exp === 'number' ? payload.exp : null
  } catch {
    return null
  }
}

export const setApiAuthToken = (token: string | null): void => {
  if (!token) {
    sessionStorage.removeItem(AUTH_TOKEN_KEY)
    sessionStorage.removeItem(TOKEN_EXP_KEY)
    return
  }
  sessionStorage.setItem(AUTH_TOKEN_KEY, token)
  // 缓存过期时间戳，避免每次请求都解码 JWT
  const exp = parseJwtExp(token)
  if (exp) sessionStorage.setItem(TOKEN_EXP_KEY, String(exp))
}

/** 当前持有的 API Bearer Token（未设置返回空串）；已过期则自动清除并返回空串。 */
export const getApiAuthToken = (): string => {
  // 先检查缓存的有效期
  const cachedExp = sessionStorage.getItem(TOKEN_EXP_KEY)
  if (cachedExp) {
    const now = Math.floor(Date.now() / 1000)
    if (now >= Number(cachedExp)) {
      // Token 已过期：主动清除，减少无效 401 请求
      sessionStorage.removeItem(AUTH_TOKEN_KEY)
      sessionStorage.removeItem(TOKEN_EXP_KEY)
      return ''
    }
  }
  let tok = sessionStorage.getItem(AUTH_TOKEN_KEY)
  if (!tok) {
    // 平滑迁移：旧版凭据残留于 localStorage，读到后搬到 sessionStorage 并清除旧值
    const legacy = localStorage.getItem(AUTH_TOKEN_KEY)
    if (legacy) {
      sessionStorage.setItem(AUTH_TOKEN_KEY, legacy)
      localStorage.removeItem(AUTH_TOKEN_KEY)
      tok = legacy
      // 迁移时也解码一次有效期
      const exp = parseJwtExp(legacy)
      if (exp) sessionStorage.setItem(TOKEN_EXP_KEY, String(exp))
    }
  }
  return tok ?? ''
}

export interface RequestOptions extends RequestInit {
  timeout?: number
  /** 跳过自动注入 Authorization（如公开登录/注册）。 */
  skipAuth?: boolean
  /** 跳过 401/403 自动跳转（给登录校验接口用）。 */
  skipAuthRedirect?: boolean
}

// cleanParams 去掉查询参数中的空值（undefined/''/null），避免 URL 出现无意义参数。
function cleanParams(p: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(p)) {
    if (v === undefined || v === null || v === '') continue
    out[k] = String(v)
  }
  return out
}

async function request<T>(
  path: string,
  options: RequestOptions = {}
): Promise<T> {
  const { timeout: timeoutOpt, headers, skipAuth, skipAuthRedirect, ...rest } = options
  // 优先级：调用方显式指定 > Settings 页运行时配置 > 默认
  const timeout = timeoutOpt ?? runtimeTimeoutMs ?? 120000

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)

  // 合并默认头 + 注入 Bearer 鉴权。
  const mergedHeaders: Record<string, string> = Object.create(null)
  mergedHeaders['Content-Type'] = 'application/json'
  if (headers) {
    for (const [k, v] of Object.entries(headers as Record<string, string>)) {
      if (v != null) mergedHeaders[k] = String(v)
    }
  }
  if (!skipAuth) {
    const tok = getApiAuthToken()
    if (tok) mergedHeaders['Authorization'] = `Bearer ${tok}`
  }

  try {
    const res = await fetch(`${runtimeBaseOverride ?? API_BASE}${path}`, {
      ...rest,
      headers: mergedHeaders,
      signal: controller.signal
    })

    // 滑动续期：后端在 token 剩余有效期不足一半时，通过 X-GEO-New-Token 下发新 access token。
    // 前端检测到后自动替换存储（并刷新缓存的过期时间），实现「有效操作一次就自动延长」。
    const newToken = res.headers.get('X-GEO-New-Token')
    if (newToken && newToken.length > 20) {
      setApiAuthToken(newToken)
    }

    // 401/403 拦截：除非显式跳过，否则触发全局跳转（但仍抛错让业务 catch）。
    if ((res.status === 401 || res.status === 403) && !skipAuthRedirect) {
      emitAuthError(res.status, path)
    }

    const contentType = res.headers.get('content-type') || ''
    const isJson = contentType.includes('application/json')

    if (!res.ok) {
      if (isJson) {
        const errJson = await res.json().catch(() => ({}))
        const msg = (errJson as any).error || `HTTP ${res.status}`
        const err = new Error(msg) as Error & { code?: string; status?: number }
        err.code = (errJson as any).code
        err.status = res.status
        throw err
      }
      const text = await res.text().catch(() => '')
      const err = new Error(text || `HTTP ${res.status}`) as Error & { status?: number }
      err.status = res.status
      throw err
    }

    if (res.status === 204) return undefined as unknown as T
    if (!isJson) return (await res.text()) as unknown as T
    return (await res.json()) as T
  } finally {
    clearTimeout(timer)
  }
}

export const api = {
  health: () => request<HealthResponse>('/health', { method: 'GET' }),
  ready: () => request<ReadyCheck>('/ready', { method: 'GET' }),
  strategies: () => request<StrategiesResponse>('/strategies', { method: 'GET' }),
  analyze: (content: string) =>
    request<ContentAnalysis>('/analyze', {
      method: 'POST',
      body: JSON.stringify({ content })
    }),
  score: (content: string) =>
    request<ScoreResponse>('/score', {
      method: 'POST',
      body: JSON.stringify({ content })
    }),
  optimize: (req: {
    content: string
    url?: string
    title?: string
    target_engines?: string[]
    domain_type?: string
    strategies?: string[]
    language?: string
  }) =>
    request<OptimizationResponse>('/optimize', {
      method: 'POST',
      body: JSON.stringify(req)
    }),

  brandAudit: (profile: BrandProfile) =>
    request<VisibilityReport>('/brand/audit', {
      method: 'POST',
      body: JSON.stringify(profile)
    }),
  // 人工修正单条判定并重算报告（管理后台权限，审计留痕）。
  auditCorrection: (body: {
    record_id: number
    brand_name: string
    index: number
    mentioned?: boolean
    cited?: boolean
    sentiment?: 'positive' | 'neutral' | 'negative'
    position?: number
    reason: string
  }) =>
    request<VisibilityReport>('/admin/audit/correction', {
      method: 'POST',
      body: JSON.stringify(body)
    }),
  // 引擎来源偏好研究（大模型引用来源：排行/趋势/对比）。
  engineSourcesTop: (p: { engine?: string; brand?: string; days?: number; limit?: number } = {}) =>
    request<{ sources: SourceStat[] }>(
      `/admin/engine-sources/top?${new URLSearchParams(cleanParams(p) as any).toString()}`,
      { method: 'GET', timeout: 30000 }
    ),
  engineSourcesTrend: (p: { engine?: string; brand?: string; domain?: string; days?: number } = {}) =>
    request<{ trend: TrendPoint[] }>(
      `/admin/engine-sources/trend?${new URLSearchParams(cleanParams(p) as any).toString()}`,
      { method: 'GET', timeout: 30000 }
    ),
  engineSourcesCompare: (p: { brand?: string; days?: number; limit?: number } = {}) =>
    request<{ engines: EngineSource[] }>(
      `/admin/engine-sources/compare?${new URLSearchParams(cleanParams(p) as any).toString()}`,
      { method: 'GET', timeout: 30000 }
    ),
  brandAutocomplete: (brand_name: string) =>
    request<AutocompleteCandidate>('/brand/autocomplete', {
      method: 'POST',
      body: JSON.stringify({ brand_name })
    }),
  brandKnowledgeSearch: (q: string, limit = 5) =>
    request<KnowledgeSearchResponse>(
      `/brand/knowledge/search?q=${encodeURIComponent(q)}&limit=${limit}`,
      { method: 'GET' }
    ),
  brandMarkets: () => request<MarketsResponse>('/brand/markets', { method: 'GET' }),

  reportHtml: (brand: string) =>
    `${API_BASE}/brand/report/html?brand=${encodeURIComponent(brand)}`,
  reportDownload: (brand: string) =>
    `${API_BASE}/brand/report/download?brand=${encodeURIComponent(brand)}`,
  reportPdf: (brand: string) =>
    `${API_BASE}/brand/report/pdf?brand=${encodeURIComponent(brand)}`,
  reportEmail: (req: ReportEmailRequest) =>
    request<ReportEmailResponse>('/brand/report/email', {
      method: 'POST',
      body: JSON.stringify(req)
    }),

  mailStatus: () => request<MailStatus>('/mail/status', { method: 'GET', skipAuthRedirect: true }),
  mailSend: (req: MailSendRequest) =>
    request<MailSendResponse>('/mail/send', {
      method: 'POST',
      body: JSON.stringify(req)
    }),

  schedulerStatus: () =>
    request<SchedulerStatus>('/brand/scheduler/status', { method: 'GET' }),
  schedulerTrigger: (brand: string) =>
    request<{ ok: boolean }>('/brand/scheduler/trigger', {
      method: 'POST',
      body: JSON.stringify({ brand })
    }),

  historyList: (brand?: string, limit = 50, offset = 0) => {
    const params = new URLSearchParams()
    if (brand) params.set('brand', brand)
    params.set('limit', String(limit))
    params.set('offset', String(offset))
    return request<HistoryListResponse>(
      `/brand/history/list?${params.toString()}`,
      { method: 'GET' }
    )
  },
  historyStats: () =>
    request<HistoryStats>('/brand/history/stats', { method: 'GET' }),
  // Dashboard 30 天趋势：按天聚合的审计次数/每日平均分。
  historyStatsDaily: (days = 30) =>
    request<HistoryDailyResponse>(
      `/brand/history/stats/daily?days=${encodeURIComponent(String(days))}`,
      { method: 'GET' }
    ),
  historyBrands: () =>
    request<{ brands: string[] }>('/brand/history/brands', { method: 'GET' }),

  // ===== 账号体系（JWT）=====
  // 登录：成功后返回 tokens/user/workspaces，调用方应保存 access_token。
  auth: {
    login: (payload: { email: string; password: string; workspace_id?: string }) =>
      request<AuthLoginResponse>('/auth/login', {
        method: 'POST',
        skipAuth: true,
        skipAuthRedirect: true,
        body: JSON.stringify(payload)
      }),
    register: (payload: {
      email: string
      password: string
      display_name?: string
      workspace_name?: string
    }) =>
      request<AuthLoginResponse>('/auth/register', {
        method: 'POST',
        skipAuth: true,
        skipAuthRedirect: true,
        body: JSON.stringify(payload)
      }),
    logout: (refresh_token: string) =>
      request<{ ok: boolean }>('/auth/logout', {
        method: 'POST',
        body: JSON.stringify({ refresh_token })
      })
  },

  driftAudit: (brand: string, from_time?: string, to_time?: string) =>
    request<DriftReport>('/brand/drift', {
      method: 'POST',
      body: JSON.stringify({ brand, from_time, to_time })
    }),
  readinessAudit: (profile: BrandProfile) =>
    request<ReadinessReport>('/brand/readiness', {
      method: 'POST',
      body: JSON.stringify(profile)
    }),
  crawlabilityAudit: (domain: string) =>
    request<CrawlabilityReport>('/brand/crawlability', {
      method: 'POST',
      body: JSON.stringify({ domain })
    }),
  localSeoAudit: (brand: string) =>
    request<LocalSEOReport>('/brand/localseo/audit', {
      method: 'POST',
      body: JSON.stringify({ brand })
    }),
  socialMonitor: (brand: string, window_days = 7) =>
    request<SocialMonitorReport>('/brand/social/monitor', {
      method: 'POST',
      body: JSON.stringify({ brand, window_days })
    }),
  kolAnalyze: (report: { engine_stats: any[]; results?: any[] }) =>
    request<KOLAnalyzeReport>('/brand/kol/analyze', {
      method: 'POST',
      body: JSON.stringify(report)
    }),
  topSourceAnalyze: (report: { engine_stats: any[]; results?: any[] }) =>
    request<TopSourceReport>('/brand/topsource/analyze', {
      method: 'POST',
      body: JSON.stringify(report)
    }),
  verticalDetect: (description: string) =>
    request<VerticalDetectResult>('/brand/vertical/detect', {
      method: 'POST',
      body: JSON.stringify({ description })
    }),
  externalSignals: (domain: string) =>
    request<ExternalSignalsReport>('/brand/externalsignals/report', {
      method: 'POST',
      body: JSON.stringify({ domain })
    }),
  discover: (seed_keywords: string[], market = 'global', language = 'en') =>
    request<DiscoverResponse>('/brand/discover', {
      method: 'POST',
      body: JSON.stringify({ seed_keywords, market, language })
    }),
  discoverReport: (keywords: string[]) =>
    request<{ ok: boolean; html_url?: string; csv_url?: string }>(
      '/brand/discover/report',
      {
        method: 'POST',
        body: JSON.stringify({ keywords })
      }
    ),

  metaWhitelabel: () =>
    request<WhitelabelMeta>('/meta/whitelabel', { method: 'GET', skipAuthRedirect: true }),

  // 公开构建信息（无需登录，首页/页脚展示打包版本/git-hash/打包时间/打包系统）
  metaSystem: () =>
    request<MetaSystem>('/meta/system', { method: 'GET', skipAuthRedirect: true }),

  brandCompare: (brands: string[]) => {
    const params = new URLSearchParams()
    brands.forEach(b => params.append('brands', b))
    return request<BrandCompareResponse>(
      `/brand/compare?${params.toString()}`,
      { method: 'GET' }
    )
  },

  leaderboard: (category?: string, limit = 50) => {
    const params = new URLSearchParams()
    if (category) params.set('category', category)
    params.set('limit', String(limit))
    return request<LeaderboardResponse>(
      `/leaderboard?${params.toString()}`,
      { method: 'GET' }
    )
  },

  // 管理员后台
  admin: {
    // 审计日志（管理员操作留痕：登录/登出/SQL 执行等）
    auditLog: (params?: { action?: string; limit?: number; offset?: number }) => {
      const qs = new URLSearchParams()
      if (params?.action) qs.set('action', params.action)
      if (params?.limit) qs.set('limit', String(params.limit))
      if (params?.offset) qs.set('offset', String(params.offset))
      return request<AdminAuditLogEntry[]>(`/auth/admin/audit?${qs.toString()}`, { method: 'GET' })
    },
    // 租户列表（支持状态/套餐/分页过滤）
    tenants: (params?: { status?: string; plan?: string; page?: number; limit?: number }) => {
      const qs = new URLSearchParams()
      if (params?.status) qs.set('status', params.status)
      if (params?.plan) qs.set('plan', params.plan)
      if (params?.page) qs.set('page', String(params.page))
      if (params?.limit) qs.set('limit', String(params.limit))
      return request<any>(`/admin/tenants?${qs.toString()}`, { method: 'GET', skipAuthRedirect: true })
    },
    // 租户详情
    tenantDetail: (id: string) =>
      request<any>(`/admin/tenants/${id}`, { method: 'GET', skipAuthRedirect: true }),
    // 更新租户状态（active/suspended）
    updateTenantStatus: (id: string, status: string) =>
      request<any>(`/admin/tenants/${id}/status`, {
        method: 'PUT',
        body: JSON.stringify({ status }),
        skipAuthRedirect: true
      }),
    // 全局用量统计
    usage: () => request<any>('/admin/usage', { method: 'GET', skipAuthRedirect: true }),
    // 公告列表
    announcements: () => request<any>('/admin/announcements', { method: 'GET', skipAuthRedirect: true }),
    // 创建公告
    createAnnouncement: (data: any) =>
      request<any>('/admin/announcements', {
        method: 'POST',
        body: JSON.stringify(data),
        skipAuthRedirect: true
      }),
    // 删除公告
    deleteAnnouncement: (id: string) =>
      request<any>(`/admin/announcements/${id}`, { method: 'DELETE', skipAuthRedirect: true }),
    // 系统信息
    system: () => request<any>('/admin/system', { method: 'GET', skipAuthRedirect: true }),
    // 系统自检（关键业务健康 + 属性/参数/配置校验 + 运行时快照）。
    // skipAuthRedirect：命中 403 时由页面自身展示"需管理员权限"引导，而非全局跳登录。
    selfCheck: () =>
      request<SelfCheckReport>('/admin/selfcheck', {
        method: 'GET',
        skipAuthRedirect: true
      }),
    // AI 爬虫访问监控（哪些大模型来爬过本站）
    aiBotVisits: (limit = 50) =>
      request<AIBotVisitsReport>(`/admin/aibots/visits?limit=${limit}`, {
        method: 'GET'
      }),
    // 管理后台 SQL 执行（管理员，写操作需二次确认）
    dbExec: (sql: string, confirmWrite = false, limit = 200) =>
      request<DBExecResult>('/admin/db/exec', {
        method: 'POST',
        body: JSON.stringify({ sql, confirm_write: confirmWrite, limit })
      }),
    // 系统设置列表（支持分类/关键字过滤）
    settings: (params?: { category?: string; q?: string }) => {
      const qs = new URLSearchParams()
      if (params?.category) qs.set('category', params.category)
      if (params?.q) qs.set('q', params.q)
      return request<any>(`/admin/settings?${qs.toString()}`, { method: 'GET', skipAuthRedirect: true })
    },
    // 更新配置项
    updateSetting: (key: string, value: string) =>
      request<any>('/admin/settings', {
        method: 'PUT',
        body: JSON.stringify({ key, value }),
        skipAuthRedirect: true
      }),
    // 恢复配置为默认值
    resetSetting: (key: string) =>
      request<any>('/admin/settings/reset', {
        method: 'POST',
        body: JSON.stringify({ key })
      }),
    // 测试引擎连通性
    testEngine: (engineKey: string) =>
      request<{ success: boolean; message: string }>('/admin/engine/test', {
        method: 'POST',
        body: JSON.stringify({ engine: engineKey })
      }),
    // 外部系统提交列表 + 统计（status 过滤：''=全部 / pending / analyzed / failed）
    externalSubmissions: (params?: { status?: string; limit?: number }) => {
      const qs = new URLSearchParams()
      if (params?.status) qs.set('status', params.status)
      if (params?.limit) qs.set('limit', String(params.limit))
      return request<ExternalSubmissionsResponse>(`/admin/external-submissions?${qs.toString()}`, { method: 'GET' })
    },
    // 手动触发一次外部提交分析（无需等待定时）
    externalSubmissionsTrigger: () =>
      request<{ processed: number }>('/admin/external-submissions/trigger', { method: 'POST' })
  },

  // 规则集管理（替代原 `geo rules` CLI）
  rules: {
    list: () => request<RulesListResponse>('/rules', { method: 'GET' }),
    default: () => request<RuleSet>('/rules/default', { method: 'GET' }),
    validate: (payload: { content?: string; path?: string }) =>
      request<RuleSetValidateResponse>('/rules/validate', {
        method: 'POST',
        body: JSON.stringify(payload),
        skipAuthRedirect: true
      })
  },

  // GEO 评测（替代原 `geo evaluate` CLI）
  evaluate: {
    run: (payload: {
      dataset: string
      format?: 'md' | 'json'
      live?: boolean
      llm_key?: string
      llm_base?: string
      llm_model?: string
      rules?: string
    }) =>
      request<EvaluateResponse>('/evaluate', {
        method: 'POST',
        body: JSON.stringify(payload),
        timeout: 600000,
        skipAuthRedirect: true
      })
  },

  // 离线工商库（替代原 `geo brand db import-*` CLI）
  offlinedb: {
    stats: () => request<OfflineDBStats>('/brand/offlinedb/stats', { method: 'GET', skipAuthRedirect: true }),
    provinces: () => request<string[]>('/brand/offlinedb/provinces', { method: 'GET', skipAuthRedirect: true }),
    // 上传 JSON 文件导入（multipart）
    // 注意：multipart 无法使用统一的 JSON request() 封装，但保留超时和鉴权一致性
    importFile: async (file: File, batch?: number): Promise<OfflineDBImportResult> => {
      const fd = new FormData()
      fd.append('file', file)
      if (batch) fd.append('batch', String(batch))
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 600000) // 10分钟超时（大文件上传）
      const headers: Record<string, string> = {}
      const tok = getApiAuthToken()
      if (tok) headers['Authorization'] = `Bearer ${tok}`
      try {
        const res = await fetch(`${API_BASE}/brand/offlinedb/import`, {
          method: 'POST',
          body: fd,
          headers,
          signal: controller.signal
        })
        // 401/403 拦截（与统一 request() 保持一致）
        if ((res.status === 401 || res.status === 403)) {
          emitAuthError(res.status, '/brand/offlinedb/import')
        }
        if (!res.ok) {
          const e = (await res.json().catch(() => ({}))) as Record<string, unknown>
          throw new Error((e?.error as string) || `HTTP ${res.status}`)
        }
        return (await res.json()) as OfflineDBImportResult
      } finally {
        clearTimeout(timer)
      }
    },
    // 直连 GitHub 下载并导入
    importGitHub: (payload: OfflineDBImportGitHubRequest) =>
      request<OfflineDBImportResult>('/brand/offlinedb/import-github', {
        method: 'POST',
        body: JSON.stringify(payload),
        timeout: 600000,
        skipAuthRedirect: true
      })
  },

  // 帮助中心
  help: {
    // 按分类获取帮助文章列表
    articles: (category?: string) => {
      const qs = new URLSearchParams()
      if (category) qs.set('category', category)
      return request<any>(`/help/articles?${qs.toString()}`, { method: 'GET' })
    },
    // 文章详情
    articleDetail: (id: string) =>
      request<any>(`/help/articles/${id}`, { method: 'GET' }),
    // 新手引导进度
    onboarding: () => request<any>('/help/onboarding', { method: 'GET' }),
    // 完成引导步骤
    completeStep: (step: number) =>
      request<any>(`/help/onboarding/complete`, {
        method: 'POST',
        body: JSON.stringify({ step })
      })
  },

  // 工单系统
  tickets: {
    // 工单列表（支持状态/类别/分页过滤）
    list: (params?: { status?: string; category?: string; page?: number; limit?: number }) => {
      const qs = new URLSearchParams()
      if (params?.status) qs.set('status', params.status)
      if (params?.category) qs.set('category', params.category)
      if (params?.page) qs.set('page', String(params.page))
      if (params?.limit) qs.set('limit', String(params.limit))
      return request<any>(`/tickets?${qs.toString()}`, { method: 'GET' })
    },
    // 工单详情（含回复列表）
    detail: (id: string) =>
      request<any>(`/tickets/${id}`, { method: 'GET' }),
    // 创建工单
    create: (data: any) =>
      request<any>('/tickets', {
        method: 'POST',
        body: JSON.stringify(data)
      }),
    // 回复工单
    reply: (id: string, content: string) =>
      request<any>(`/tickets/${id}/replies`, {
        method: 'POST',
        body: JSON.stringify({ content })
      }),
    // 更新工单状态
    updateStatus: (id: string, status: string) =>
      request<any>(`/tickets/${id}/status`, {
        method: 'PUT',
        body: JSON.stringify({ status })
      })
  },

  // 官网落地页
  landing: {
    // 功能亮点
    features: () => request<any>('/landing/features', { method: 'GET', skipAuthRedirect: true }),
    // 平台数据
    stats: () => request<any>('/landing/stats', { method: 'GET', skipAuthRedirect: true }),
    // 定价方案列表
    plans: () => request<any>('/landing/plans', { method: 'GET' }),
    // 定价方案详情
    planDetail: (id: string) =>
      request<any>(`/landing/plans/${id}`, { method: 'GET' })
  },

  // ── 法务合规：合规元数据与数据权利（roadmap #80 / #81） ──
  meta: {
    compliance: () => request<any>('/meta/compliance', { method: 'GET', skipAuthRedirect: true })
  },
  legal: {
    // 访问我的数据（个保法第 44-47 条 / GDPR Art.15）
    requestDataAccess: () =>
      request<{
        ok: boolean
        request_id: string
        action: string
        accepted_at: string
        sla: string
        contact: string
        note: string
      }>('/legal/data-access', { method: 'GET' }),
    // 导出我的数据（可携带权：个保法第 45 条 / GDPR Art.20）
    requestDataExport: () =>
      request<{
        ok: boolean
        request_id: string
        action: string
        accepted_at: string
        sla: string
        contact: string
        note: string
      }>('/legal/data-export', { method: 'GET' }),
    // 删除我的数据（删除权 / 被遗忘权：个保法第 47 条 / GDPR Art.17）
    requestDataDelete: () =>
      request<{
        ok: boolean
        request_id: string
        action: string
        accepted_at: string
        sla: string
        contact: string
        note: string
      }>('/legal/data-delete', { method: 'POST' })
  }
}

export default api

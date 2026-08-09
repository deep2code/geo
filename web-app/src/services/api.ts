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
  DriftReport,
  ReadinessReport,
  CrawlabilityReport,
  LocalSEOReport,
  SocialMonitorReport,
  KOLAnalyzeReport,
  TopSourceReport,
  VerticalDetectResult,
  ExternalSignalsReport,
  DiscoverResponse,
  WhitelabelMeta,
  BrandCompareResponse,
  LeaderboardResponse
} from '@/types/api'

const API_BASE = '/api/v1'

export interface RequestOptions extends RequestInit {
  timeout?: number
}

async function request<T>(
  path: string,
  options: RequestOptions = {}
): Promise<T> {
  const { timeout = 120000, headers, ...rest } = options

  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)

  try {
    const res = await fetch(`${API_BASE}${path}`, {
      ...rest,
      headers: {
        'Content-Type': 'application/json',
        ...(headers || {})
      },
      signal: controller.signal
    })

    const contentType = res.headers.get('content-type') || ''
    const isJson = contentType.includes('application/json')

    if (!res.ok) {
      if (isJson) {
        const errJson = await res.json().catch(() => ({}))
        const msg = (errJson as any).error || `HTTP ${res.status}`
        throw new Error(msg)
      }
      const text = await res.text().catch(() => '')
      throw new Error(text || `HTTP ${res.status}`)
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

  mailStatus: () => request<MailStatus>('/mail/status', { method: 'GET' }),
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
  historyBrands: () =>
    request<{ brands: string[] }>('/brand/history/brands', { method: 'GET' }),

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
    request<WhitelabelMeta>('/meta/whitelabel', { method: 'GET' }),

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
  }
}

export default api

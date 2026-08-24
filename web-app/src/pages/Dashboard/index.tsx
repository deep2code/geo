import React, { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Kpi } from '@/components/Kpi'
import { Table, type TableColumn } from '@/components/Table'
import { Button } from '@/components/Button'
import { MatrixBubble, type MatrixBubbleDatum } from '@/components/MatrixBubble'
import { SparklineLine } from '@/components/SparklineLine'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type {
  VisibilityReport,
  HistoryListResponse,
  HistoryStats,
  HistoryDailyResponse,
  EngineStats,
  HistoryRecord,
  ReadyCheckItem,
  MetaSystem
} from '@/types/api'
import './Dashboard.scss'

const Dashboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)
  const [loading, setLoading] = useState(true)
  const [history, setHistory] = useState<HistoryListResponse | null>(null)
  const [historyStats, setHistoryStats] = useState<HistoryStats | null>(null)
  const [historyDaily, setHistoryDaily] = useState<HistoryDailyResponse | null>(null)
  const [metaSystem, setMetaSystem] = useState<MetaSystem | null>(null)
  const [systemReady, setSystemReady] = useState<{
    ready: boolean
    checks: Record<string, ReadyCheckItem>
  } | null>(null)

  const brands = useAppStore(s => s.brands)
  const lastReport = useAppStore(s => s.lastReport)

  useEffect(() => {
    const load = async () => {
      try {
        const [hist, hstats, hdaily, ready, meta] = await Promise.all([
          api.historyList(undefined, 5).catch(() => null),
          api.historyStats().catch(() => null),
          api.historyStatsDaily(30).catch(() => null),
          api.ready().catch(() => null),
          api.metaSystem().catch(() => null)
        ])
        if (hist) setHistory(hist)
        if (hstats) setHistoryStats(hstats)
        if (hdaily) setHistoryDaily(hdaily)
        if (ready) setSystemReady({ ready: ready.status === 'ready', checks: ready.checks })
        if (meta) setMetaSystem(meta)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  // 平均分：优先取自历史统计/最近审计结果；无数据时用占位 null（UI 显示 "—"），绝不使用随机。
  const avgScore: number | null =
    lastReport?.score != null
      ? Math.round(lastReport.score)
      : history?.records && history.records.length > 0
        ? Math.round(
            history.records.reduce((s, r) => s + (r.score ?? 0), 0) / history.records.length
          )
        : historyDaily?.summary?.avg_score_daily != null
          ? Math.round(historyDaily.summary.avg_score_daily)
          : null

  const trend7d: number[] | null = history?.records && history.records.length >= 2
    ? history.records
        .slice()
        .sort((a, b) => +new Date(a.generated_at) - +new Date(b.generated_at))
        .map(r => Math.round(r.score ?? 0))
        .slice(-7)
    : null

  // 30 天趋势：取自历史库按天聚合 count；后端已按日期升序、无记录日期补 0。
  const trend30d: number[] | null = useMemo(() => {
    const recs = historyDaily?.records
    if (!recs || recs.length === 0) return null
    return recs.map(r => Number(r.count) || 0)
  }, [historyDaily])

  // 每日平均分趋势（给内容优化篇数 KPI 的 sparkline 用；没有分数的天用上一天/整体均值兜底不强制，这里无数据即 0）。
  const trend30dScore: number[] | null = useMemo(() => {
    const recs = historyDaily?.records
    if (!recs || recs.length === 0) return null
    return recs.map(r => (r.avg_score != null && r.avg_score >= 0 ? Math.round(r.avg_score) : 0))
  }, [historyDaily])

  // 品牌榜：仅当 brands + 历史均可用时才取最后一次审计分；否则空数组避免 mock。
  // 注：未使用 Array.prototype.findLast 以兼容 ES2022 以下 TS lib 配置。
  const pickLast = (records: HistoryRecord[] | undefined, name: string): HistoryRecord | undefined => {
    if (!records) return undefined
    let last: HistoryRecord | undefined
    for (let i = records.length - 1; i >= 0; i--) {
      if (records[i].brand_name === name) {
        last = records[i]
        break
      }
    }
    return last
  }
  const topBrandsData = brands.map((b) => {
    const last = pickLast(history?.records, b.name)
    const score = last?.score != null ? Math.round(last.score) : null
    const grade = score != null
      ? (score >= 90 ? 'A' : score >= 80 ? 'B' : score >= 70 ? 'C' : score >= 60 ? 'D' : 'F')
      : null
    return {
      id: b.name,
      rank: 0,
      name: b.name,
      score,
      grade,
      trend: null as number[] | null
    }
  }).filter(d => d.score != null)

  const cols: TableColumn<typeof topBrandsData[number]>[] = [
    {
      key: 'rank',
      title: t('dashboard.topBrandsRank'),
      width: 60,
      align: 'center',
      render: (_r, idx) => <span style={{ color: idx < 3 ? 'var(--primary-500)' : undefined }}>{idx + 1}</span>
    },
    { key: 'name', title: '品牌', dataIndex: 'name' },
    {
      key: 'score',
      title: t('dashboard.topBrandsScore'),
      render: (r) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <strong style={{ fontSize: 16 }}>{r.score ?? '—'}</strong>
          {r.grade && (
            <span
              style={{
                padding: '2px 8px',
                borderRadius: 999,
                fontSize: 12,
                fontWeight: 600,
                background: ['A', 'B'].includes(r.grade) ? 'var(--status-success-bg)' : 'var(--status-warning-bg)',
                color: ['A', 'B'].includes(r.grade) ? 'var(--status-success)' : 'var(--status-warning)'
              }}
            >
              {r.grade}
            </span>
          )}
        </div>
      ),
      sortable: true
    },
    {
      key: 'trend',
      title: t('dashboard.trend7d'),
      width: 120,
      render: (r) => (
        <div style={{ height: 32, display: 'flex', alignItems: 'center' }}>
          {r.trend && r.trend.length > 0 ? (
            <SparklineLine data={r.trend} height={32} showArea={false} showEndDot={false} showTooltip={false} />
          ) : (
            <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>—</span>
          )}
        </div>
      )
    }
  ]

  const engineData: MatrixBubbleDatum[] = [
    { id: 'chatgpt', label: 'ChatGPT', x: 85, y: 78, size: 95, category: 'Global' },
    { id: 'perplexity', label: 'Perplexity', x: 90, y: 82, size: 78, category: 'Global' },
    { id: 'gemini', label: 'Gemini', x: 72, y: 70, size: 80, category: 'Global' },
    { id: 'claude', label: 'Claude', x: 88, y: 75, size: 65, category: 'Global' },
    { id: 'qwen', label: '通义千问', x: 65, y: 88, size: 88, category: 'CN' },
    { id: 'glm', label: '智谱GLM', x: 58, y: 76, size: 72, category: 'CN' },
    { id: 'deepseek', label: 'DeepSeek', x: 70, y: 85, size: 68, category: 'CN' },
    { id: 'doubao', label: '豆包', x: 52, y: 90, size: 82, category: 'CN' },
    { id: 'kimi', label: 'Kimi', x: 60, y: 80, size: 58, category: 'CN' }
  ]

  const contentGaps = [
    { id: '1', prompt: '最好的 CRM 软件推荐', engine: 'ChatGPT', competitors: '竞品A, 竞品B', suggestion: '发布 CRM 软件对比评测长文' },
    { id: '2', prompt: '中小企业客户管理系统', engine: 'Perplexity', competitors: '竞品A', suggestion: '撰写「中小企业 CRM 选型指南」' },
    { id: '3', prompt: '国内 SaaS CRM 排行榜', engine: '通义千问', competitors: '竞品B, 竞品C', suggestion: '制作 CRM 排行榜数据图并投稿' },
    { id: '4', prompt: '开源 CRM 系统对比', engine: 'Gemini', competitors: '竞品C', suggestion: '补充开源版产品信息和对比文档' }
  ]

  const recentAudits = history?.records ?? [
    { id: '1', brand_name: '示例科技', generated_at: new Date(Date.now() - 3600_000).toISOString(), score: 78, grade: 'B' },
    { id: '2', brand_name: '示例科技', generated_at: new Date(Date.now() - 86400_000 * 3).toISOString(), score: 74, grade: 'B' },
    { id: '3', brand_name: '示例科技', generated_at: new Date(Date.now() - 86400_000 * 7).toISOString(), score: 71, grade: 'C' }
  ]

  return (
    <div className="dashboard-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('dashboard.title')}</h1>
          <p className="page-subtitle">{t('dashboard.subtitle')}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button variant="secondary" size="sm" onClick={() => navigate('/brand-audit')}>
            🔍 {t('nav.brandAudit')}
          </Button>
          <Button size="sm" onClick={() => navigate('/content-optimizer')}>
            ✍️ {t('nav.contentOptimizer')}
          </Button>
        </div>
      </div>

      <div className="kpi-grid">
        <Kpi
          label={t('dashboard.overviewBvs')}
          value={(lastReport?.score ?? avgScore) != null ? (lastReport?.score ?? avgScore)! : '—'}
          suffix={avgScore != null || lastReport?.score != null ? '/100' : ''}
          icon="🎯"
          trendValue={
            avgScore != null && trend7d != null && trend7d.length >= 2
              ? `${(avgScore - trend7d[0] >= 0 ? '+' : '')}${(avgScore - trend7d[0]).toFixed(1)}`
              : undefined
          }
          trendDirection={
            avgScore != null && trend7d != null && trend7d.length >= 2
              ? (avgScore >= trend7d[0] ? 'up' : 'down')
              : 'neutral'
          }
          sparklineData={trend7d ?? undefined}
          variant="info"
          footer={<span>{t('dashboard.trend7d')}</span>}
        />
        <Kpi
          label={t('dashboard.overviewAuditedBrands')}
          value={historyStats?.unique_brands ?? brands.length}
          icon="🏢"
          trendValue={brands.length > 0 ? `${brands.length}` : undefined}
          trendDirection="neutral"
          sparklineData={brands.length > 0 ? [0, 0, 0, 0, 0, 0, brands.length] : undefined}
          variant="success"
        />
        <Kpi
          label={t('dashboard.overviewContentOptimized')}
          value={historyStats?.total_records ?? historyDaily?.summary?.total_records ?? (loading ? '...' : '—')}
          suffix={typeof (historyStats?.total_records ?? historyDaily?.summary?.total_records) === 'number' ? '篇' : ''}
          icon="📝"
          trendValue={
            (historyStats?.total_records ?? historyDaily?.summary?.total_records) != null
              ? `${historyStats?.total_records ?? historyDaily?.summary?.total_records}`
              : undefined
          }
          trendDirection="neutral"
          sparklineData={trend30d ?? undefined}
          variant="warning"
          footer={
            <span style={{ color: 'var(--text-tertiary)' }}>
              过去 30 天
              {historyDaily?.summary?.daily_avg_records != null
                ? ` · 日均 ${historyDaily.summary.daily_avg_records.toFixed(1)} 篇`
                : ''}
            </span>
          }
        />
        <Kpi
          label="系统就绪"
          value={systemReady?.ready ? '✓' : '⚠️'}
          icon="🩺"
          trendValue={systemReady?.ready ? '所有依赖正常' : '部分依赖未就绪'}
          trendDirection={systemReady?.ready ? 'up' : 'neutral'}
          variant={systemReady?.ready ? 'success' : 'warning'}
          footer={
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {Object.entries(systemReady?.checks ?? {}).map(([k, v]) => {
                const ok = v?.status === 'ok'
                return (
                  <span
                    key={k}
                    title={v?.message || v?.detail || ''}
                    style={{
                      fontSize: 11,
                      padding: '2px 6px',
                      borderRadius: 4,
                      background: ok ? 'var(--status-success-bg)' : 'var(--status-warning-bg)',
                      color: ok ? 'var(--status-success)' : 'var(--status-warning)'
                    }}
                  >
                    {k}: {ok ? 'ok' : (v?.status ?? 'unknown')}
                  </span>
                )
              })}
            </div>
          }
        />
      </div>

      <div className="dashboard-grid">
        <Card title={t('dashboard.topBrands')} subtitle={`${brands.length} brands`} compact>
          <Table columns={cols} dataSource={topBrandsData} rowKey="id" striped />
        </Card>

        <Card title={t('dashboard.engineDistribution')} compact>
          <div style={{ height: 320 }}>
            <MatrixBubble
              data={engineData}
              xLabel="引用率 (%)"
              yLabel="提及率 (%)"
              xMax={100}
              yMax={100}
              onBubbleClick={(d) => showToast(`引擎: ${d.label}, 提及率: ${d.y}, 引用率: ${d.x}`)}
            />
          </div>
        </Card>

        <Card title={t('dashboard.contentGap')} actions={<Button variant="ghost" size="sm">查看全部 →</Button>} compact>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {contentGaps.map(g => (
              <div key={g.id} style={{
                padding: 12,
                borderRadius: 8,
                border: '1px solid var(--border-primary)',
                background: 'var(--surface-secondary)'
              }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                  <strong style={{ fontSize: 13 }}>{g.prompt}</strong>
                  <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>{g.engine}</span>
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 6 }}>
                  竞品被提及: <span style={{ color: 'var(--status-error)' }}>{g.competitors}</span>
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-primary)' }}>
                  💡 {g.suggestion}
                </div>
              </div>
            ))}
          </div>
        </Card>

        <Card title={t('dashboard.recentAudits')} compact>
          <Table
            rowKey="id"
            striped
            dataSource={recentAudits.map(r => ({ ...r, time: r.generated_at })) as any}
            columns={[
              { key: 'brand', title: t('dashboard.recentAuditsBrand'), dataIndex: 'brand_name' as any },
              {
                key: 'score',
                title: t('dashboard.recentAuditsScore'),
                render: (r: any) => <strong>{r.score}</strong>,
                sortable: true
              },
              {
                key: 'grade',
                title: t('dashboard.recentAuditsGrade'),
                render: (r: any) => (
                  <span style={{
                    padding: '2px 8px',
                    borderRadius: 999,
                    fontSize: 12,
                    background: r.grade === 'A' || r.grade === 'B' ? 'var(--status-success-bg)' : 'var(--status-warning-bg)',
                    color: r.grade === 'A' || r.grade === 'B' ? 'var(--status-success)' : 'var(--status-warning)'
                  }}>{r.grade}</span>
                )
              },
              {
                key: 'time',
                title: t('dashboard.recentAuditsTime'),
                render: (r: any) => new Date(r.generated_at).toLocaleString()
              }
            ]}
          />
        </Card>
      </div>

      {/* 系统构建信息（公开接口 /api/v1/meta/system，无需登录） */}
      {metaSystem && (
        <Card title="📦 系统构建信息" compact>
          <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap', fontSize: 13, color: 'var(--text-secondary)' }}>
            <span><b style={{ color: 'var(--text-primary)' }}>打包版本</b>：{metaSystem.build_version}</span>
            <span><b style={{ color: 'var(--text-primary)' }}>打包时间</b>：{metaSystem.build_at && metaSystem.build_at !== 'unknown' ? new Date(metaSystem.build_at).toLocaleString() : '-'}</span>
            <span><b style={{ color: 'var(--text-primary)' }}>打包系统</b>：{metaSystem.build_os || '-'}</span>
            <span style={{ maxWidth: 320, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
              title={metaSystem.build_commit}>
              <b style={{ color: 'var(--text-primary)' }}>git-hash</b>：{metaSystem.build_commit}</span>
            <span><b style={{ color: 'var(--text-primary)' }}>Go</b>：{metaSystem.go_version}</span>
          </div>
        </Card>
      )}
    </div>
  )
}

export default Dashboard

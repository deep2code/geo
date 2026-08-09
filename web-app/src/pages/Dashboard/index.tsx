import React, { useEffect, useState } from 'react'
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
import type { VisibilityReport, HistoryListResponse, EngineStats } from '@/types/api'
import './Dashboard.scss'

const Dashboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)
  const [loading, setLoading] = useState(true)
  const [history, setHistory] = useState<HistoryListResponse | null>(null)
  const [systemReady, setSystemReady] = useState<{
    ready: boolean
    checks: Record<string, string>
  } | null>(null)

  const brands = useAppStore(s => s.brands)
  const lastReport = useAppStore(s => s.lastReport)

  useEffect(() => {
    const load = async () => {
      try {
        const [hist, ready] = await Promise.all([
          api.historyList(undefined, 5).catch(() => null),
          api.ready().catch(() => null)
        ])
        if (hist) setHistory(hist)
        if (ready) setSystemReady({ ready: ready.status === 'ready', checks: ready.checks })
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  const avgScore = brands.length > 0
    ? Math.round(brands.length * 72 + Math.random() * 150) / brands.length
    : 72

  const trend7d = [65, 68, 66, 70, 73, 71, Math.round(avgScore)]
  const trend30d = Array.from({ length: 30 }, (_, i) => 60 + Math.round(Math.sin(i / 3) * 8 + Math.random() * 5))

  const topBrandsData = brands.map((b, i) => ({
    id: b.name,
    rank: i + 1,
    name: b.name,
    score: Math.round(60 + Math.random() * 35),
    grade: ['A', 'B', 'B', 'C', 'B'][i % 5],
    trend: Array.from({ length: 7 }, () => 55 + Math.round(Math.random() * 40))
  }))

  const cols: TableColumn<typeof topBrandsData[number]>[] = [
    { key: 'rank', title: t('dashboard.topBrandsRank'), width: 60, align: 'center' },
    { key: 'name', title: '品牌', dataIndex: 'name' },
    {
      key: 'score',
      title: t('dashboard.topBrandsScore'),
      render: (r) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <strong style={{ fontSize: 16 }}>{r.score}</strong>
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
        </div>
      ),
      sortable: true
    },
    {
      key: 'trend',
      title: t('dashboard.trend7d'),
      width: 120,
      render: (r) => (
        <div style={{ height: 32 }}>
          <SparklineLine data={r.trend} height={32} showArea={false} showEndDot={false} showTooltip={false} />
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
          value={lastReport?.score ?? avgScore}
          suffix="/100"
          icon="🎯"
          trendValue={`+${(avgScore - 65).toFixed(1)}%`}
          trendDirection="up"
          sparklineData={trend7d}
          variant="info"
          footer={<span>{t('dashboard.trend7d')}</span>}
        />
        <Kpi
          label={t('dashboard.overviewAuditedBrands')}
          value={brands.length}
          icon="🏢"
          trendValue="+1"
          trendDirection="up"
          sparklineData={[1, 1, 2, 2, 3, 3, brands.length]}
          variant="success"
        />
        <Kpi
          label={t('dashboard.overviewContentOptimized')}
          value={loading ? '...' : Math.round(80 + Math.random() * 120)}
          suffix="篇"
          icon="📝"
          trendValue="+12"
          trendDirection="up"
          sparklineData={trend30d.slice(-7)}
          variant="warning"
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
              {Object.entries(systemReady?.checks ?? { brand_engine: 'ok' }).map(([k, v]) => (
                <span
                  key={k}
                  style={{
                    fontSize: 11,
                    padding: '2px 6px',
                    borderRadius: 4,
                    background: v === 'ok' ? 'var(--status-success-bg)' : 'var(--status-warning-bg)',
                    color: v === 'ok' ? 'var(--status-success)' : 'var(--status-warning)'
                  }}
                >
                  {k}: {v}
                </span>
              ))}
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
    </div>
  )
}

export default Dashboard

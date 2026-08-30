import React, { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Kpi } from '@/components/Kpi'
import { Table, type TableColumn } from '@/components/Table'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { SparklineLine } from '@/components/SparklineLine'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { LeaderboardResponse, LeaderboardRow } from '@/types/api'
import './Leaderboard.scss'

const LIMIT_OPTIONS = [10, 20, 50, 100]

const Leaderboard: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)
  const addBrand = useAppStore(s => s.addBrand)
  const setCurrentBrand = useAppStore(s => s.setCurrentBrand)
  const brands = useAppStore(s => s.brands)

  const [category, setCategory] = useState<string>('')
  const [limit, setLimit] = useState<number>(50)
  const [loading, setLoading] = useState(false)
  const [data, setData] = useState<LeaderboardResponse | null>(null)

  const fetchData = async () => {
    setLoading(true)
    try {
      const res = await api.leaderboard(category || undefined, limit)
      setData(res)
    } catch (e: any) {
      // 请求失败时退回演示数据兜底（此前是每次切换筛选就用随机 mock
      // 覆盖展示，真实榜单只存活到下一次筛选）
      setData(generateMockData())
      showToast(e?.message || t('common.operationFailed'), 'error')
    } finally {
      setLoading(false)
    }
  }

  const generateMockData = (): LeaderboardResponse => {
    const defaultCategories = [
      { code: '', name: t('leaderboard.allCategories'), count: 120 },
      { code: 'crm', name: 'CRM', count: 18 },
      { code: 'ecommerce', name: '电商 SaaS', count: 24 },
      { code: 'ai_tools', name: 'AI 工具', count: 32 },
      { code: 'finance', name: '金融科技', count: 15 },
      { code: 'health', name: '医疗健康', count: 16 },
      { code: 'education', name: '在线教育', count: 15 }
    ]

    const sampleBrands = [
      '示例科技', '竞品A', '竞品B', '竞品C', 'GlobalOne', 'TechCorp',
      'CloudSoft', 'DataMagic', 'AILeader', 'NichePlayer', 'StarSaaS',
      'ZenithTech', 'PinnacleSoft', 'VertexAI', 'PrimeData'
    ]

    const cats = category ? ['crm', 'ecommerce', 'ai_tools', 'finance', 'health', 'education'] : ['crm', 'ecommerce', 'ai_tools']

    const rows: LeaderboardRow[] = Array.from({ length: Math.min(limit, 30) }, (_, i) => {
      const score = Math.max(30, Math.min(98, 95 - i * 2 - Math.round(Math.random() * 3)))
      const grade = score >= 90 ? 'A' : score >= 80 ? 'B' : score >= 70 ? 'B' : score >= 60 ? 'C' : 'D'
      const tier = i < 5 ? 'household' : i < 15 ? 'midmarket' : 'niche'
      const baseBrand = sampleBrands[i % sampleBrands.length]
      const brandName = i < sampleBrands.length ? baseBrand : `${baseBrand} ${Math.floor(i / sampleBrands.length) + 1}`
      const cat = cats[i % cats.length]
      return {
        rank: i + 1,
        brand_name: brandName,
        brand_domain: `${brandName.toLowerCase().replace(/\s+/g, '')}.com`,
        category: cat,
        score,
        grade,
        tier,
        sov: Math.max(1, Math.round(25 - i * 0.7 + Math.random() * 5)),
        mention_rate: Math.max(5, Math.round(90 - i * 2.5 + Math.random() * 5)),
        citation_rate: Math.max(3, Math.round(85 - i * 2.8 + Math.random() * 4)),
        trend_7d: Array.from({ length: 7 }, (_, ti) => Math.max(40, Math.min(100, score + Math.round(Math.sin(ti + i) * 5 + Math.random() * 4)))),
        updated_at: new Date(Date.now() - i * 3600_000 * Math.random() * 24).toISOString()
      }
    })

    return {
      category: category || '',
      limit,
      generated_at: new Date().toISOString(),
      categories: defaultCategories,
      rows,
      total: rows.length * 4
    }
  }

  // 切换分类/条数自动拉取真实数据（此前直接用随机 mock 覆盖展示）
  useEffect(() => {
    fetchData()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [category, limit])

  const categoryOptions = data?.categories ?? []

  const handleRowClick = (row: LeaderboardRow) => {
    showToast(`${t('leaderboard.viewDetail')}: ${row.brand_name}`, 'info')
  }

  const handleJumpAudit = (row: LeaderboardRow) => {
    const existing = brands.find(b => b.name === row.brand_name)
    if (existing) {
      setCurrentBrand(existing)
    } else {
      const newBrand = {
        name: row.brand_name,
        aliases: [],
        domain: row.brand_domain ?? '',
        products: [],
        industry: '',
        category: row.category,
        prompts: [],
        competitors: []
      }
      addBrand(newBrand)
      setCurrentBrand(newBrand)
    }
    navigate('/brand-audit')
  }

  const top3 = useMemo(() => data?.rows.slice(0, 3) ?? [], [data])

  const columns: TableColumn<LeaderboardRow>[] = [
    {
      key: 'rank',
      title: t('leaderboard.columns.rank'),
      width: 70,
      align: 'center',
      sortable: true,
      render: (r) => (
        <span style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 28,
          height: 28,
          borderRadius: '50%',
          fontWeight: 700,
          fontSize: 13,
          background: r.rank === 1 ? '#fbbf24' : r.rank === 2 ? '#94a3b8' : r.rank === 3 ? '#d97706' : 'var(--bg-tertiary)',
          color: r.rank <= 3 ? '#fff' : 'var(--text-primary)'
        }}>
          {r.rank <= 3 ? ['🥇', '🥈', '🥉'][r.rank - 1] : r.rank}
        </span>
      )
    },
    {
      key: 'brand_name',
      title: t('leaderboard.columns.brandName'),
      sortable: true,
      render: (r) => (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <strong style={{ fontSize: 14 }}>{r.brand_name}</strong>
          {r.brand_domain && (
            <a
              href={`https://${r.brand_domain}`}
              target="_blank"
              rel="noopener noreferrer"
              style={{ fontSize: 11, color: 'var(--text-tertiary)', textDecoration: 'none' }}
              onClick={(e) => e.stopPropagation()}
            >
              🌐 {r.brand_domain}
            </a>
          )}
        </div>
      )
    },
    {
      key: 'category',
      title: t('leaderboard.columns.category'),
      width: 110,
      dataIndex: 'category' as any,
      sortable: true,
      render: (r) => (
        <span style={{
          fontSize: 11,
          padding: '2px 8px',
          borderRadius: 999,
          background: 'var(--surface-tertiary)',
          color: 'var(--text-secondary)',
          textTransform: 'uppercase'
        }}>
          {r.category}
        </span>
      )
    },
    {
      key: 'score',
      title: t('leaderboard.columns.score'),
      width: 100,
      sortable: true,
      render: (r) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <strong style={{ fontSize: 16 }}>{r.score}</strong>
          <span style={{
            fontSize: 11,
            padding: '2px 8px',
            borderRadius: 999,
            fontWeight: 600,
            background: ['A', 'B'].includes(r.grade) ? 'var(--status-success-bg)' : r.grade === 'C' ? 'var(--status-warning-bg)' : 'var(--status-error-bg)',
            color: ['A', 'B'].includes(r.grade) ? 'var(--status-success)' : r.grade === 'C' ? 'var(--status-warning)' : 'var(--status-error)'
          }}>
            {r.grade}
          </span>
        </div>
      )
    },
    {
      key: 'tier',
      title: t('leaderboard.columns.tier'),
      width: 90,
      render: (r) => {
        const tierMap: Record<string, { label: string; color: string; bg: string }> = {
          household: { label: t('leaderboard.tiers.household'), color: '#92400e', bg: '#fef3c7' },
          midmarket: { label: t('leaderboard.tiers.midmarket'), color: '#1e40af', bg: '#dbeafe' },
          niche: { label: t('leaderboard.tiers.niche'), color: '#374151', bg: '#f3f4f6' }
        }
        const tm = tierMap[r.tier] ?? tierMap.niche
        return (
          <span style={{
            fontSize: 11,
            padding: '2px 8px',
            borderRadius: 4,
            fontWeight: 500,
            background: tm.bg,
            color: tm.color
          }}>
            {tm.label}
          </span>
        )
      }
    },
    {
      key: 'sov',
      title: t('leaderboard.columns.sov'),
      width: 90,
      sortable: true,
      align: 'right',
      render: (r) => <span>{r.sov.toFixed(1)}%</span>
    },
    {
      key: 'mention_rate',
      title: t('leaderboard.columns.mentionRate'),
      width: 90,
      sortable: true,
      align: 'right',
      render: (r) => <span>{r.mention_rate}%</span>
    },
    {
      key: 'citation_rate',
      title: t('leaderboard.columns.citationRate'),
      width: 90,
      sortable: true,
      align: 'right',
      render: (r) => <span>{r.citation_rate}%</span>
    },
    {
      key: 'trend_7d',
      title: t('leaderboard.columns.trend7d'),
      width: 110,
      render: (r) => (
        <div style={{ height: 32 }}>
          <SparklineLine data={r.trend_7d} height={32} showArea={false} showEndDot={true} showTooltip={false} />
        </div>
      )
    },
    {
      key: 'updated_at',
      title: t('leaderboard.columns.updatedAt'),
      width: 150,
      sortable: true,
      render: (r) => new Date(r.updated_at).toLocaleString()
    },
    {
      key: 'action',
      title: '',
      width: 100,
      align: 'right',
      render: (r) => (
        <Button
          size="sm"
          variant="ghost"
          onClick={(e) => {
            e.stopPropagation()
            handleJumpAudit(r)
          }}
        >
          {t('leaderboard.jumpAudit')} →
        </Button>
      )
    }
  ]

  return (
    <div className="leaderboard-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('leaderboard.title')}</h1>
          <p className="page-subtitle">{t('leaderboard.subtitle')}</p>
        </div>
      </div>

      <Card>
        <div style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 16,
          alignItems: 'flex-end'
        }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 220 }}>
            <label style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
              {t('leaderboard.selectCategory')}
            </label>
            <select
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              style={{
                padding: '8px 12px',
                borderRadius: 8,
                border: '1px solid var(--border-primary)',
                background: 'var(--bg-primary)',
                color: 'var(--text-primary)',
                fontSize: 14,
                cursor: 'pointer'
              }}
            >
              {categoryOptions.map(cat => (
                <option key={cat.code} value={cat.code}>
                  {cat.name} ({cat.count})
                </option>
              ))}
            </select>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 140 }}>
            <label style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
              {t('leaderboard.limit')}
            </label>
            <select
              value={limit}
              onChange={(e) => setLimit(Number(e.target.value))}
              style={{
                padding: '8px 12px',
                borderRadius: 8,
                border: '1px solid var(--border-primary)',
                background: 'var(--bg-primary)',
                color: 'var(--text-primary)',
                fontSize: 14,
                cursor: 'pointer'
              }}
            >
              {LIMIT_OPTIONS.map(n => (
                <option key={n} value={n}>Top {n}</option>
              ))}
            </select>
          </div>

          <Button variant="secondary" onClick={fetchData} loading={loading}>
            🔄 {t('leaderboard.refresh')}
          </Button>

          {data && (
            <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 16 }}>
              <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                {t('leaderboard.generatedAt')}: {new Date(data.generated_at).toLocaleString()}
              </span>
            </div>
          )}
        </div>
      </Card>

      {data && top3.length >= 3 && (
        <div className="kpi-grid" style={{ marginTop: 24 }}>
          <Kpi
            label={`🥈 #2 ${top3[1].brand_name}`}
            value={top3[1].score}
            suffix="/100"
            icon="🏆"
            variant="info"
            trendValue={`SOV ${top3[1].sov}%`}
            trendDirection="neutral"
            sparklineData={top3[1].trend_7d}
          />
          <Kpi
            label={`🥇 #1 ${top3[0].brand_name}`}
            value={top3[0].score}
            suffix="/100"
            icon="👑"
            variant="success"
            trendValue={`SOV ${top3[0].sov}%`}
            trendDirection="up"
            sparklineData={top3[0].trend_7d}
            footer={<strong style={{ color: 'var(--status-success)' }}>{t('leaderboard.totalBrands')}: {data.total}</strong>}
          />
          <Kpi
            label={`🥉 #3 ${top3[2].brand_name}`}
            value={top3[2].score}
            suffix="/100"
            icon="🏆"
            variant="warning"
            trendValue={`SOV ${top3[2].sov}%`}
            trendDirection="neutral"
            sparklineData={top3[2].trend_7d}
          />
        </div>
      )}

      <Card style={{ marginTop: 24 }}>
        <Table
          columns={columns}
          dataSource={data?.rows ?? []}
          rowKey={(r) => `${r.rank}-${r.brand_name}`}
          loading={loading}
          striped
          pagination
          pageSize={20}
          onRowClick={handleRowClick}
          emptyText={t('leaderboard.noData')}
        />
      </Card>
    </div>
  )
}

export default Leaderboard

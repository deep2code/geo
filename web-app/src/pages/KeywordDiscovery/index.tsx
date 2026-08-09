import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Input, Button, Tabs, TabPane, Table, MatrixBubble, Kpi, type TableColumn, type MatrixBubbleDatum } from '@/components'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { DiscoverKeyword, DiscoverResponse } from '@/types/api'
import '../Dashboard/Dashboard.scss'

const marketOptions = [
  { v: 'cn', l: '🇨🇳 中国' },
  { v: 'us', l: '🇺🇸 美国' },
  { v: 'jp', l: '🇯🇵 日本' },
  { v: 'kr', l: '🇰🇷 韩国' },
  { v: 'de', l: '🇩🇪 德国' },
  { v: 'fr', l: '🇫🇷 法国' },
  { v: 'uk', l: '🇬🇧 英国' },
  { v: 'global', l: '🌍 全球' }
]

const intentColor: Record<string, string> = {
  informational: 'var(--status-info)',
  navigational: 'var(--status-warning)',
  transactional: 'var(--status-success)',
  commercial: 'var(--chart-tertiary)'
}

const KeywordDiscovery: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)
  const [seeds, setSeeds] = useState('CRM软件, 客户管理系统, SaaS销售工具')
  const [market, setMarket] = useState('cn')
  const [language, setLanguage] = useState('zh')
  const [loading, setLoading] = useState(false)
  const [viewMode, setViewMode] = useState<'list' | 'matrix'>('list')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [data, setData] = useState<DiscoverResponse | null>(null)

  const run = async () => {
    const arr = seeds.split(/[,，]/).map(s => s.trim()).filter(Boolean)
    if (arr.length === 0) return showToast('请输入种子关键词', 'error')
    setLoading(true)
    try {
      const r: DiscoverResponse = await api.discover(arr, market, language)
      setData(r)
      showToast(`发现 ${r.keywords.length} 个关键词，${r.clusters.length} 个聚类`, 'success')
    } catch (e: any) {
      showToast(e.message || '发现失败（后端 Discover 可能未实现），使用示例数据', 'warning')
      const sample: DiscoverResponse = {
        seed_keywords: arr,
        market,
        language,
        clusters: [
          { name: 'CRM选型对比', keywords: ['CRM软件对比', 'CRM系统推荐2025', '中小企业CRM排行'], geo_potential: 92 },
          { name: 'SaaS销售工具', keywords: ['销售自动化工具', '线索管理系统', '客户跟进软件'], geo_potential: 85 },
          { name: '客户管理系统', keywords: ['客户档案管理', '客户跟进系统', '销售CRM'], geo_potential: 78 }
        ],
        keywords: Array.from({ length: 40 }, (_, i) => {
          const base = arr[i % arr.length]
          const suff = ['排行榜', '对比', '推荐', '哪家好', '价格', '功能', '开源', '免费版', '2025', '中小企业', '报价', '厂商']
          const intents: DiscoverKeyword['intent'][] = ['informational', 'commercial', 'transactional', 'navigational']
          const intent = intents[i % 4]
          const difficulty = 20 + Math.round(Math.random() * 70)
          const search_volume = 500 + Math.round(Math.random() * 50000)
          const relevance = Math.min(100, 60 + Math.random() * 40)
          const priority: DiscoverKeyword['priority'] = difficulty < 50 && relevance > 75 ? 'high' : difficulty < 70 ? 'medium' : 'low'
          return {
            keyword: base + ' ' + suff[i % suff.length] + (i > suff.length ? ' ' + (i - suff.length) : ''),
            search_volume,
            difficulty,
            relevance,
            intent,
            cpc: +(0.5 + Math.random() * 15).toFixed(1),
            priority,
            trend: Array.from({ length: 7 }, () => 50 + Math.round(Math.random() * 50)),
            cluster: (['CRM选型对比', 'SaaS销售工具', '客户管理系统'] as const)[i % 3]
          }
        })
      }
      setData(sample)
    } finally {
      setLoading(false)
    }
  }

  const toggleSelect = (k: string) => {
    const next = new Set(selected)
    next.has(k) ? next.delete(k) : next.add(k)
    setSelected(next)
  }

  const cols: TableColumn<DiscoverKeyword & { sel: boolean }>[] = [
    {
      key: 'sel',
      title: <input type="checkbox" checked={selected.size === (data?.keywords.length ?? 0) && selected.size > 0}
        onChange={(e) => {
          if (!data) return
          if (e.target.checked) setSelected(new Set(data.keywords.map(k => k.keyword)))
          else setSelected(new Set())
        }} />,
      width: 40,
      align: 'center',
      render: (r) => (
        <input type="checkbox" checked={selected.has(r.keyword)} onChange={() => toggleSelect(r.keyword)} />
      )
    },
    { key: 'keyword', title: t('keywordDiscovery.keyword'), dataIndex: 'keyword', sortable: true },
    {
      key: 'cluster',
      title: t('keywordDiscovery.cluster'),
      dataIndex: 'cluster',
      render: (r) => r.cluster ? (
        <span style={{ padding: '2px 8px', borderRadius: 999, fontSize: 11, background: 'var(--status-info-bg)', color: 'var(--status-info)' }}>
          {r.cluster}
        </span>
      ) : <span style={{ color: 'var(--text-tertiary)' }}>-</span>
    },
    {
      key: 'sv',
      title: t('keywordDiscovery.searchVolume'),
      dataIndex: 'search_volume',
      sortable: true,
      render: (r) => <strong>{r.search_volume.toLocaleString()}</strong>
    },
    {
      key: 'diff',
      title: t('keywordDiscovery.difficulty'),
      dataIndex: 'difficulty',
      sortable: true,
      render: (r) => (
        <span style={{
          color: r.difficulty < 40 ? 'var(--status-success)' : r.difficulty < 70 ? 'var(--status-warning)' : 'var(--status-error)',
          fontWeight: 600
        }}>{r.difficulty}</span>
      )
    },
    {
      key: 'rel',
      title: t('keywordDiscovery.relevance'),
      dataIndex: 'relevance',
      sortable: true,
      render: (r) => <span style={{ fontWeight: 600 }}>{r.relevance.toFixed(0)}</span>
    },
    {
      key: 'intent',
      title: t('keywordDiscovery.intent'),
      align: 'center',
      render: (r) => (
        <span style={{
          padding: '2px 8px', borderRadius: 999, fontSize: 11,
          background: `color-mix(in srgb, ${intentColor[r.intent]}, transparent 85%)`,
          color: intentColor[r.intent],
          fontWeight: 600
        }}>
          {t(`keywordDiscovery.intents.${r.intent}`)}
        </span>
      )
    },
    {
      key: 'cpc',
      title: t('keywordDiscovery.cpc'),
      render: (r) => <span>¥{r.cpc.toFixed(1)}</span>
    },
    {
      key: 'priority',
      title: t('keywordDiscovery.priority'),
      align: 'center',
      render: (r) => {
        const color = r.priority === 'high' ? 'var(--status-success)' : r.priority === 'medium' ? 'var(--status-warning)' : 'var(--text-tertiary)'
        return (
          <span style={{
            padding: '2px 8px', borderRadius: 999, fontSize: 11,
            background: `color-mix(in srgb, ${color}, transparent 88%)`,
            color, fontWeight: 600
          }}>{t(`keywordDiscovery.priorities.${r.priority}`)}</span>
        )
      }
    }
  ]

  const bubbleData: MatrixBubbleDatum[] = (data?.keywords ?? []).slice(0, 35).map(k => ({
    id: k.keyword,
    label: k.keyword.length > 6 ? k.keyword.slice(0, 6) : k.keyword,
    x: k.relevance,
    y: 100 - k.difficulty,
    size: k.search_volume / 1000,
    color: intentColor[k.intent],
    category: k.intent,
    value: k.cpc
  }))

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('keywordDiscovery.title')}</h1>
          <p className="page-subtitle">{t('keywordDiscovery.subtitle')}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          {data && (
            <>
              <Button variant="secondary" onClick={() => setViewMode(viewMode === 'list' ? 'matrix' : 'list')}>
                {viewMode === 'list' ? '📊 ' + t('keywordDiscovery.matrixView') : '📋 ' + t('keywordDiscovery.listView')}
              </Button>
              <Button variant="secondary" onClick={() => showToast(`导出 ${selected.size || data.keywords.length} 关键词为 CSV`, 'success')}>
                ⬇️ {t('keywordDiscovery.exportKeywords')}
              </Button>
              <Button onClick={() => showToast(selected.size ? `为 ${selected.size} 个关键词生成 GEO 报告` : t('keywordDiscovery.selectKeywords'), selected.size ? 'success' : 'warning')}>
                📝 {t('keywordDiscovery.generateReport')}
              </Button>
            </>
          )}
        </div>
      </div>

      <Card title="发现参数" compact>
        <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr auto', gap: 12, alignItems: 'flex-end' }}>
          <Input
            label={t('keywordDiscovery.seedKeywords')}
            value={seeds}
            onChange={(e) => setSeeds(e.target.value)}
            hint={t('keywordDiscovery.seedKeywordsHint')}
          />
          <div>
            <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
              {t('keywordDiscovery.targetMarket')}
            </label>
            <select value={market} onChange={(e) => setMarket(e.target.value)}
              style={{
                width: '100%', padding: '8px 12px', borderRadius: 8,
                border: '1px solid var(--border-primary)',
                background: 'var(--surface-primary)', color: 'var(--text-primary)', fontSize: 13
              }}>
              {marketOptions.map(m => <option key={m.v} value={m.v}>{m.l}</option>)}
            </select>
          </div>
          <Input label={t('keywordDiscovery.targetLanguage')} value={language}
            onChange={(e) => setLanguage(e.target.value)}
            placeholder="zh / en / ja" />
          <Button onClick={run} loading={loading} style={{ marginBottom: 2 }}>
            🔑 {t('keywordDiscovery.discoverButton')}
          </Button>
        </div>
      </Card>

      {data && (
        <div style={{ marginTop: 16, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
            <Kpi label="发现关键词" value={data.keywords.length} icon="🔑" variant="info" suffix="个" />
            <Kpi label="聚类数量" value={data.clusters.length} icon="🧩" variant="success" suffix="组" />
            <Kpi label="高优先级" value={data.keywords.filter(k => k.priority === 'high').length}
              icon="⭐" variant="success" suffix="个" />
            <Kpi label="已选择" value={selected.size} icon="✅" variant="warning" suffix={`/ ${data.keywords.length}`} />
          </div>

          <Card title={t('keywordDiscovery.clusterAnalysis')} compact>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: 12 }}>
              {data.clusters.map((c, i) => (
                <div key={i} style={{
                  padding: 14, borderRadius: 8,
                  border: '1px solid var(--border-primary)',
                  background: 'var(--surface-secondary)'
                }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                    <strong>{c.name}</strong>
                    <span style={{
                      padding: '2px 8px', borderRadius: 999, fontSize: 11,
                      background: c.geo_potential >= 85 ? 'var(--status-success-bg)' : 'var(--status-warning-bg)',
                      color: c.geo_potential >= 85 ? 'var(--status-success)' : 'var(--status-warning)',
                      fontWeight: 600
                    }}>GEO {c.geo_potential}</span>
                  </div>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                    {c.keywords.map((kw, j) => (
                      <span key={j} style={{
                        padding: '2px 8px', borderRadius: 4,
                        fontSize: 11, background: 'var(--surface-primary)',
                        border: '1px solid var(--border-primary)',
                        cursor: 'pointer'
                      }} onClick={() => toggleSelect(kw)}>
                        {kw}
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </Card>

          <Tabs>
            <TabPane tabKey="list" tab={t('keywordDiscovery.listView')}>
              <Card compact>
                <Table
                  rowKey="keyword"
                  pagination
                  pageSize={10}
                  striped
                  dataSource={(data.keywords.map(k => ({ ...k, sel: selected.has(k.keyword) }))) as any}
                  columns={cols as any}
                />
              </Card>
            </TabPane>
            <TabPane tabKey="matrix" tab={t('keywordDiscovery.matrixView')}>
              <Card compact>
                <div style={{ height: 480 }}>
                  <MatrixBubble
                    data={bubbleData}
                    xLabel="相关度 (Relevance %)"
                    yLabel="低难度度 (100-Difficulty)"
                    xMax={100}
                    yMax={100}
                    formatValue={(v) => '¥' + v.toFixed(1)}
                    onBubbleClick={(d) => toggleSelect(d.id)}
                  />
                </div>
                <div style={{ marginTop: 12, display: 'flex', gap: 16, fontSize: 12 }}>
                  {Object.entries(intentColor).map(([k, v]) => (
                    <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                      <span style={{ width: 10, height: 10, borderRadius: '50%', background: v }} />
                      {t(`keywordDiscovery.intents.${k}`)}
                    </div>
                  ))}
                </div>
              </Card>
            </TabPane>
          </Tabs>
        </div>
      )}
    </div>
  )
}

export default KeywordDiscovery

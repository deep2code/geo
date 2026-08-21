import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { SparklineLine } from '@/components/SparklineLine'
import api from '@/services/api'
import type { SourceStat, TrendPoint, EngineSource } from '@/types/api'

interface Props {
  brandName: string
  engines: string[]
}

const CATEGORY_LABELS: Record<string, string> = {
  review_site: '评测站', docs: '技术文档', social: '社交/问答', news: '新闻媒体',
  blog: '博客/内容', video: '视频平台', other: '其他'
}

// SourceStudy 引擎来源偏好研究：每个大模型喜欢采用哪里的文章（排行/趋势/引擎对比）。
const SourceStudy: React.FC<Props> = ({ brandName, engines }) => {
  const { t } = useTranslation()
  const [engine, setEngine] = useState('')
  const [days, setDays] = useState(90)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [compare, setCompare] = useState<EngineSource[]>([])
  const [top, setTop] = useState<SourceStat[]>([])
  const [trend, setTrend] = useState<TrendPoint[]>([])
  const [trendDomain, setTrendDomain] = useState('')

  const engineOptions = ['', ...Array.from(new Set(engines))]

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const base = { brand: brandName || undefined, days }
      const [c, tp, tr] = await Promise.all([
        api.engineSourcesCompare({ ...base, limit: 5 }),
        api.engineSourcesTop({ ...base, engine: engine || undefined, limit: 10 }),
        api.engineSourcesTrend({ ...base, engine: engine || undefined })
      ])
      setCompare(c.engines || [])
      setTop(tp.sources || [])
      setTrend(tr.trend || [])
    } catch (e: any) {
      setError(e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [brandName, engine, days])

  useEffect(() => { load() }, [load])

  // 点击来源域名 → 查看该来源单域名趋势。
  const loadDomainTrend = async (domain: string) => {
    setTrendDomain(domain)
    try {
      const tr = await api.engineSourcesTrend({ brand: brandName || undefined, engine: engine || undefined, domain, days })
      setTrend(tr.trend || [])
    } catch { /* 保持原趋势 */ }
  }

  const bar = (pct: number) => (
    <div style={{
      height: 6, borderRadius: 3, background: 'var(--bg-tertiary)', overflow: 'hidden', flex: 1
    }}>
      <div style={{
        height: '100%', borderRadius: 3,
        background: 'var(--brand-primary)', width: `${Math.min(pct, 100)}%`
      }} />
    </div>
  )

  const selectStyle = {
    padding: '6px 10px', borderRadius: 8, fontSize: 13,
    border: '1px solid var(--border-primary)',
    background: 'var(--surface-primary)', color: 'var(--text-primary)'
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {/* 控制行 */}
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
        <select value={engine} onChange={e => setEngine(e.target.value)} style={selectStyle}>
          <option value="">全部引擎</option>
          {engineOptions.filter(e => e).map(e => <option key={e} value={e}>{e}</option>)}
        </select>
        <select value={days} onChange={e => setDays(parseInt(e.target.value, 10))} style={selectStyle}>
          {[30, 90, 180, 365].map(d => <option key={d} value={d}>近 {d} 天</option>)}
        </select>
        <Button size="sm" variant="secondary" loading={loading} onClick={load}>刷新</Button>
        {trendDomain && (
          <Button size="xs" variant="secondary" onClick={() => { setTrendDomain(''); load() }}>
            清除来源过滤（{trendDomain}）
          </Button>
        )}
        <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
          {t('brandAudit.sourceStudyHint')}
        </span>
      </div>

      {error && (
        <Card compact>
          <div style={{ padding: 16, textAlign: 'center', color: 'var(--status-error)', fontSize: 13 }}>
            {error}
          </div>
        </Card>
      )}

      {!error && (
        <>
          {/* 引擎对比：每个大模型 Top 来源 */}
          <Card compact title="🧠 各引擎偏好来源（Top 5）">
            {compare.length === 0 ? (
              <div style={{ padding: 24, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 13 }}>
                暂无数据——运行一次品牌审计后，系统会自动记录每个引擎引用的来源。
              </div>
            ) : (
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 12 }}>
                {compare.map(ec => (
                  <div key={ec.engine} style={{
                    padding: 14, borderRadius: 8, border: '1px solid var(--border-primary)',
                    background: 'var(--surface-secondary)'
                  }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
                      <strong style={{ fontSize: 14 }}>{ec.engine}</strong>
                      <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>{ec.total_citations} 次引用</span>
                    </div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      {(ec.top_sources || []).map(s => (
                        <div key={s.source_domain}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 3 }}>
                            <span>
                              <button
                                onClick={() => loadDomainTrend(s.source_domain)}
                                style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', color: 'var(--brand-primary)', font: 'inherit', textAlign: 'left' }}
                                title="查看该来源趋势"
                              >
                                {s.source_domain}
                              </button>
                              <span style={{ color: 'var(--text-tertiary)', marginLeft: 6, fontSize: 11 }}>
                                {CATEGORY_LABELS[s.category] || s.category}
                              </span>
                            </span>
                            <span style={{ color: 'var(--text-secondary)' }}>{s.citation_count} 次 · {s.share_percent}%</span>
                          </div>
                          {bar(s.share_percent)}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>

          {/* 来源排行明细 */}
          <Card compact title="📊 来源排行（全部来源）">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {top.length === 0 && (
                <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 13 }}>
                  {t('brandAudit.sourceStudyNoData')}
                </div>
              )}
              {top.map(s => (
                <div key={s.source_domain} style={{
                  display: 'grid', gridTemplateColumns: '200px 110px 80px 80px 110px 1fr', gap: 8, alignItems: 'center', fontSize: 12
                }}>
                  <button
                    onClick={() => loadDomainTrend(s.source_domain)}
                    style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', color: 'var(--brand-primary)', font: 'inherit', textAlign: 'left', fontWeight: 600 }}
                  >
                    {s.source_domain}
                  </button>
                  <span style={{ color: 'var(--text-tertiary)' }}>{CATEGORY_LABELS[s.category] || s.category}</span>
                  <span>{s.citation_count} 次引用</span>
                  <span style={{ color: 'var(--text-tertiary)' }}>{s.prompt_count} 查询</span>
                  <span>{s.share_percent}% 占比</span>
                  {bar(s.share_percent)}
                </div>
              ))}
            </div>
          </Card>

          {/* 趋势 */}
          <Card compact title={`📈 引用趋势${trendDomain ? `：${trendDomain}` : ''}${engine ? `（${engine}）` : '（全部引擎）'}`}>
            {trend.length === 0 ? (
              <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 13 }}>
                {t('brandAudit.sourceStudyNoData')}
              </div>
            ) : (
              <div>
                <SparklineLine
                  data={trend.map(p => p.citation_count)}
                  height={60}
                  variant="info"
                  showTooltip
                  formatValue={(v) => `${v} 次`}
                />
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 10 }}>
                  {trend.slice(-14).map(p => (
                    <span key={p.date} style={{ fontSize: 10, color: 'var(--text-tertiary)', background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>
                      {p.date.slice(5)} · {p.citation_count}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  )
}

export default SourceStudy

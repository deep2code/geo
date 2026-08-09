import React, { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Kpi } from '@/components/Kpi'
import { Table, type TableColumn } from '@/components/Table'
import { Tabs, TabPane } from '@/components/Tabs'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { BrandCompareResponse } from '@/types/api'
import './Compare.scss'

const COLORS = ['#6366f1', '#f59e0b', '#10b981', '#ef4444', '#3b82f6', '#8b5cf6', '#ec4899']

const RadarChart: React.FC<{ data: BrandCompareResponse }> = ({ data }) => {
  const { radar_axes, radar_values } = data
  const size = 400
  const center = size / 2
  const radius = 140
  const levels = 5

  const angleForAxis = (i: number) => (Math.PI * 2 * i) / radar_axes.length - Math.PI / 2

  const pointForValue = (axisIdx: number, value: number, max: number) => {
    const angle = angleForAxis(axisIdx)
    const r = (value / max) * radius
    return {
      x: center + Math.cos(angle) * r,
      y: center + Math.sin(angle) * r
    }
  }

  return (
    <svg viewBox={`0 0 ${size} ${size}`} style={{ width: '100%', maxWidth: size, margin: '0 auto', display: 'block' }}>
      {Array.from({ length: levels }).map((_, li) => {
        const lr = (radius * (li + 1)) / levels
        return (
          <polygon
            key={`level-${li}`}
            points={radar_axes.map((_, i) => {
              const angle = angleForAxis(i)
              return `${center + Math.cos(angle) * lr},${center + Math.sin(angle) * lr}`
            }).join(' ')}
            fill="none"
            stroke="var(--border-secondary)"
            strokeWidth="1"
          />
        )
      })}
      {radar_axes.map((axis, i) => {
        const angle = angleForAxis(i)
        return (
          <line
            key={`axis-${axis.key}`}
            x1={center}
            y1={center}
            x2={center + Math.cos(angle) * radius}
            y2={center + Math.sin(angle) * radius}
            stroke="var(--border-secondary)"
            strokeWidth="1"
          />
        )
      })}
      {radar_values.map((rv, bi) => {
        const color = COLORS[bi % COLORS.length]
        const pts = radar_axes.map((axis, ai) => {
          const val = rv.values[axis.key] ?? 0
          const p = pointForValue(ai, val, axis.max)
          return `${p.x},${p.y}`
        }).join(' ')
        return (
          <g key={`radar-${rv.brand}`}>
            <polygon
              points={pts}
              fill={color}
              fillOpacity="0.15"
              stroke={color}
              strokeWidth="2"
            />
            {radar_axes.map((axis, ai) => {
              const val = rv.values[axis.key] ?? 0
              const p = pointForValue(ai, val, axis.max)
              return (
                <circle
                  key={`pt-${rv.brand}-${axis.key}`}
                  cx={p.x}
                  cy={p.y}
                  r="3"
                  fill={color}
                />
              )
            })}
          </g>
        )
      })}
      {radar_axes.map((axis, i) => {
        const angle = angleForAxis(i)
        const lr = radius + 24
        const x = center + Math.cos(angle) * lr
        const y = center + Math.sin(angle) * lr
        return (
          <text
            key={`label-${axis.key}`}
            x={x}
            y={y}
            textAnchor="middle"
            dominantBaseline="middle"
            fontSize="12"
            fill="var(--text-secondary)"
          >
            {axis.label}
          </text>
        )
      })}
      <g transform="translate(12, 12)">
        {radar_values.map((rv, bi) => {
          const color = COLORS[bi % COLORS.length]
          return (
            <g key={`legend-${rv.brand}`} transform={`translate(0, ${bi * 20})`}>
              <rect width="14" height="14" rx="2" fill={color} />
              <text x="20" y="11" fontSize="12" fill="var(--text-primary)">{rv.brand}</text>
            </g>
          )
        })}
      </g>
    </svg>
  )
}

const Compare: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)
  const brands = useAppStore(s => s.brands)

  const [searchInput, setSearchInput] = useState('')
  const [selectedBrands, setSelectedBrands] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<BrandCompareResponse | null>(null)

  const candidateBrands = useMemo(() => {
    const allBrands = new Set(brands.map(b => b.name))
    brands.forEach(b => b.competitors?.forEach(c => allBrands.add(c.name)))
    const list = Array.from(allBrands)
    if (!searchInput.trim()) return list
    return list.filter(n => n.toLowerCase().includes(searchInput.trim().toLowerCase()))
  }, [brands, searchInput])

  const handleAddBrand = (name: string) => {
    if (!name || selectedBrands.includes(name)) return
    setSelectedBrands(prev => [...prev, name])
    setSearchInput('')
  }

  const handleRemoveBrand = (name: string) => {
    setSelectedBrands(prev => prev.filter(b => b !== name))
  }

  const handleClearAll = () => {
    setSelectedBrands([])
    setResult(null)
  }

  const handleCompare = async () => {
    if (selectedBrands.length < 2) {
      showToast(t('compare.noSelection'), 'warning')
      return
    }
    setLoading(true)
    try {
      const res = await api.brandCompare(selectedBrands)
      setResult(res)
      showToast(t('common.operationSuccess'), 'success')
    } catch (e: any) {
      showToast(e?.message || t('common.operationFailed'), 'error')
    } finally {
      setLoading(false)
    }
  }

  const mockCompare = (): BrandCompareResponse => {
    const radarAxes = [
      { key: 'mention_rate', label: t('leaderboard.columns.mentionRate'), max: 100 },
      { key: 'citation_rate', label: t('leaderboard.columns.citationRate'), max: 100 },
      { key: 'content_quality', label: t('brandAudit.contentQuality'), max: 100 },
      { key: 'technical_seo', label: t('brandAudit.technicalSeo'), max: 100 },
      { key: 'on_page_seo', label: t('brandAudit.onPageSeo'), max: 100 },
      { key: 'ai_readiness', label: t('brandAudit.aiReadiness'), max: 100 },
      { key: 'schema', label: t('brandAudit.schema'), max: 100 }
    ]
    const radarValues = selectedBrands.map((brand, bi) => {
      const base = 50 + bi * 10
      const values: Record<string, number> = {}
      radarAxes.forEach((a, ai) => {
        values[a.key] = Math.max(10, Math.min(100, base + Math.round(Math.sin(bi + ai) * 20 + Math.random() * 15)))
      })
      return { brand, values }
    })

    const dimensions = radarAxes.map(a => ({
      key: a.key,
      label: a.label,
      weight: 1 / radarAxes.length,
      values: Object.fromEntries(selectedBrands.map(b => [b, radarValues.find(rv => rv.brand === b)?.values[a.key] ?? 0]))
    }))

    const categories = [t('compare.category') + ' 1', t('compare.category') + ' 2', t('compare.category') + ' 3']
    const diffTable = radarAxes.map((a, ai) => {
      const values: Record<string, number> = {}
      selectedBrands.forEach((b) => {
        values[b] = radarValues.find(rv => rv.brand === b)?.values[a.key] ?? 0
      })
      const entries = Object.entries(values)
      const winner = entries.length ? entries.reduce((a, b) => (b[1] > a[1] ? b : a))[0] : null
      const maxVal = Math.max(...Object.values(values))
      const delta: Record<string, number> = {}
      entries.forEach(([b, v]) => { delta[b] = Math.round((v - maxVal) * 10) / 10 })
      return {
        key: a.key,
        label: a.label,
        category: categories[ai % categories.length],
        values,
        winner,
        delta
      }
    })

    const overallScores = selectedBrands.map(b => {
      const sum = radarAxes.reduce((acc, a) => acc + (radarValues.find(rv => rv.brand === b)?.values[a.key] ?? 0), 0)
      return { brand: b, score: Math.round(sum / radarAxes.length) }
    })
    const overallWinner = overallScores.length ? overallScores.reduce((a, b) => (b.score > a.score ? b : a)).brand : null

    const strengths: Record<string, string[]> = {}
    const weaknesses: Record<string, string[]> = {}
    selectedBrands.forEach(b => {
      strengths[b] = diffTable.filter(r => r.winner === b).map(r => r.label).slice(0, 3)
      weaknesses[b] = diffTable
        .filter(r => r.winner !== b)
        .sort((a, b2) => (a.delta[b] ?? 0) - (b2.delta[b] ?? 0))
        .map(r => r.label).slice(0, 3)
    })

    return {
      brands: selectedBrands,
      generated_at: new Date().toISOString(),
      dimensions,
      radar_axes: radarAxes,
      radar_values: radarValues,
      diff_table: diffTable,
      summary: { overall_winner: overallWinner, strengths, weaknesses }
    }
  }

  useEffect(() => {
    if (selectedBrands.length >= 2 && selectedBrands.length <= 4) {
      setResult(mockCompare())
    }
  }, [selectedBrands])

  const overallScores = useMemo(() => {
    if (!result) return []
    return result.radar_values.map(rv => {
      const sum = result.radar_axes.reduce((acc, a) => acc + (rv.values[a.key] ?? 0), 0)
      return { brand: rv.brand, score: Math.round(sum / result.radar_axes.length) }
    })
  }, [result])

  const diffColumns: TableColumn<any>[] = useMemo(() => {
    if (!result) return []
    const cols: TableColumn<any>[] = [
      { key: 'category', title: t('compare.category'), dataIndex: 'category' as any, width: 100 },
      { key: 'dimension', title: t('compare.dimension'), dataIndex: 'label' as any },
    ]
    result.brands.forEach(b => {
      cols.push({
        key: b,
        title: b,
        sortable: true,
        render: (r) => {
          const val = r.values?.[b]
          const isWinner = r.winner === b
          return (
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <strong style={isWinner ? { color: 'var(--status-success)' } : undefined}>
                {val}
              </strong>
              {isWinner && <span style={{ fontSize: 12 }}>🏆</span>}
              {r.delta?.[b] != null && r.winner !== b && (
                <span style={{ fontSize: 11, color: 'var(--status-error)' }}>
                  ({r.delta[b]})
                </span>
              )}
            </div>
          )
        }
      })
    })
    cols.push({ key: 'winner', title: t('compare.winner'), width: 100, render: (r) => r.winner || '-' })
    return cols
  }, [result, t])

  return (
    <div className="compare-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('compare.title')}</h1>
          <p className="page-subtitle">{t('compare.subtitle')}</p>
        </div>
      </div>

      <Card title={t('compare.selectBrands')} subtitle={t('compare.selectBrandsHint')}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'flex-start' }}>
            <Input
              placeholder={t('compare.searchPlaceholder')}
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && candidateBrands[0]) {
                  handleAddBrand(candidateBrands[0])
                }
              }}
              style={{ flex: 1, minWidth: 240 }}
            />
            <Button variant="primary" onClick={() => candidateBrands[0] && handleAddBrand(candidateBrands[0])}>
              {t('compare.addBrand')}
            </Button>
            <Button variant="secondary" onClick={handleClearAll}>
              {t('compare.clearAll')}
            </Button>
          </div>

          {candidateBrands.length > 0 && searchInput.trim() && (
            <div style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 6,
              padding: 8,
              background: 'var(--surface-secondary)',
              borderRadius: 8
            }}>
              {candidateBrands.slice(0, 10).map(name => (
                <Button
                  key={name}
                  size="sm"
                  variant={selectedBrands.includes(name) ? 'primary' : 'ghost'}
                  onClick={() => handleAddBrand(name)}
                  disabled={selectedBrands.includes(name)}
                >
                  + {name}
                </Button>
              ))}
            </div>
          )}

          {selectedBrands.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {selectedBrands.map((name, i) => (
                <span
                  key={name}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '4px 8px 4px 10px',
                    borderRadius: 999,
                    background: COLORS[i % COLORS.length] + '20',
                    color: COLORS[i % COLORS.length],
                    border: `1px solid ${COLORS[i % COLORS.length]}40`,
                    fontSize: 13,
                    fontWeight: 500
                  }}
                >
                  {name}
                  <button
                    type="button"
                    onClick={() => handleRemoveBrand(name)}
                    style={{
                      border: 'none',
                      background: 'transparent',
                      color: COLORS[i % COLORS.length],
                      cursor: 'pointer',
                      fontSize: 14,
                      lineHeight: 1,
                      padding: '0 2px'
                    }}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}

          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <Button
              variant="primary"
              onClick={handleCompare}
              loading={loading}
              disabled={selectedBrands.length < 2}
            >
              🔍 {t('compare.startCompare')}
            </Button>
            {loading && <span style={{ color: 'var(--text-tertiary)' }}>{t('compare.comparing')}</span>}
          </div>
        </div>
      </Card>

      {result && (
        <>
          <div className="kpi-grid" style={{ marginTop: 24 }}>
            {overallScores.map((os, i) => (
              <Kpi
                key={os.brand}
                label={os.brand}
                value={os.score}
                suffix="/100"
                icon={result.summary.overall_winner === os.brand ? '🏆' : '📊'}
                variant={i === 0 ? 'info' : i === 1 ? 'success' : i === 2 ? 'warning' : 'default'}
                footer={
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    {result.summary.strengths[os.brand]?.length > 0 && (
                      <div style={{ fontSize: 11, color: 'var(--status-success)' }}>
                        ✓ {t('compare.strengths')}: {result.summary.strengths[os.brand].slice(0, 2).join('、')}
                      </div>
                    )}
                    {result.summary.weaknesses[os.brand]?.length > 0 && (
                      <div style={{ fontSize: 11, color: 'var(--status-warning)' }}>
                        ⚠ {t('compare.weaknesses')}: {result.summary.weaknesses[os.brand].slice(0, 2).join('、')}
                      </div>
                    )}
                  </div>
                }
              />
            ))}
            {result.summary.overall_winner && (
              <Kpi
                label={t('compare.overallWinner')}
                value={result.summary.overall_winner}
                icon="👑"
                variant="success"
                footer={<span style={{ fontSize: 11 }}>{t('compare.generatedAt')}: {new Date(result.generated_at).toLocaleString()}</span>}
              />
            )}
          </div>

          <Card style={{ marginTop: 24 }}>
            <Tabs variant="underline" defaultActiveKey="radar">
              <TabPane tabKey="radar" tab={t('compare.tabs.radar')}>
                <div style={{ padding: '16px 0' }}>
                  <h3 style={{ margin: '0 0 16px', fontSize: 16 }}>{t('compare.radarTitle')}</h3>
                  <RadarChart data={result} />
                </div>
              </TabPane>

              <TabPane tabKey="diff" tab={t('compare.tabs.diff')}>
                <div style={{ padding: '16px 0' }}>
                  <h3 style={{ margin: '0 0 16px', fontSize: 16 }}>{t('compare.diffTitle')}</h3>
                  <Table
                    columns={diffColumns}
                    dataSource={result.diff_table as any[]}
                    rowKey="key"
                    striped
                  />
                </div>
              </TabPane>

              <TabPane tabKey="sideBySide" tab={t('compare.tabs.sideBySide')}>
                <div style={{ padding: '16px 0' }}>
                  <h3 style={{ margin: '0 0 16px', fontSize: 16 }}>{t('compare.sideBySideTitle')}</h3>
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: `repeat(${Math.min(result.brands.length, 4)}, 1fr)`,
                      gap: 16
                    }}
                  >
                    {result.brands.map((b, bi) => (
                      <Card
                        key={b}
                        title={b}
                        compact
                        style={{ borderColor: COLORS[bi % COLORS.length] + '40' }}
                      >
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                          {result.dimensions.map(d => {
                            const val = d.values[b] ?? 0
                            const max = Math.max(...Object.values(d.values))
                            const pct = Math.round((val / (max || 1)) * 100)
                            const isWinner = val === max
                            return (
                              <div key={d.key}>
                                <div style={{
                                  display: 'flex',
                                  justifyContent: 'space-between',
                                  fontSize: 12,
                                  marginBottom: 4
                                }}>
                                  <span style={{ color: 'var(--text-secondary)' }}>{d.label}</span>
                                  <strong style={isWinner ? { color: 'var(--status-success)' } : undefined}>
                                    {val} {isWinner && '🏆'}
                                  </strong>
                                </div>
                                <div style={{
                                  height: 6,
                                  borderRadius: 999,
                                  background: 'var(--bg-tertiary)',
                                  overflow: 'hidden'
                                }}>
                                  <div
                                    style={{
                                      height: '100%',
                                      width: `${pct}%`,
                                      background: isWinner ? COLORS[bi % COLORS.length] : 'var(--border-secondary)',
                                      borderRadius: 999,
                                      transition: 'width 300ms'
                                    }}
                                  />
                                </div>
                              </div>
                            )
                          })}
                        </div>
                      </Card>
                    ))}
                  </div>
                </div>
              </TabPane>
            </Tabs>
          </Card>
        </>
      )}
    </div>
  )
}

export default Compare

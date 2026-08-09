import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Kpi } from '@/components/Kpi'
import { Tabs, TabPane, Table, type TableColumn } from '@/components'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { VisibilityReport, BrandProfile } from '@/types/api'
import '../Dashboard/Dashboard.scss'

const gradeColor: Record<string, string> = {
  A: 'success', B: 'success', C: 'info', D: 'warning', F: 'error'
}

const BrandAudit: React.FC = () => {
  const { t } = useTranslation()
  const brands = useAppStore(s => s.brands)
  const currentBrand = useAppStore(s => s.currentBrand)
  const setLastReport = useAppStore(s => s.setLastReport)
  const showToast = useAppStore(s => s.showToast)

  const [selectedBrandName, setSelectedBrandName] = useState(currentBrand?.name || brands[0]?.name || '')
  const [loading, setLoading] = useState(false)
  const [report, setReport] = useState<VisibilityReport | null>(null)
  const [progress, setProgress] = useState('')

  const selectedBrand = brands.find(b => b.name === selectedBrandName)

  const runAudit = async () => {
    const brand: BrandProfile | undefined = selectedBrand
    if (!brand) return showToast('请先选择品牌', 'error')
    setLoading(true)
    setProgress('正在连接多个 AI 引擎...')
    try {
      const steps = [
        '查询品牌提及与引用...',
        '计算引擎级统计...',
        '分析内容缺口与竞品声量...',
        '生成 BVS 评分与行动建议...'
      ]
      let stepIdx = 0
      const timer = setInterval(() => {
        setProgress(steps[Math.min(stepIdx++, steps.length - 1)])
      }, 8000)
      const r = await api.brandAudit(brand)
      clearInterval(timer)
      setReport(r)
      setLastReport(r)
      showToast(`审计完成：BVS ${r.score.toFixed(1)} (${r.grade})`, 'success')
    } catch (e: any) {
      showToast(e.message || '审计失败', 'error')
    } finally {
      setLoading(false)
      setProgress('')
    }
  }

  const scoreBreakdown = report?.score_breakdown
  const weightMap: Record<string, number> = {
    content_quality: 0.23, technical_seo: 0.22, on_page_seo: 0.20,
    schema: 0.10, performance: 0.10, ai_readiness: 0.10, image_optimization: 0.05
  }
  const dimList = scoreBreakdown ? [
    { key: 'content_quality', name: t('brandAudit.contentQuality'), score: scoreBreakdown.content_quality, w: weightMap.content_quality },
    { key: 'technical_seo', name: t('brandAudit.technicalSeo'), score: scoreBreakdown.technical_seo, w: weightMap.technical_seo },
    { key: 'on_page_seo', name: t('brandAudit.onPageSeo'), score: scoreBreakdown.on_page_seo, w: weightMap.on_page_seo },
    { key: 'schema', name: t('brandAudit.schema'), score: scoreBreakdown.schema, w: weightMap.schema },
    { key: 'performance', name: t('brandAudit.performance'), score: scoreBreakdown.performance, w: weightMap.performance },
    { key: 'ai_readiness', name: t('brandAudit.aiReadiness'), score: scoreBreakdown.ai_readiness, w: weightMap.ai_readiness },
    { key: 'image_optimization', name: t('brandAudit.imageOptimization'), score: scoreBreakdown.image_optimization, w: weightMap.image_optimization }
  ] : []

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('brandAudit.title')}</h1>
          <p className="page-subtitle">{t('brandAudit.subtitle')}</p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <select
            value={selectedBrandName}
            onChange={(e) => setSelectedBrandName(e.target.value)}
            style={{
              padding: '8px 12px',
              borderRadius: 8,
              fontSize: 13,
              border: '1px solid var(--border-primary)',
              background: 'var(--surface-primary)',
              color: 'var(--text-primary)'
            }}
          >
            {brands.map(b => <option key={b.name} value={b.name}>{b.name}</option>)}
          </select>
          <Button onClick={runAudit} loading={loading}>
            🔍 {t('brandAudit.startAudit')}
          </Button>
        </div>
      </div>

      {loading && (
        <Card compact>
          <div style={{ padding: 32, textAlign: 'center' }}>
            <div style={{
              width: 48, height: 48, margin: '0 auto 16px',
              border: '4px solid var(--border-primary)',
              borderTopColor: 'var(--brand-primary)',
              borderRadius: '50%',
              animation: 'spin 0.8s linear infinite'
            }} />
            <div style={{ fontWeight: 600, marginBottom: 4 }}>{t('brandAudit.auditing')}</div>
            <div style={{ fontSize: 13, color: 'var(--text-tertiary)' }}>{progress || t('brandAudit.auditingHint')}</div>
          </div>
        </Card>
      )}

      {!report && !loading && (
        <Card compact>
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-tertiary)' }}>
            <div style={{ fontSize: 56, marginBottom: 16 }}>🔍</div>
            <div style={{ fontSize: 16, color: 'var(--text-primary)', marginBottom: 8 }}>
              选中品牌：<strong>{selectedBrandName || '未选择'}</strong>
            </div>
            <div>点击「开始审计」运行品牌可见度审计</div>
          </div>
        </Card>
      )}

      {report && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <Card
            title={
              <div>
                <div style={{ fontSize: 18, fontWeight: 700 }}>{report.brand_name}</div>
                <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                  {report.industry && report.industry + ' · '}{report.category} · 生成于 {new Date(report.generated_at).toLocaleString()}
                </div>
              </div>
            }
            actions={
              <div style={{ display: 'flex', gap: 6 }}>
                <Button size="sm" variant="secondary" onClick={() => showToast('就绪度审计：接口 /api/v1/brand/readiness', 'info')}>AI 就绪度</Button>
                <Button size="sm" variant="secondary" onClick={() => showToast('可爬取性审计：接口 /api/v1/brand/crawlability', 'info')}>可爬取性</Button>
                <Button size="sm" variant="primary" onClick={() => window.open(api.reportHtml(report.brand_name), '_blank')}>
                  📄 查看HTML报告
                </Button>
              </div>
            }
          >
            <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
              <Kpi
                label={t('brandAudit.bvsScore')}
                value={report.score.toFixed(1)}
                suffix="/100"
                icon="🎯"
                variant={(['A', 'B'].includes(report.grade) ? 'success' : ['C'].includes(report.grade) ? 'info' : ['D'].includes(report.grade) ? 'warning' : 'error') as any}
                footer={<span>等级：<strong className={`score-badge score-badge-${report.grade}`} style={{ padding: '4px 10px' }}>{report.grade}</strong></span>}
              />
              <Kpi
                label={t('brandAudit.tier')}
                value={t(`brandAudit.tiers.${report.tier}`)}
                icon="🏆"
                variant="info"
                footer={
                  <span>
                    实体完备度：{report.entity_completeness_score ? report.entity_completeness_score.toFixed(0) + '/100' : '-'}
                  </span>
                }
              />
              <Kpi
                label="品牌声量 (总提及)"
                value={report.engine_stats.reduce((s, e) => s + e.mention_count, 0)}
                icon="📣"
                variant="warning"
                footer={<span>正面情感率 {report.engine_stats.length ? Math.round(report.engine_stats.reduce((s, e) => s + e.positive_rate, 0) / report.engine_stats.length) : 0}%</span>}
              />
              <Kpi
                label="行动项"
                value={report.actions.length}
                suffix="项"
                icon="✅"
                variant="success"
                footer={
                  <span style={{ display: 'flex', gap: 6 }}>
                    <span style={{ color: 'var(--status-error)' }}>
                      高 {report.actions.filter(a => a.priority === 'high').length}
                    </span>
                    <span style={{ color: 'var(--status-warning)' }}>
                      中 {report.actions.filter(a => a.priority === 'medium').length}
                    </span>
                    <span style={{ color: 'var(--status-success)' }}>
                      低 {report.actions.filter(a => a.priority === 'low').length}
                    </span>
                  </span>
                }
              />
            </div>
          </Card>

          <Tabs>
            <TabPane tabKey="score" tab={t('brandAudit.scoreBreakdown')}>
              <Card compact>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                  <div>
                    <h4 style={{ marginBottom: 12 }}>7 维权重评分</h4>
                    {dimList.map(d => (
                      <div key={d.key} style={{ marginBottom: 12 }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 4 }}>
                          <span>
                            {d.name}
                            <span style={{ color: 'var(--text-tertiary)', fontSize: 11, marginLeft: 4 }}>
                              权重 {(d.w * 100).toFixed(0)}%
                            </span>
                          </span>
                          <strong>{d.score.toFixed(1)}/100</strong>
                        </div>
                        <div style={{
                          height: 8,
                          background: 'var(--bg-tertiary)',
                          borderRadius: 999,
                          overflow: 'hidden'
                        }}>
                          <div style={{
                            width: d.score + '%',
                            height: '100%',
                            borderRadius: 999,
                            background: `linear-gradient(90deg, ${d.score >= 80 ? 'var(--status-success)' : d.score >= 60 ? 'var(--status-warning)' : 'var(--status-error)'}, var(--brand-primary))`
                          }} />
                        </div>
                      </div>
                    ))}
                  </div>
                  <div>
                    <h4 style={{ marginBottom: 12 }}>引擎级统计</h4>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                      {report.engine_stats.map(es => (
                        <div key={es.engine} style={{
                          padding: 10, borderRadius: 8,
                          background: 'var(--surface-secondary)',
                          border: '1px solid var(--border-primary)'
                        }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                            <strong>{es.engine}</strong>
                            <span style={{ fontSize: 11, color: es.configured ? 'var(--status-success)' : 'var(--text-tertiary)' }}>
                              {es.configured ? '✓ 配置' : '未配置'}
                            </span>
                          </div>
                          <div style={{ fontSize: 12, display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 4, color: 'var(--text-secondary)' }}>
                            <span>提及率 {es.mention_rate.toFixed(0)}%</span>
                            <span>引用率 {es.citation_rate.toFixed(0)}%</span>
                            <span>SOV {es.share_of_voice.toFixed(1)}</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </Card>
            </TabPane>

            <TabPane tabKey="gaps" tab={t('brandAudit.contentGaps')} badge={report.content_gaps?.length ?? 0}>
              <Card compact>
                {(!report.content_gaps || report.content_gaps.length === 0) ? (
                  <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-tertiary)' }}>暂无内容缺口 🎉</div>
                ) : (
                  <Table
                    rowKey="prompt"
                    dataSource={report.content_gaps as any}
                    columns={[
                      { key: 'prompt', title: t('brandAudit.prompt'), dataIndex: 'prompt' as any },
                      { key: 'engine', title: t('brandAudit.engine'), dataIndex: 'engine' as any },
                      {
                        key: 'competitors',
                        title: t('brandAudit.competitorNamed'),
                        render: (r: any) => (r.competitor_named || []).map((c: string) => (
                          <span key={c} style={{
                            padding: '1px 6px', borderRadius: 999, fontSize: 11,
                            background: 'var(--status-error-bg)', color: 'var(--status-error)',
                            marginRight: 4
                          }}>{c}</span>
                        ))
                      },
                      { key: 'suggested', title: t('brandAudit.suggestedTopic'), dataIndex: 'suggested_topic' as any }
                    ]}
                  />
                )}
              </Card>
            </TabPane>

            <TabPane tabKey="sov" tab={t('brandAudit.competitorSOV')}>
              <Card compact>
                <Table
                  rowKey="name"
                  dataSource={[
                    { name: report.brand_name, mention_count: report.engine_stats.reduce((s, e) => s + e.mention_count, 0), sov: 100 / (1 + (report.competitor_sov?.length || 1)) },
                    ...(report.competitor_sov || [])
                  ]}
                  columns={[
                    { key: 'name', title: t('brandAudit.competitorName'), dataIndex: 'name' as any, sortable: true },
                    {
                      key: 'bar',
                      title: 'SOV 分布',
                      render: (r: any) => (
                        <div style={{
                          height: 8, width: 140,
                          background: 'var(--bg-tertiary)', borderRadius: 999, overflow: 'hidden'
                        }}>
                          <div style={{
                            width: Math.min(100, r.sov) + '%', height: '100%',
                            background: r.name === report.brand_name ? 'linear-gradient(90deg, var(--brand-primary), var(--brand-secondary))' : 'var(--bg-active)'
                          }} />
                        </div>
                      )
                    },
                    { key: 'mentions', title: t('brandAudit.mentionCount'), dataIndex: 'mention_count' as any, sortable: true },
                    {
                      key: 'sov',
                      title: t('brandAudit.sovPercent'),
                      render: (r: any) => <strong>{r.sov.toFixed(1)}%</strong>
                    }
                  ]}
                />
              </Card>
            </TabPane>

            <TabPane tabKey="actions" tab={t('brandAudit.actionItems')} badge={report.actions.length}>
              <Card compact>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {report.actions.map((a, i) => (
                    <div key={i} style={{
                      padding: 12, borderRadius: 8,
                      border: '1px solid var(--border-primary)',
                      background: 'var(--surface-secondary)'
                    }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                        <span style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                          <span style={{
                            padding: '2px 8px', borderRadius: 999, fontSize: 11, fontWeight: 600,
                            background: a.priority === 'high' ? 'var(--status-error-bg)' : a.priority === 'medium' ? 'var(--status-warning-bg)' : 'var(--status-success-bg)',
                            color: a.priority === 'high' ? 'var(--status-error)' : a.priority === 'medium' ? 'var(--status-warning)' : 'var(--status-success)'
                          }}>
                            {a.priority.toUpperCase()}
                          </span>
                          <span style={{
                            padding: '2px 8px', borderRadius: 4, fontSize: 11,
                            background: 'var(--bg-tertiary)', color: 'var(--text-secondary)'
                          }}>
                            {t(`brandAudit.categories.${a.category}`)}
                          </span>
                          <strong>{a.title}</strong>
                        </span>
                        {a.expected_impact && (
                          <span style={{ fontSize: 11, color: 'var(--status-success)' }}>
                            预期影响：{a.expected_impact}
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 6 }}>{a.detail}</div>
                      {a.tasks && a.tasks.length > 0 && (
                        <ul style={{ paddingLeft: 20, fontSize: 12, color: 'var(--text-primary)' }}>
                          {a.tasks.map((task, j) => <li key={j}>{task}</li>)}
                        </ul>
                      )}
                    </div>
                  ))}
                </div>
              </Card>
            </TabPane>

            <TabPane tabKey="results" tab={t('brandAudit.promptResults')}>
              <Card compact>
                <Table
                  rowKey={(r: any) => r.prompt + '|' + r.engine}
                  pagination
                  pageSize={5}
                  dataSource={(report.results || []).map(r => ({ ...r, sent: r.sentiment })) as any}
                  columns={[
                    { key: 'engine', title: t('brandAudit.engine'), dataIndex: 'engine' as any, width: 100 },
                    { key: 'prompt', title: t('brandAudit.prompt'), dataIndex: 'prompt' as any },
                    {
                      key: 'mentioned',
                      title: t('brandAudit.mentioned'),
                      align: 'center',
                      render: (r: any) => r.brand_mentioned
                        ? <span style={{ color: 'var(--status-success)' }}>✓ 是 (第{r.brand_position}段)</span>
                        : <span style={{ color: 'var(--text-tertiary)' }}>— 否</span>
                    },
                    {
                      key: 'cited',
                      title: t('brandAudit.cited'),
                      align: 'center',
                      render: (r: any) => (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, fontSize: 11 }}>
                          <span>{r.brand_cited ? <span style={{ color: 'var(--status-success)' }}>✓ 引用</span> : '— 未引用'}</span>
                          {r.ghost_citation && <span style={{ color: 'var(--status-warning)' }}>⚠️ 幽灵引用</span>}
                        </div>
                      )
                    },
                    {
                      key: 'sent',
                      title: t('brandAudit.sentiment'),
                      align: 'center',
                      render: (r: any) => (
                        <span style={{
                          padding: '2px 8px', borderRadius: 999, fontSize: 11,
                          background: r.sentiment === 'positive' ? 'var(--status-success-bg)' : r.sentiment === 'negative' ? 'var(--status-error-bg)' : 'var(--bg-tertiary)',
                          color: r.sentiment === 'positive' ? 'var(--status-success)' : r.sentiment === 'negative' ? 'var(--status-error)' : 'var(--text-secondary)'
                        }}>
                          {t(`brandAudit.sentiments.${r.sentiment}`)}
                        </span>
                      )
                    }
                  ]}
                />
              </Card>
            </TabPane>

            <TabPane tabKey="tools" tab="更多审计工具">
              <Card compact>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
                  {[
                    { icon: '🤖', k: 'readiness', name: t('brandAudit.readinessAudit'), desc: '8维 AI 就绪度 + CI 闸门', ep: '/api/v1/brand/readiness' },
                    { icon: '🕷️', k: 'crawlability', name: t('brandAudit.crawlabilityAudit'), desc: 'robots/llms.txt/JSON-LD/KG', ep: '/api/v1/brand/crawlability' },
                    { icon: '📍', k: 'local', name: t('brandAudit.localSeoAudit'), desc: 'GMB/点评/本地包/LBS引用', ep: '/api/v1/brand/localseo/audit' },
                    { icon: '📱', k: 'social', name: t('brandAudit.socialMonitor'), desc: 'Reddit/微博/YouTube 情感', ep: '/api/v1/brand/social/monitor' },
                    { icon: '🎤', k: 'kol', name: t('brandAudit.kolAnalyze'), desc: '权威媒体/KOL/作者挖掘', ep: '/api/v1/brand/kol/analyze' },
                    { icon: '🔗', k: 'top', name: t('brandAudit.topSourceAnalyze'), desc: '引用归因 Top Source 分析', ep: '/api/v1/brand/topsource/analyze' },
                    { icon: '🏷️', k: 'vertical', name: t('brandAudit.verticalDetect'), desc: '行业类型自动识别', ep: '/api/v1/brand/vertical/detect' },
                    { icon: '📡', k: 'external', name: t('brandAudit.externalSignals'), desc: 'DataForSEO / CommonCrawl', ep: '/api/v1/brand/externalsignals/report' },
                    { icon: '📈', k: 'drift', name: t('brandAudit.driftAnalysis'), desc: '两次审计之间的漂移对比', ep: '/api/v1/brand/drift' }
                  ].map(it => (
                    <div key={it.k} style={{
                      padding: 14, borderRadius: 8,
                      border: '1px solid var(--border-primary)',
                      background: 'var(--surface-secondary)'
                    }}>
                      <div style={{ fontSize: 22, marginBottom: 6 }}>{it.icon}</div>
                      <div style={{ fontWeight: 600, marginBottom: 4 }}>{it.name}</div>
                      <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 8 }}>{it.desc}</div>
                      <Button size="xs" variant="secondary" onClick={() => showToast(`调用 ${it.ep}（需要提供品牌/域名参数）`, 'info')}>
                        调用接口 →
                      </Button>
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

export default BrandAudit

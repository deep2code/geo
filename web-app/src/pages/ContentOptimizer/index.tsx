import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Input, Textarea } from '@/components/Input'
import { Button } from '@/components/Button'
import { Kpi } from '@/components/Kpi'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { OptimizationResponse, StrategyInfo } from '@/types/api'
import '../Dashboard/Dashboard.scss'

const engineOptions = [
  { v: 'chatgpt', l: 'ChatGPT' },
  { v: 'perplexity', l: 'Perplexity' },
  { v: 'gemini', l: 'Gemini' },
  { v: 'claude', l: 'Claude' },
  { v: 'qwen', l: '通义千问' },
  { v: 'deepseek', l: 'DeepSeek' },
  { v: 'kimi', l: 'Kimi' }
]

const ContentOptimizer: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)
  const setLastOptimization = useAppStore(s => s.setLastOptimization)

  const [content, setContent] = useState(
    `# CRM 软件的 5 大选择标准\n\n` +
    `中小企业在选择 CRM 软件时应关注以下 5 个方面：\n\n` +
    `## 1. 价格合理性\n根据 Gartner 2024 报告，中小企业 CRM 平均年费在 ¥5000-¥20000 之间。\n\n` +
    `## 2. 功能匹配度\n需要客户管理、商机跟进、数据分析三大核心模块。\n\n` +
    `## 3. 易用性\n界面友好，上手时间不超过 2 小时。\n\n` +
    `## 4. 集成能力\n支持钉钉、企微、邮件等现有工具对接。\n\n` +
    `## 5. 数据安全\nSaaS 模式需确认数据主权与合规性。`
  )
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [domainType, setDomainType] = useState('knowledge')
  const [selectedEngines, setSelectedEngines] = useState<string[]>([])
  const [result, setResult] = useState<OptimizationResponse | null>(null)
  const [strategies, setStrategies] = useState<StrategyInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [analyzing, setAnalyzing] = useState(false)

  const loadStrategies = async () => {
    if (strategies.length > 0) return
    try {
      const r = await api.strategies()
      setStrategies(r.strategies)
    } catch (e) {}
  }

  const handleAnalyze = async () => {
    if (!content.trim()) return showToast('请输入内容', 'error')
    setAnalyzing(true)
    try {
      const r = await api.analyze(content)
      showToast(`分析完成：字数 ${r.word_count}，常青度 ${r.evergreen_score}/100`, 'success')
    } catch (e: any) {
      showToast(e.message || '分析失败', 'error')
    } finally {
      setAnalyzing(false)
    }
  }

  const handleOptimize = async () => {
    if (!content.trim()) return showToast('请输入内容', 'error')
    setLoading(true)
    try {
      await loadStrategies()
      const r = await api.optimize({
        content,
        url,
        title,
        target_engines: selectedEngines.length ? selectedEngines : undefined,
        domain_type: domainType
      })
      setResult(r)
      setLastOptimization(r)
      const diff = (r.score_after - r.score_before).toFixed(1)
      showToast(`优化完成！评分 ${r.score_before} → ${r.score_after}（+${diff}）`, 'success')
    } catch (e: any) {
      showToast(e.message || '优化失败', 'error')
    } finally {
      setLoading(false)
    }
  }

  const handleCopyResult = async () => {
    if (!result) return
    try {
      await navigator.clipboard.writeText(result.optimized_content)
      showToast('已复制优化结果', 'success')
    } catch {
      showToast('复制失败', 'error')
    }
  }

  const toggleEngine = (v: string) => {
    setSelectedEngines(prev => prev.includes(v) ? prev.filter(x => x !== v) : [...prev, v])
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('contentOptimizer.title')}</h1>
          <p className="page-subtitle">{t('contentOptimizer.subtitle')}</p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
        <Card title="输入区" compact>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Input
                label={t('contentOptimizer.inputTitle')}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="内容标题"
              />
              <Input
                label={t('contentOptimizer.inputUrl')}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://..."
              />
            </div>
            <div className="form-field">
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                {t('contentOptimizer.inputDomain')}
              </label>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                {[
                  { v: 'serious', l: t('contentOptimizer.domains.serious') },
                  { v: 'soft', l: t('contentOptimizer.domains.soft') },
                  { v: 'knowledge', l: t('contentOptimizer.domains.knowledge') }
                ].map(d => (
                  <button
                    key={d.v}
                    type="button"
                    onClick={() => setDomainType(d.v)}
                    style={{
                      padding: '6px 12px',
                      borderRadius: 8,
                      fontSize: 13,
                      border: domainType === d.v ? '1px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: domainType === d.v ? 'color-mix(in srgb, var(--brand-primary) 10%, var(--surface-primary))' : 'var(--surface-secondary)',
                      color: domainType === d.v ? 'var(--brand-primary)' : 'var(--text-primary)',
                      cursor: 'pointer'
                    }}
                  >
                    {d.l}
                  </button>
                ))}
              </div>
            </div>
            <div className="form-field">
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                {t('contentOptimizer.targetEngines')}
                <span style={{ fontWeight: 400, color: 'var(--text-tertiary)', marginLeft: 6, fontSize: 12 }}>
                  {t('contentOptimizer.strategiesHint')}
                </span>
              </label>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {engineOptions.map(e => (
                  <button
                    key={e.v}
                    type="button"
                    onClick={() => toggleEngine(e.v)}
                    style={{
                      padding: '4px 10px',
                      borderRadius: 999,
                      fontSize: 12,
                      border: selectedEngines.includes(e.v) ? '1px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: selectedEngines.includes(e.v) ? 'var(--brand-primary)' : 'var(--surface-primary)',
                      color: selectedEngines.includes(e.v) ? 'white' : 'var(--text-primary)',
                      cursor: 'pointer'
                    }}
                  >
                    {e.l}
                  </button>
                ))}
              </div>
            </div>
            <Textarea
              label="内容"
              required
              rows={16}
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder={t('contentOptimizer.inputPlaceholder')}
            />
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <Button variant="secondary" onClick={handleAnalyze} loading={analyzing}>
                🔍 {t('contentOptimizer.analyzeButton')}
              </Button>
              <Button onClick={handleOptimize} loading={loading}>
                ⚡ {t('contentOptimizer.optimizeButton')}
              </Button>
            </div>
          </div>
        </Card>

        <Card
          title={result ? '优化结果' : '优化评分预览'}
          actions={result && (
            <div style={{ display: 'flex', gap: 8 }}>
              <Button size="sm" variant="secondary" onClick={handleCopyResult}>📋 {t('contentOptimizer.copyResult')}</Button>
              <Button size="sm" variant="outline">⬇️ {t('contentOptimizer.downloadResult')}</Button>
            </div>
          )}
          compact
        >
          {!result ? (
            <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-tertiary)' }}>
              <div style={{ fontSize: 48, marginBottom: 16 }}>✨</div>
              <div>点击「开始优化」生成 GEO 优化结果</div>
              <div style={{ fontSize: 12, marginTop: 8 }}>内容级别：{content.length} 字符</div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(2, 1fr)' }}>
                <Kpi
                  label={t('contentOptimizer.scoreBefore')}
                  value={result.score_before.toFixed(1)}
                  suffix="/100"
                  icon="📉"
                  variant="warning"
                />
                <Kpi
                  label={t('contentOptimizer.scoreAfter')}
                  value={result.score_after.toFixed(1)}
                  suffix="/100"
                  icon="📈"
                  variant="success"
                  trendValue={`+${(result.score_after - result.score_before).toFixed(1)}`}
                  trendDirection="up"
                />
              </div>

              <div>
                <h4 style={{ marginBottom: 8 }}>{t('contentOptimizer.appliedStrategies')}</h4>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {result.applied_strategies.filter(s => s.applied).map(s => (
                    <span key={s.strategy} style={{
                      padding: '3px 8px',
                      borderRadius: 999,
                      fontSize: 12,
                      background: 'var(--status-success-bg)',
                      color: 'var(--status-success)'
                    }}>
                      +{s.improvement.toFixed(1)} {s.strategy.replace(/_/g, ' ')}
                    </span>
                  ))}
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <div style={{ padding: 12, borderRadius: 8, background: 'var(--surface-secondary)' }}>
                  <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 4 }}>{t('contentOptimizer.geoScore')}</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{result.geo_score.overall_score.toFixed(1)}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginTop: 4 }}>
                    引用 {result.geo_score.citation_frequency} 次 · 位置 {result.geo_score.position_score.toFixed(1)}
                  </div>
                </div>
                <div style={{ padding: 12, borderRadius: 8, background: 'var(--surface-secondary)' }}>
                  <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 4 }}>{t('contentOptimizer.utilityScore')}</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{result.utility_score.overall_score.toFixed(1)}</div>
                  <div style={{ fontSize: 11, color: 'var(--text-tertiary)', marginTop: 4 }}>
                    引用质量 {result.utility_score.citation_quality.toFixed(1)} · 覆盖 {result.utility_score.keypoint_coverage.toFixed(1)}
                  </div>
                </div>
              </div>

              {result.recommendations && result.recommendations.length > 0 && (
                <div>
                  <h4 style={{ marginBottom: 8 }}>{t('contentOptimizer.recommendations')}</h4>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {result.recommendations.slice(0, 5).map((r, i) => (
                      <div key={i} style={{
                        padding: 8,
                        borderRadius: 6,
                        fontSize: 13,
                        background: r.priority === 'high' ? 'var(--status-error-bg)' : r.priority === 'medium' ? 'var(--status-warning-bg)' : 'var(--bg-tertiary)',
                        color: 'var(--text-primary)',
                        display: 'flex',
                        gap: 8
                      }}>
                        <span style={{ flexShrink: 0 }}>
                          {r.priority === 'high' ? '🔴' : r.priority === 'medium' ? '🟡' : '🟢'}
                        </span>
                        <span>[{r.category}] {r.message}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <div>
                <h4 style={{ marginBottom: 8 }}>{t('contentOptimizer.copyResult')}</h4>
                <textarea
                  readOnly
                  value={result.optimized_content}
                  style={{
                    width: '100%',
                    minHeight: 180,
                    padding: 12,
                    borderRadius: 8,
                    border: '1px solid var(--border-primary)',
                    background: 'var(--surface-secondary)',
                    fontFamily: 'var(--font-family-mono)',
                    fontSize: 13,
                    resize: 'vertical'
                  }}
                />
              </div>

              {result.generated_assets && (
                <div>
                  <h4 style={{ marginBottom: 8 }}>{t('contentOptimizer.generatedAssets')}</h4>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {result.generated_assets.json_ld && (
                      <details style={{ padding: 8, borderRadius: 6, background: 'var(--surface-secondary)' }}>
                        <summary style={{ cursor: 'pointer', fontSize: 13 }}>📄 JSON-LD 结构化数据</summary>
                        <pre style={{ marginTop: 8, padding: 8, overflow: 'auto', fontSize: 12 }}>
                          {result.generated_assets.json_ld}
                        </pre>
                      </details>
                    )}
                    {result.generated_assets.llms_txt && (
                      <details style={{ padding: 8, borderRadius: 6, background: 'var(--surface-secondary)' }}>
                        <summary style={{ cursor: 'pointer', fontSize: 13 }}>📄 LLMs.txt</summary>
                        <pre style={{ marginTop: 8, padding: 8, overflow: 'auto', fontSize: 12 }}>
                          {result.generated_assets.llms_txt}
                        </pre>
                      </details>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}
        </Card>
      </div>
    </div>
  )
}

export default ContentOptimizer

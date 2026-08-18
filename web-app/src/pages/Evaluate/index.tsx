import React, { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/services/api'
import type { EvaluateResponse } from '@/types/api'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import './Evaluate.scss'

/**
 * GEO 评测（替代原 `geo evaluate` CLI）。
 * 上传/粘贴评测集 JSON，运行改前/改后引用率与评分对比，产出可复现报告。
 */
const Evaluate: React.FC = () => {
  const { t } = useTranslation()
  const [dataset, setDataset] = useState('')
  const [format, setFormat] = useState<'md' | 'json'>('md')
  const [live, setLive] = useState(false)
  const [llmKey, setLlmKey] = useState('')
  const [llmBase, setLlmBase] = useState('')
  const [llmModel, setLlmModel] = useState('')
  const [rules, setRules] = useState('')

  const [running, setRunning] = useState(false)
  const [report, setReport] = useState<EvaluateResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const onFile = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f) return
    const reader = new FileReader()
    reader.onload = () => setDataset(String(reader.result || ''))
    reader.readAsText(f)
  }, [])

  const run = useCallback(async () => {
    if (!dataset.trim()) {
      setError(t('evaluate.needDataset', '请提供评测集 JSON（粘贴或上传文件）'))
      return
    }
    setRunning(true)
    setReport(null)
    setError(null)
    try {
      const res = await api.evaluate.run({
        dataset,
        format,
        live: live || undefined,
        llm_key: llmKey || undefined,
        llm_base: llmBase || undefined,
        llm_model: llmModel || undefined,
        rules: rules.trim() || undefined
      })
      setReport(res)
    } catch (err: any) {
      setError(err?.message || t('evaluate.failed', '评测运行失败'))
    } finally {
      setRunning(false)
    }
  }, [dataset, format, live, llmKey, llmBase, llmModel, rules, t])

  return (
    <div className="evaluate-page">
      <div className="evaluate-head">
        <div>
          <h1 className="evaluate-title">📊 {t('evaluate.title', 'GEO 评测')}</h1>
          <p className="evaluate-sub">{t('evaluate.subtitle', '运行评测集，产出改前/改后引用率与评分对比报告（替代命令行 geo evaluate）。')}</p>
        </div>
      </div>

      <Card>
        <h3 className="evaluate-section-title">📝 {t('evaluate.dataset', '评测集')}</h3>
        <p className="evaluate-hint">{t('evaluate.datasetHint', '粘贴评测集 JSON，或上传 .json 文件。每个用例需含 target_content（待优化内容）。')}</p>
        <textarea
          className="evaluate-textarea"
          placeholder='{ "name": "zh-geo-sample", "cases": [ { "query": "...", "target_url": "https://...", "target_content": "待优化内容…", "engines": ["chatgpt"], "expected_citations": ["来源A"] } ] }'
          value={dataset}
          onChange={(e) => setDataset(e.target.value)}
          rows={10}
        />
        <label className="evaluate-upload">
          📎 {t('evaluate.upload', '上传评测集 .json')}
          <input type="file" accept="application/json,.json" onChange={onFile} hidden />
        </label>

        <div className="evaluate-options">
          <div className="evaluate-opt">
            <span className="evaluate-opt-label">{t('evaluate.format', '输出格式')}</span>
            <div className="evaluate-seg">
              <button className={format === 'md' ? 'on' : ''} onClick={() => setFormat('md')}>Markdown</button>
              <button className={format === 'json' ? 'on' : ''} onClick={() => setFormat('json')}>JSON</button>
            </div>
          </div>

          <label className="evaluate-live">
            <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} />
            <span>{t('evaluate.live', '接入真实引擎实测引用（live）')}</span>
          </label>
        </div>

        {live && (
          <div className="evaluate-live-fields">
            <input className="evaluate-input" placeholder="LLM API Key" value={llmKey} onChange={(e) => setLlmKey(e.target.value)} />
            <input className="evaluate-input" placeholder="LLM Base URL（默认 https://api.openai.com/v1）" value={llmBase} onChange={(e) => setLlmBase(e.target.value)} />
            <input className="evaluate-input" placeholder="LLM Model（默认 gpt-4o-mini）" value={llmModel} onChange={(e) => setLlmModel(e.target.value)} />
          </div>
        )}

        <div className="evaluate-rules">
          <span className="evaluate-opt-label">{t('evaluate.rules', '规则集（可选，路径或 JSON）')}</span>
          <input className="evaluate-input" placeholder="GEO_RULES 路径或规则集 JSON" value={rules} onChange={(e) => setRules(e.target.value)} />
        </div>

        <div className="evaluate-actions">
          <Button variant="primary" size="md" loading={running} onClick={run}>
            {running ? t('evaluate.running', '评测中…') : `▶ ${t('evaluate.run', '运行评测')}`}
          </Button>
        </div>
        {error && <div className="evaluate-error">{error}</div>}
      </Card>

      {report && (
        <Card>
          <div className="evaluate-report-head">
            <h3 className="evaluate-section-title">📄 {t('evaluate.report', '评测报告')}</h3>
            <span className="evaluate-mode">mode: {report.mode} · {report.format}</span>
          </div>
          <pre className="evaluate-report">{report.report}</pre>
        </Card>
      )}
    </div>
  )
}

export default Evaluate

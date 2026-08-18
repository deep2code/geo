import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/services/api'
import type { RulesListResponse, RuleSet, RuleSetValidateResponse, RuleSetListItem } from '@/types/api'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import './Rules.scss'

/**
 * 规则集管理（替代原 `geo rules` CLI）。
 * - 列出可用规则集（内置默认 + config/rules/*.json）；
 * - 查看内置默认规则集 JSON；
 * - 粘贴/上传规则集 JSON 并校验。
 */
const Rules: React.FC = () => {
  const { t } = useTranslation()
  const [list, setList] = useState<RuleSetListItem[]>([])
  const [listError, setListError] = useState<string | null>(null)

  const [defaultRS, setDefaultRS] = useState<RuleSet | null>(null)
  const [showDefault, setShowDefault] = useState(false)

  const [content, setContent] = useState('')
  const [validating, setValidating] = useState(false)
  const [result, setResult] = useState<RuleSetValidateResponse | null>(null)
  const [resultError, setResultError] = useState<string | null>(null)

  const loadList = useCallback(async () => {
    try {
      const data = await api.rules.list()
      setList(data.rulesets || [])
      setListError(null)
    } catch (err: any) {
      setListError(err?.message || '加载失败')
    }
  }, [])

  useEffect(() => {
    loadList()
  }, [loadList])

  const viewDefault = useCallback(async () => {
    try {
      const rs = await api.rules.default()
      setDefaultRS(rs)
      setShowDefault(true)
    } catch (err: any) {
      setListError(err?.message || '加载默认规则集失败')
    }
  }, [])

  const onFile = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (!f) return
    const reader = new FileReader()
    reader.onload = () => setContent(String(reader.result || ''))
    reader.readAsText(f)
  }, [])

  const validate = useCallback(async () => {
    if (!content.trim()) {
      setResultError('请粘贴规则集 JSON 或上传文件')
      return
    }
    setValidating(true)
    setResult(null)
    setResultError(null)
    try {
      const res = await api.rules.validate({ content })
      setResult(res)
    } catch (err: any) {
      setResultError(err?.message || '校验请求失败')
    } finally {
      setValidating(false)
    }
  }, [content])

  return (
    <div className="rules-page">
      <div className="rules-head">
        <div>
          <h1 className="rules-title">⚙️ {t('rules.title', '规则集管理')}</h1>
          <p className="rules-sub">{t('rules.subtitle', '查看、校验外部化评分规则集（评分权重 + 策略效果系数），替代原命令行 geo rules。')}</p>
        </div>
        <Button variant="secondary" size="md" onClick={viewDefault}>查看默认规则集</Button>
      </div>

      <Card>
        <h3 className="rules-section-title">📋 {t('rules.available', '可用规则集')}</h3>
        {listError ? (
          <div className="rules-error">{listError}</div>
        ) : (
          <table className="rules-table">
            <thead>
              <tr><th>名称</th><th>版本</th><th>来源</th><th>状态</th></tr>
            </thead>
            <tbody>
              {list.map((r) => (
                <tr key={r.source + r.name}>
                  <td>{r.name}</td>
                  <td>{r.version}</td>
                  <td>{r.source}</td>
                  <td>
                    {r.valid ? (
                      <span className="rules-badge ok">✓ 有效</span>
                    ) : (
                      <span className="rules-badge err" title={r.error}>✗ 无效</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {showDefault && defaultRS && (
        <Card>
          <div className="rules-default-head">
            <h3 className="rules-section-title">🔍 {t('rules.default', '默认规则集 JSON')}</h3>
            <Button variant="ghost" size="sm" onClick={() => setShowDefault(false)}>收起</Button>
          </div>
          <pre className="rules-json">{JSON.stringify(defaultRS, null, 2)}</pre>
        </Card>
      )}

      <Card>
        <h3 className="rules-section-title">✅ {t('rules.validate', '校验规则集')}</h3>
        <p className="rules-hint">{t('rules.validateHint', '粘贴规则集 JSON，或从 config/rules 上传文件后点校验。')}</p>
        <textarea
          className="rules-textarea"
          placeholder='{ "name": "zh-ecom", "version": "1.0.0", "weights": { ... }, "strategy_effectiveness": { ... } }'
          value={content}
          onChange={(e) => setContent(e.target.value)}
          rows={12}
        />
        <div className="rules-actions">
          <label className="rules-upload">
            📎 {t('rules.upload', '上传 .json 文件')}
            <input type="file" accept="application/json,.json" onChange={onFile} hidden />
          </label>
          <Button variant="primary" size="md" loading={validating} onClick={validate}>
            {t('rules.validateBtn', '校验')}
          </Button>
        </div>

        {resultError && <div className="rules-error">{resultError}</div>}
        {result && (
          <div className={`rules-result ${result.valid ? 'ok' : 'err'}`}>
            {result.valid ? (
              <div>
                <div className="rules-result-title">✓ {t('rules.valid', '校验通过')}{result.name ? `：${result.name} v${result.version}` : ''}</div>
                <div className="rules-result-meta">
                  权重条目 {result.weights ?? 0} · 策略系数 {result.strategy_effectiveness ?? 0} · 触发条件 {result.strategy_triggers ?? 0}
                  {result.engine ? ` · 适用引擎 ${result.engine}` : ''}
                  {result.domain ? ` · 领域 ${result.domain}` : ''}
                </div>
              </div>
            ) : (
              <div>
                <div className="rules-result-title">✗ {t('rules.invalid', '校验未通过')}{result.name ? `：${result.name}` : ''}</div>
                <div className="rules-result-meta">{result.error}</div>
              </div>
            )}
          </div>
        )}
      </Card>
    </div>
  )
}

export default Rules

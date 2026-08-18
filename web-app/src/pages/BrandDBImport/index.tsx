import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/services/api'
import type { OfflineDBStats, OfflineDBImportResult, OfflineDBImportGitHubRequest } from '@/types/api'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import './BrandDBImport.scss'

/**
 * 离线工商库导入（替代原 `geo brand db import-*` CLI）。
 * - 上传本地 JSON 文件（JSON 数组 / JSONL 自动识别）导入；
 * - 或直连 GitHub 按年份+省份下载并导入。
 * 需管理员权限（X-Admin-Key）。
 */
const BrandDBImport: React.FC = () => {
  const { t } = useTranslation()
  const [tab, setTab] = useState<'upload' | 'github'>('upload')
  const [stats, setStats] = useState<OfflineDBStats | null>(null)
  const [statsError, setStatsError] = useState<string | null>(null)

  const [file, setFile] = useState<File | null>(null)
  const [importing, setImporting] = useState(false)
  const [result, setResult] = useState<OfflineDBImportResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [years, setYears] = useState('2019')
  const [provinces, setProvinces] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [timeoutSec, setTimeoutSec] = useState(900)

  const loadStats = useCallback(async () => {
    try {
      const s = await api.offlinedb.stats()
      setStats(s)
      setStatsError(null)
    } catch (err: any) {
      setStatsError(err?.message || '加载统计失败')
    }
  }, [])

  useEffect(() => {
    loadStats()
  }, [loadStats])

  const doUpload = useCallback(async () => {
    if (!file) {
      setError(t('branddb.needFile', '请先选择 JSON 文件'))
      return
    }
    setImporting(true)
    setResult(null)
    setError(null)
    try {
      const res = await api.offlinedb.importFile(file)
      setResult(res)
      loadStats()
    } catch (err: any) {
      setError(err?.message || t('branddb.failed', '导入失败'))
    } finally {
      setImporting(false)
    }
  }, [file, t, loadStats])

  const doGitHub = useCallback(async () => {
    if (!provinces.trim()) {
      setError(t('branddb.needProvinces', '请填写 provinces（逗号分隔）'))
      return
    }
    const payload: OfflineDBImportGitHubRequest = {
      years: years.trim(),
      provinces: provinces.trim(),
      timeout_seconds: timeoutSec
    }
    if (baseURL.trim()) payload.base_url = baseURL.trim()
    setImporting(true)
    setResult(null)
    setError(null)
    try {
      const res = await api.offlinedb.importGitHub(payload)
      setResult(res)
      loadStats()
    } catch (err: any) {
      setError(err?.message || t('branddb.failed', '导入失败'))
    } finally {
      setImporting(false)
    }
  }, [years, provinces, baseURL, timeoutSec, t, loadStats])

  const fmtMB = (n?: number) => (n ? `${(n / 1024 / 1024).toFixed(1)} MB` : '-')

  return (
    <div className="branddb-page">
      <div className="branddb-head">
        <div>
          <h1 className="branddb-title">🗄️ {t('branddb.title', '离线工商库导入')}</h1>
          <p className="branddb-sub">{t('branddb.subtitle', '将 1978-2019 工商注册数据导入 MySQL（替代命令行 geo brand db import-*）。需管理员权限。')}</p>
        </div>
        <Button variant="secondary" size="md" onClick={loadStats}>刷新统计</Button>
      </div>

      <Card>
        <h3 className="branddb-section-title">📈 {t('branddb.current', '当前库状态')}</h3>
        {statsError ? (
          <div className="branddb-error">{statsError}</div>
        ) : stats ? (
          <div className="branddb-stats">
            <div className="branddb-stat"><span>{stats.count.toLocaleString()}</span><label>记录数</label></div>
            <div className="branddb-stat"><span>{stats.backend || '-'}</span><label>后端</label></div>
            <div className="branddb-stat"><span>{fmtMB(stats.file_size_bytes)}</span><label>文件大小</label></div>
          </div>
        ) : (
          <div className="branddb-error">…</div>
        )}
      </Card>

      <Card>
        <div className="branddb-tabs">
          <button className={tab === 'upload' ? 'on' : ''} onClick={() => setTab('upload')}>{t('branddb.tabUpload', '上传文件')}</button>
          <button className={tab === 'github' ? 'on' : ''} onClick={() => setTab('github')}>{t('branddb.tabGithub', 'GitHub 直连')}</button>
        </div>

        {tab === 'upload' ? (
          <div className="branddb-upload">
            <p className="branddb-hint">{t('branddb.uploadHint', '支持单个 JSON 文件（JSON 数组 / JSON 对象包数组 / JSONL 自动识别）。')}</p>
            <input type="file" accept="application/json,.json" onChange={(e) => setFile(e.target.files?.[0] || null)} />
            <div className="branddb-actions">
              <Button variant="primary" size="md" loading={importing} onClick={doUpload} disabled={!file}>
                {importing ? t('branddb.importing', '导入中…') : `⬆ ${t('branddb.import', '导入')}`}
              </Button>
            </div>
          </div>
        ) : (
          <div className="branddb-github">
            <p className="branddb-hint">{t('branddb.githubHint', '按年份+省份从 GitHub raw 下载并导入（适合小样本）。大批量建议本地 clone 后上传文件。')}</p>
            <label className="branddb-field"><span>years</span>
              <input value={years} onChange={(e) => setYears(e.target.value)} placeholder="2018,2019" />
            </label>
            <label className="branddb-field"><span>provinces</span>
              <input value={provinces} onChange={(e) => setProvinces(e.target.value)} placeholder="广东,北京,上海" />
            </label>
            <label className="branddb-field"><span>base_url（可选）</span>
              <input value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder="https://raw.githubusercontent.com/.../json" />
            </label>
            <label className="branddb-field"><span>timeout（秒）</span>
              <input type="number" value={timeoutSec} onChange={(e) => setTimeoutSec(Number(e.target.value) || 900)} />
            </label>
            <div className="branddb-actions">
              <Button variant="primary" size="md" loading={importing} onClick={doGitHub}>
                {importing ? t('branddb.importing', '导入中…') : `⬇ ${t('branddb.importGithub', '下载并导入')}`}
              </Button>
            </div>
          </div>
        )}

        {error && <div className="branddb-error">{error}</div>}
        {result && (
          <div className="branddb-result">
            <div className="branddb-result-title">✓ {t('branddb.done', '导入完成')}</div>
            <div className="branddb-result-meta">
              新增 {result.imported.toLocaleString()} · 跳过 {result.skipped.toLocaleString()}（重复） · 失败 {result.failed.toLocaleString()} · 文件 {result.files} · 当前库 {result.db_count.toLocaleString()} 条
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}

export default BrandDBImport

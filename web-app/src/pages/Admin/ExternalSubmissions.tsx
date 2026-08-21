import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Table, type TableColumn } from '@/components/Table'
import api from '@/services/api'
import type { ExternalSubmission } from '@/types/api'
import { useAppStore } from '@/store/useAppStore'

const STATUS_COLORS: Record<string, { bg: string; color: string }> = {
  pending: { bg: 'var(--status-warning-bg)', color: 'var(--status-warning)' },
  analyzed: { bg: 'var(--status-success-bg)', color: 'var(--status-success)' },
  failed: { bg: 'var(--status-danger-bg)', color: 'var(--status-danger)' }
}

const SENTIMENT_LABELS: Record<string, string> = {
  positive: '正面',
  neutral: '中性',
  negative: '负面'
}

const ExternalSubmissions: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)
  const [items, setItems] = useState<ExternalSubmission[]>([])
  const [total, setTotal] = useState(0)
  const [pending, setPending] = useState(0)
  const [analyzed, setAnalyzed] = useState(0)
  const [statusFilter, setStatusFilter] = useState('')
  const [loading, setLoading] = useState(false)
  const [triggering, setTriggering] = useState(false)
  const [detail, setDetail] = useState<ExternalSubmission | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.admin.externalSubmissions({ status: statusFilter || undefined, limit: 100 })
      setItems(res.items || [])
      setTotal(res.total || 0)
      setPending(res.pending || 0)
      setAnalyzed(res.analyzed || 0)
    } catch {
      // 后台未启用或网络异常时保持空列表
      setItems([])
      setTotal(0)
      setPending(0)
      setAnalyzed(0)
    } finally {
      setLoading(false)
    }
  }, [statusFilter])

  useEffect(() => { load() }, [load])

  const handleTrigger = async () => {
    setTriggering(true)
    try {
      const res = await api.admin.externalSubmissionsTrigger()
      const n = res?.processed ?? 0
      const msg = t('extSubTriggerDone').replace('{n}', String(n))
      showToast(msg, 'success')
      load()
    } catch {
      showToast(t('extSubError'), 'error')
    } finally {
      setTriggering(false)
    }
  }

  const renderStatus = (s: string) => {
    const c = STATUS_COLORS[s] || STATUS_COLORS.pending
    const label = s === 'analyzed' ? t('extSubStatusAnalyzed') : s === 'failed' ? t('extSubStatusFailed') : t('extSubStatusPending')
    return (
      <span style={{
        fontSize: 11, padding: '2px 8px', borderRadius: 999,
        background: c.bg, color: c.color, fontWeight: 600
      }}>{label}</span>
    )
  }

  const columns: TableColumn<ExternalSubmission>[] = [
    {
      key: 'model_name',
      title: t('extSubModel'),
      width: 130,
      render: (r) => <strong style={{ fontSize: 13 }}>{r.model_name || '-'}</strong>
    },
    {
      key: 'question',
      title: t('extSubQuestion'),
      render: (r) => (
        <div style={{ maxWidth: 280, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--text-primary)' }}>
          {r.question || '-'}
        </div>
      )
    },
    {
      key: 'status',
      title: t('extSubStatus'),
      width: 90,
      render: (r) => renderStatus(r.status)
    },
    {
      key: 'sentiment',
      title: t('extSubSentiment'),
      width: 80,
      render: (r) => (r.sentiment ? (SENTIMENT_LABELS[r.sentiment] || r.sentiment) : '-')
    },
    {
      key: 'category',
      title: t('extSubCategory'),
      width: 120,
      render: (r) => (r.category ? <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{r.category}</span> : '-')
    },
    {
      key: 'source_domains',
      title: t('extSubSources'),
      width: 160,
      render: (r) => {
        const ds = r.source_domains || []
        if (ds.length === 0) return <span style={{ color: 'var(--text-tertiary)' }}>-</span>
        return (
          <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
            {ds.slice(0, 3).map((d, i) => <span key={d} style={{ marginRight: 6 }}>{d}</span>)}
            {ds.length > 3 && <span style={{ color: 'var(--text-tertiary)' }}>+{ds.length - 3}</span>}
          </span>
        )
      }
    },
    {
      key: 'created_at',
      title: t('extSubCreatedAt'),
      width: 150,
      render: (r) => <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>{new Date(r.created_at * 1000).toLocaleString()}</span>
    },
    {
      key: 'action',
      title: '',
      width: 80,
      align: 'right',
      render: (r) => (
        <Button size="xs" variant="secondary" onClick={() => setDetail(r)}>{t('extSubDetail')}</Button>
      )
    }
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {/* 统计卡片 */}
      <div className="admin-stats-grid">
        <div className="admin-stat-card">
          <div className="admin-stat-icon">📥</div>
          <div className="admin-stat-label">{t('extSubTotal')}</div>
          <div className="admin-stat-value">{total}</div>
        </div>
        <div className="admin-stat-card">
          <div className="admin-stat-icon">⏳</div>
          <div className="admin-stat-label">{t('extSubPending')}</div>
          <div className="admin-stat-value">{pending}</div>
        </div>
        <div className="admin-stat-card">
          <div className="admin-stat-icon">✅</div>
          <div className="admin-stat-label">{t('extSubAnalyzed')}</div>
          <div className="admin-stat-value">{analyzed}</div>
        </div>
      </div>

      {/* 过滤 + 操作 */}
      <div className="admin-filter-bar">
        <div className="admin-filter-item">
          <label>{t('extSubStatus')}</label>
          <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
            <option value="">{t('common.all')}</option>
            <option value="pending">{t('extSubStatusPending')}</option>
            <option value="analyzed">{t('extSubStatusAnalyzed')}</option>
            <option value="failed">{t('extSubStatusFailed')}</option>
          </select>
        </div>
        <Button variant="secondary" size="sm" onClick={load}>🔄 {t('common.refresh')}</Button>
        <Button size="sm" variant="primary" loading={triggering} onClick={handleTrigger}>⚡ {t('extSubTrigger')}</Button>
      </div>

      <Card compact>
        <Table
          columns={columns}
          dataSource={items}
          rowKey="id"
          striped
          pagination
          pageSize={10}
          emptyText={t('extSubNoData')}
        />
      </Card>

      {/* 详情弹窗 */}
      {detail && (
        <div
          onClick={() => setDetail(null)}
          style={{
            position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)',
            display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000, padding: 20
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              background: 'var(--surface-primary)', color: 'var(--text-primary)',
              borderRadius: 12, maxWidth: 720, width: '100%', maxHeight: '85vh', overflow: 'auto', padding: 20
            }}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
              <strong style={{ fontSize: 16 }}>{detail.model_name}</strong>
              <Button size="xs" variant="secondary" onClick={() => setDetail(null)}>{t('extSubClose')}</Button>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 10 }}>
              {renderStatus(detail.status)} · {detail.question}
            </div>

            <div style={{ marginBottom: 14 }}>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>{t('extSubAnswer')}</div>
              <div style={{ fontSize: 13, lineHeight: 1.6, whiteSpace: 'pre-wrap', background: 'var(--bg-tertiary)', padding: 12, borderRadius: 8 }}>
                {detail.answer}
              </div>
            </div>

            {detail.share_link && (
              <div style={{ marginBottom: 14 }}>
                <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>{t('extSubShareLink')}</div>
                <a href={detail.share_link} target="_blank" rel="noreferrer" style={{ color: 'var(--brand-primary)', fontSize: 13, wordBreak: 'break-all' }}>
                  {detail.share_link}
                </a>
              </div>
            )}

            {detail.status === 'analyzed' && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                <div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>{t('extSubSummary')}</div>
                  <div style={{ fontSize: 13, lineHeight: 1.6 }}>{detail.summary || '-'}</div>
                </div>
                <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', fontSize: 13 }}>
                  <span><b>{t('extSubSentiment')}:</b> {SENTIMENT_LABELS[detail.sentiment] || detail.sentiment || '-'}</span>
                  <span><b>{t('extSubCategory')}:</b> {detail.category || '-'}</span>
                </div>
                {detail.mentions && detail.mentions.length > 0 && (
                  <div style={{ fontSize: 13 }}>
                    <b>{t('extSubMention')}:</b> {detail.mentions.join('、')}
                  </div>
                )}
                {detail.source_domains && detail.source_domains.length > 0 && (
                  <div style={{ fontSize: 13 }}>
                    <b>{t('extSubSources')}:</b> {detail.source_domains.join('、')}
                  </div>
                )}
              </div>
            )}

            {detail.status === 'failed' && (
              <div style={{ marginTop: 10, fontSize: 13, color: 'var(--status-danger)' }}>
                ⚠️ {t('extSubError')}: {detail.error_msg}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default ExternalSubmissions

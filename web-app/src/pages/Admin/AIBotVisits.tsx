import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Table, type TableColumn } from '@/components/Table'
import { api } from '@/services/api'
import type { AIBotVisitsReport, AIBotVisit } from '@/types/api'

/**
 * AI 爬虫访问监控 Tab：展示哪些大模型爬虫（GPTBot/ClaudeBot/PerplexityBot 等）
 * 访问过本站，含聚合计数与最近访问明细。数据来自进程内监控（重启后清空）。
 */
const AIBotVisits: React.FC = () => {
  const { t } = useTranslation()
  const [report, setReport] = useState<AIBotVisitsReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.admin.aiBotVisits(50)
      setReport(data)
    } catch (err: any) {
      setError(err?.message ? String(err.message) : '加载 AI 爬虫访问记录失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const columns: TableColumn<AIBotVisit>[] = [
    { key: 'bot', title: t('admin.aibotColBot', '爬虫'), dataIndex: 'bot' },
    { key: 'vendor', title: t('admin.aibotColVendor', '厂商'), dataIndex: 'vendor' },
    { key: 'path', title: t('admin.aibotColPath', '访问路径'), dataIndex: 'path' },
    {
      key: 'status',
      title: t('admin.aibotColStatus', '状态'),
      dataIndex: 'status',
      render: (record) => (
        <span style={{ color: record.status === 200 ? 'var(--status-success)' : 'var(--status-error)' }}>{record.status}</span>
      )
    },
    {
      key: 'at',
      title: t('admin.aibotColTime', '时间'),
      dataIndex: 'at',
      render: (record) => new Date(record.at).toLocaleString()
    }
  ]

  const botEntries = report ? Object.entries(report.bots).sort((a, b) => b[1] - a[1]) : []

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <Card title={t('admin.aibotTitle', 'AI 爬虫访问监控')} compact style={{ flex: 1 }}>
          <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
            <div>
              <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>{report?.total ?? 0}</div>
              <div style={{ fontSize: 13, color: 'var(--text-tertiary)' }}>{t('admin.aibotTotal', '累计访问')}</div>
            </div>
            <div>
              <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>{botEntries.length}</div>
              <div style={{ fontSize: 13, color: 'var(--text-tertiary)' }}>{t('admin.aibotBotCount', '爬虫种类')}</div>
            </div>
            <div>
              <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>{report?.uptime ?? '-'}</div>
              <div style={{ fontSize: 13, color: 'var(--text-tertiary)' }}>{t('admin.aibotUptime', '监控时长')}</div>
            </div>
          </div>
        </Card>
        <Button variant="secondary" size="md" loading={loading} onClick={load}>
          {loading ? '刷新中…' : '🔄 刷新'}
        </Button>
      </div>

      {error && (
        <div style={{ padding: 12, borderRadius: 8, background: 'var(--status-error-bg)', color: 'var(--status-error)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {botEntries.length > 0 && (
        <Card title={t('admin.aibotByBot', '按爬虫聚合')} compact>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            {botEntries.map(([bot, count]) => (
              <span
                key={bot}
                style={{
                  padding: '4px 12px', borderRadius: 999, fontSize: 13,
                  background: 'var(--bg-tertiary)', color: 'var(--text-primary)'
                }}
              >
                {bot} <strong style={{ color: 'var(--brand-primary)' }}>{count}</strong>
              </span>
            ))}
          </div>
        </Card>
      )}

      <Card title={t('admin.aibotRecent', '最近访问')} compact>
        <Table
          columns={columns}
          dataSource={report?.visits ?? []}
          rowKey={(r) => `${r.bot}-${r.at}-${r.path}`}
          striped
          emptyText={t('common.noData')}
        />
      </Card>
    </div>
  )
}

export default AIBotVisits

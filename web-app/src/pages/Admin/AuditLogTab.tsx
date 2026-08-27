import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Table, type TableColumn } from '@/components/Table'
import { api } from '@/services/api'
import type { AdminAuditLogEntry } from '@/types/api'

/**
 * 管理后台「审计日志」Tab：展示管理员操作留痕
 * （登录/登出/改密/权限变更/SQL 执行等，来自 admin_audit_log 表）。
 */
const AuditLogTab: React.FC = () => {
  const { t } = useTranslation()
  const [logs, setLogs] = useState<AdminAuditLogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionFilter, setActionFilter] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.admin.auditLog({ action: actionFilter || undefined, limit: 200 })
      setLogs(Array.isArray(data) ? data : [])
    } catch (err: any) {
      setError(err?.message ? String(err.message) : '加载审计日志失败')
    } finally {
      setLoading(false)
    }
  }, [actionFilter])

  useEffect(() => {
    load()
  }, [load])

  // 动作 → 人类可读标签 + 徽章色
  const actionMeta = (action: string): { label: string; tone: string } => {
    if (action.includes('db.exec')) {
      return action.includes('confirmed')
        ? { label: 'SQL 执行（已确认）', tone: 'warn' }
        : { label: 'SQL 执行', tone: 'warn' }
    }
    if (action.startsWith('user.')) {
      return {
        label: action === 'user.login' ? '登录' : action === 'user.logout' ? '登出' : '用户操作',
        tone: 'info'
      }
    }
    if (action.includes('password')) return { label: '密码变更', tone: 'warn' }
    if (action.includes('role') || action.includes('permission')) return { label: '权限变更', tone: 'warn' }
    if (action.includes('settings')) return { label: '系统设置', tone: 'info' }
    return { label: action, tone: 'info' }
  }

  const toneColor = (tone: string) => {
    switch (tone) {
      case 'warn': return 'var(--status-warning)'
      case 'info': return 'var(--status-info)'
      case 'ok': return 'var(--status-success)'
      default: return 'var(--text-tertiary)'
    }
  }

  const columns: TableColumn<AdminAuditLogEntry>[] = [
    {
      key: 'time',
      title: t('admin.auditColTime', '时间'),
      dataIndex: 'timestamp',
      render: (record) => new Date(record.timestamp).toLocaleString()
    },
    { key: 'actor', title: t('admin.auditColActor', '操作人'), dataIndex: 'actor' },
    {
      key: 'action',
      title: t('admin.auditColAction', '操作'),
      dataIndex: 'action',
      render: (record) => {
        const m = actionMeta(record.action)
        return (
          <span style={{
            display: 'inline-block', padding: '2px 10px', borderRadius: 999,
            fontSize: 12, fontWeight: 500,
            background: `color-mix(in srgb, ${toneColor(m.tone)} 12%, transparent)`,
            color: toneColor(m.tone)
          }}>
            {m.label}
          </span>
        )
      }
    },
    {
      key: 'sql',
      title: t('admin.auditColDetail', '详情 / SQL'),
      dataIndex: 'details',
      render: (record) => {
        const sql = record.details?.sql
        const detail = record.details?.detail
        if (sql) {
          return <code style={{ fontSize: 12, color: 'var(--text-secondary)', wordBreak: 'break-all' }}>{sql}</code>
        }
        return detail ? <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>{detail}</span> : <span style={{ color: 'var(--text-muted)' }}>-</span>
      }
    },
    { key: 'ip', title: t('admin.auditColIP', 'IP'), dataIndex: 'ip' }
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <Card title={t('admin.auditTitle', '审计日志')} compact style={{ flex: 1 }}>
          <div style={{ fontSize: 13, color: 'var(--text-tertiary)', lineHeight: 1.7 }}>
            {t('admin.auditDesc', '记录管理员关键操作（登录/登出/密码变更/SQL 执行等），来源 admin_audit_log 表，按时间倒序。')}
          </div>
        </Card>
        <div style={{ display: 'flex', gap: 8 }}>
          <select
            value={actionFilter}
            onChange={(e) => setActionFilter(e.target.value)}
            style={{
              padding: '6px 10px', borderRadius: 8, border: '1px solid var(--border-primary)',
              background: 'var(--surface-primary)', color: 'var(--text-primary)', fontSize: 13
            }}
          >
            <option value="">{t('admin.auditFilterAll', '全部操作')}</option>
            <option value="user.login">登录</option>
            <option value="user.logout">登出</option>
            <option value="admin.db.exec">SQL 执行</option>
          </select>
          <Button variant="secondary" size="md" loading={loading} onClick={load}>
            {loading ? '刷新中…' : '🔄 刷新'}
          </Button>
        </div>
      </div>

      {error && (
        <div style={{ padding: 12, borderRadius: 8, background: 'var(--status-error-bg)', color: 'var(--status-error)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <Card title={t('admin.auditRecent', '最近记录')} compact>
        <Table
          columns={columns}
          dataSource={logs}
          rowKey={(r) => r.id}
          striped
          emptyText={t('common.noData')}
        />
      </Card>
    </div>
  )
}

export default AuditLogTab

import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Textarea } from '@/components/Input'
import { api } from '@/services/api'
import type { DBExecResult } from '@/types/api'

/**
 * 管理后台「数据库」Tab：执行 SQL 完全修改数据库。
 *
 * 安全设计：
 *  - 管理员专用（后端 PermManageData 鉴权）；
 *  - 只允许单条语句（后端拦截多语句拼接）；
 *  - 写操作（DELETE/UPDATE/INSERT/DROP 等）需二次确认弹窗；
 *  - 查询自动限制行数（默认 200）。
 */
const DatabaseTab: React.FC = () => {
  const { t } = useTranslation()
  const [sql, setSql] = useState('')
  const [result, setResult] = useState<DBExecResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pendingWrite, setPendingWrite] = useState(false)

  const execute = async (confirmWrite: boolean) => {
    if (!sql.trim()) {
      setError('请输入 SQL')
      return
    }
    setLoading(true)
    setError(null)
    setResult(null)
    try {
      const data = await api.admin.dbExec(sql, confirmWrite, 200)
      if (data.need_confirm) {
        setPendingWrite(true)
        setError(null)
      } else {
        setResult(data)
        setPendingWrite(false)
      }
    } catch (err: any) {
      setError(err?.message ? String(err.message) : '执行失败')
      setPendingWrite(false)
    } finally {
      setLoading(false)
    }
  }

  const run = () => execute(false)
  const runConfirmed = () => {
    setPendingWrite(false)
    execute(true)
  }
  const cancelWrite = () => setPendingWrite(false)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card title={t('admin.dbTitle', '数据库管理（SQL 执行）')} compact>
        <p style={{ fontSize: 13, color: 'var(--text-tertiary)', margin: '0 0 12px', lineHeight: 1.7 }}>
          {t('admin.dbDesc', '管理员专用：可执行 SELECT 查询与 UPDATE/DELETE/INSERT 等写操作。写操作需要二次确认；查询自动限制 200 行。请谨慎操作——误执行可能造成数据不可恢复。')}
        </p>
        <Textarea
          value={sql}
          onChange={(e) => setSql(e.target.value)}
          placeholder="SELECT * FROM app_settings LIMIT 20;"
          rows={6}
          style={{ fontFamily: 'var(--font-family-mono)', fontSize: 13 }}
        />
        <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
          <Button variant="primary" size="md" loading={loading} onClick={run}>
            {loading ? '执行中…' : '▶ 执行'}
          </Button>
          <Button variant="ghost" size="md" onClick={() => { setSql(''); setResult(null); setError(null) }}>
            清空
          </Button>
        </div>
      </Card>

      {pendingWrite && (
        <div style={{
          padding: 16, borderRadius: 12,
          background: 'var(--status-warning-bg)', border: '1px solid var(--status-warning)'
        }}>
          <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--status-warning)', marginBottom: 8 }}>
            ⚠️ 这是写操作，确认执行？
          </div>
          <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 12, lineHeight: 1.7 }}>
            检测到 <code style={{ background: 'var(--bg-tertiary)', padding: '2px 6px', borderRadius: 4 }}>{sql.trim().split('\n')[0]}</code>{' '}
            是写操作（INSERT/UPDATE/DELETE/DDL），执行后不可撤销。
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button variant="danger" size="md" loading={loading} onClick={runConfirmed}>
              {loading ? '执行中…' : '确认执行'}
            </Button>
            <Button variant="secondary" size="md" onClick={cancelWrite}>取消</Button>
          </div>
        </div>
      )}

      {error && (
        <div style={{ padding: 12, borderRadius: 8, background: 'var(--status-error-bg)', color: 'var(--status-error)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {result && (
        <Card title={result.kind === 'query' ? `查询结果（${result.row_count} 行${result.truncated ? '，已截断' : ''}，${result.duration_ms}ms）` : `执行完成（影响 ${result.rows_affected} 行，${result.duration_ms}ms）`} compact>
          {result.kind === 'query' && result.columns && (
            <div style={{ overflowX: 'auto' }}>
              <table style={{ borderCollapse: 'collapse', fontSize: 13, width: '100%' }}>
                <thead>
                  <tr>
                    {result.columns.map((c) => (
                      <th key={c} style={{
                        textAlign: 'left', padding: '8px 12px', background: 'var(--surface-secondary)',
                        borderBottom: '1px solid var(--border-primary)', fontWeight: 600, whiteSpace: 'nowrap'
                      }}>{c}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(result.rows ?? []).map((row, i) => (
                    <tr key={i}>
                      {row.map((cell, j) => (
                        <td key={j} style={{
                          padding: '6px 12px', borderBottom: '1px solid var(--border-tertiary)',
                          color: 'var(--text-secondary)', maxWidth: 320, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'
                        }}>{cell === null || cell === undefined ? <span style={{ color: 'var(--text-muted)' }}>NULL</span> : String(cell)}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {result.kind === 'exec' && (
            <div style={{ fontSize: 14, color: 'var(--status-success)' }}>✅ 执行成功，影响 {result.rows_affected} 行</div>
          )}
        </Card>
      )}
    </div>
  )
}

export default DatabaseTab

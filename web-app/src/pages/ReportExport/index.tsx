import React, { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Input, Button, Modal } from '@/components'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { MailStatus, ReportEmailResponse } from '@/types/api'
import '../Dashboard/Dashboard.scss'

type Format = 'html' | 'htmlDownload' | 'pdf'

const ReportExport: React.FC = () => {
  const { t } = useTranslation()
  const brands = useAppStore(s => s.brands)
  const showToast = useAppStore(s => s.showToast)
  const [brand, setBrand] = useState(brands[0]?.name || '')
  const [mailStatus, setMailStatus] = useState<MailStatus | null>(null)
  const [emailOpen, setEmailOpen] = useState(false)
  const [emailTo, setEmailTo] = useState('ops@example.com')
  const [emailCC, setEmailCC] = useState('')
  const [emailFormat, setEmailFormat] = useState<'pdf' | 'html' | 'both'>('both')
  const [emailSending, setEmailSending] = useState(false)

  useEffect(() => {
    api.mailStatus().then(setMailStatus).catch(() => {})
  }, [])

  const doExport = (fmt: Format) => {
    if (!brand) return showToast('请选择品牌', 'error')
    const encodedBrand = encodeURIComponent(brand)
    const map: Record<Format, string> = {
      html: `/api/v1/brand/report/html?brand=${encodedBrand}`,
      htmlDownload: `/api/v1/brand/report/download?brand=${encodedBrand}`,
      pdf: `/api/v1/brand/report/pdf?brand=${encodedBrand}`
    }
    const url = map[fmt]
    if (fmt === 'html') {
      window.open(url, '_blank')
    } else {
      const a = document.createElement('a')
      a.href = url
      a.download = ''
      a.click()
    }
    showToast('导出成功', 'success')
  }

  const sendEmail = async () => {
    if (!brand || !emailTo.trim()) return showToast('请填写品牌和收件人', 'error')
    setEmailSending(true)
    try {
      const to = emailTo.split(/[,，]/).map(s => s.trim()).filter(Boolean)
      const cc = emailCC.split(/[,，]/).map(s => s.trim()).filter(Boolean)
      const r: ReportEmailResponse = await api.reportEmail({ brand, to, cc, format: emailFormat })
      showToast(`${t('alertEmail.emailSentSuccess')} → ${r.subject}`, 'success')
      setEmailOpen(false)
    } catch (e: any) {
      showToast(e.message || '发送失败', 'error')
    } finally {
      setEmailSending(false)
    }
  }

  const cards: { key: Format; icon: string; name: string; desc: string; variant: 'primary' | 'secondary' | 'outline' }[] = [
    { key: 'html', icon: '🌐', name: t('reportExport.formats.html'), desc: '在浏览器打开，可直接打印为PDF', variant: 'primary' },
    { key: 'htmlDownload', icon: '📄', name: t('reportExport.formats.htmlDownload'), desc: '下载为HTML文件，离线查看', variant: 'secondary' },
    { key: 'pdf', icon: '📕', name: t('reportExport.formats.pdf'), desc: '服务端chromedp渲染为A4 PDF', variant: 'outline' }
  ]

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('reportExport.title')}</h1>
          <p className="page-subtitle">{t('reportExport.subtitle')}</p>
        </div>
      </div>

      <Card
        title={t('reportExport.selectBrand') + ' / ' + t('reportExport.selectFormat')}
        actions={
          <Button
            variant={mailStatus?.enabled ? 'primary' : 'secondary'}
            disabled={!mailStatus?.enabled}
            onClick={() => setEmailOpen(true)}
          >
            📧 {t('reportExport.emailReport')}
            {!mailStatus?.enabled && <span style={{ fontSize: 10, marginLeft: 4, opacity: 0.7 }}>未启用</span>}
          </Button>
        }
        compact
      >
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 20, flexWrap: 'wrap' }}>
          <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' }}>品牌：</label>
          <select value={brand} onChange={(e) => setBrand(e.target.value)}
            style={{
              padding: '8px 12px', borderRadius: 8, fontSize: 13,
              border: '1px solid var(--border-primary)',
              background: 'var(--surface-primary)', color: 'var(--text-primary)'
            }}>
            {brands.map(b => <option key={b.name} value={b.name}>{b.name}</option>)}
          </select>
          <div style={{ flex: 1 }} />
          <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
            后端会从审计历史库读取该品牌的最新记录
          </span>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
          {cards.map(c => (
            <div key={c.key} style={{
              padding: 20, borderRadius: 12,
              border: '1px solid var(--border-primary)',
              background: 'linear-gradient(180deg, var(--surface-secondary), var(--surface-primary))',
              cursor: 'pointer',
              transition: 'all 0.2s'
            }}
              onClick={() => doExport(c.key)}
              onMouseEnter={(e) => { e.currentTarget.style.transform = 'translateY(-2px)'; e.currentTarget.style.boxShadow = 'var(--shadow-lg)' }}
              onMouseLeave={(e) => { e.currentTarget.style.transform = ''; e.currentTarget.style.boxShadow = '' }}
            >
              <div style={{ fontSize: 36, marginBottom: 8 }}>{c.icon}</div>
              <div style={{ fontSize: 15, fontWeight: 700, marginBottom: 4 }}>{c.name}</div>
              <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 12 }}>{c.desc}</div>
              <Button variant={c.variant} size="sm" onClick={(e) => { e.stopPropagation(); doExport(c.key) }}>
                立即导出 →
              </Button>
            </div>
          ))}
        </div>
      </Card>

      <Card title={t('reportExport.reportHistory')} subtitle="示例数据" compact style={{ marginTop: 16 }}>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead style={{ background: 'var(--surface-secondary)' }}>
              <tr>
                <th style={{ textAlign: 'left', padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>{t('reportExport.exportTime')}</th>
                <th style={{ textAlign: 'left', padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>品牌</th>
                <th style={{ textAlign: 'left', padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>{t('reportExport.exportFormat')}</th>
                <th style={{ textAlign: 'left', padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>{t('reportExport.fileSize')}</th>
                <th style={{ textAlign: 'right', padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {[
                { t: Date.now() - 3600_000, b: brands[0]?.name || '示例科技', f: 'PDF', s: '3.2 MB' },
                { t: Date.now() - 86400_000 * 2, b: brands[0]?.name || '示例科技', f: 'HTML', s: '1.1 MB' },
                { t: Date.now() - 86400_000 * 5, b: brands[0]?.name || '示例科技', f: 'PDF', s: '3.0 MB' }
              ].map((r, i) => (
                <tr key={i} style={{ transition: 'background 0.15s' }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--surface-secondary)')}
                  onMouseLeave={(e) => (e.currentTarget.style.background = '')}
                >
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>
                    {new Date(r.t).toLocaleString()}
                  </td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>{r.b}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>
                    <span style={{
                      padding: '2px 8px', borderRadius: 999, fontSize: 11,
                      background: r.f === 'PDF' ? 'var(--status-error-bg)' : 'var(--status-info-bg)',
                      color: r.f === 'PDF' ? 'var(--status-error)' : 'var(--status-info)'
                    }}>{r.f}</span>
                  </td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-primary)' }}>{r.s}</td>
                  <td style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-primary)', textAlign: 'right' }}>
                    <Button size="xs" variant="ghost" onClick={() => showToast('重新下载', 'info')}>下载</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      <Modal
        open={emailOpen}
        onClose={() => setEmailOpen(false)}
        title={t('reportExport.emailReport')}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setEmailOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={sendEmail} loading={emailSending}>📧 {t('reportExport.sendEmail')}</Button>
          </>
        }
      >
        {!mailStatus?.enabled ? (
          <div style={{ padding: 20, borderRadius: 8, background: 'var(--status-warning-bg)', color: 'var(--status-warning)', fontSize: 13 }}>
            ⚠️ 邮件服务未启用。请在后端配置 GEO_SMTP_* 环境变量。
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div style={{ padding: 10, borderRadius: 6, background: 'var(--surface-secondary)', fontSize: 12 }}>
              SMTP: {mailStatus.host}:{mailStatus.port} · 发件人：{mailStatus.from}
            </div>
            <Input
              label="品牌"
              value={brand}
              onChange={(e) => setBrand(e.target.value)}
            />
            <Input
              label={t('reportExport.emailRecipients') + '（逗号分隔）'}
              required
              value={emailTo}
              onChange={(e) => setEmailTo(e.target.value)}
              placeholder="ops@x.com, mgr@x.com"
            />
            <Input
              label={t('reportExport.emailCC') + '（逗号分隔）'}
              value={emailCC}
              onChange={(e) => setEmailCC(e.target.value)}
            />
            <div>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                {t('reportExport.emailFormat')}
              </label>
              <div style={{ display: 'flex', gap: 6 }}>
                {(['pdf', 'html', 'both'] as const).map(f => (
                  <button key={f} type="button"
                    onClick={() => setEmailFormat(f)}
                    style={{
                      padding: '6px 12px', borderRadius: 8, fontSize: 12,
                      border: emailFormat === f ? '1px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: emailFormat === f ? 'color-mix(in srgb, var(--brand-primary) 10%, var(--surface-primary))' : 'var(--surface-primary)',
                      color: emailFormat === f ? 'var(--brand-primary)' : 'var(--text-primary)',
                      cursor: 'pointer'
                    }}>
                    {t(`reportExport.emailFormats.${f}`)}
                  </button>
                ))}
              </div>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

export default ReportExport

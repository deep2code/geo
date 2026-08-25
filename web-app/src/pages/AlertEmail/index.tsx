import React, { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Input, Button, Modal, Table, type TableColumn } from '@/components'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { MailStatus, SchedulerStatus } from '@/types/api'
import '../Dashboard/Dashboard.scss'

type RuleMetric = 'bvs' | 'mentionRate' | 'citationRate' | 'positiveRate' | 'sov' | 'negativeMentions'
type DayKey = 'monday' | 'tuesday' | 'wednesday' | 'thursday' | 'friday' | 'saturday' | 'sunday'

interface AlertRule {
  id: string
  name: string
  brand: string
  metric: RuleMetric
  threshold: number
  recipients: string[]
  enabled: boolean
}

const dayKeys: DayKey[] = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday']

const AlertEmail: React.FC = () => {
  const { t } = useTranslation()
  const brands = useAppStore(s => s.brands)
  const showToast = useAppStore(s => s.showToast)

  const [mailStatus, setMailStatus] = useState<MailStatus | null>(null)
  const [schedulerStatus, setSchedulerStatus] = useState<SchedulerStatus | null>(null)
  const [rules, setRules] = useState<AlertRule[]>([
    { id: '1', name: '示例科技-BVS监控', brand: '示例科技', metric: 'bvs', threshold: 70, recipients: ['ops@x.com'], enabled: true },
    { id: '2', name: '示例科技-负面预警', brand: '示例科技', metric: 'negativeMentions', threshold: 3, recipients: ['pr@x.com'], enabled: true }
  ])
  const [ruleOpen, setRuleOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<Partial<AlertRule> | null>(null)
  const [weeklyEnabled, setWeeklyEnabled] = useState(true)
  const [weeklyDay, setWeeklyDay] = useState<DayKey>('monday')
  const [weeklyTime, setWeeklyTime] = useState('09:00')
  const [weeklyRecipients, setWeeklyRecipients] = useState('weekly@x.com, mgr@x.com')
  const [weeklyBrands, setWeeklyBrands] = useState<string[]>(brands.map(b => b.name))

  const [testOpen, setTestOpen] = useState(false)
  const [testSubject, setTestSubject] = useState('[崛起GEO] 测试邮件')
  const [testBody, setTestBody] = useState('这是一封来自 崛起GEO 控制台的测试邮件。')
  const [testSending, setTestSending] = useState(false)

  useEffect(() => {
    api.mailStatus().then(setMailStatus).catch(() => {})
    api.schedulerStatus().then(setSchedulerStatus).catch(() => null)
  }, [])

  const toggleRule = (id: string) => {
    setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !r.enabled } : r))
  }

  const openAddRule = () => {
    setEditingRule({
      name: '',
      brand: brands[0]?.name,
      metric: 'bvs',
      threshold: 70,
      recipients: [''],
      enabled: true
    })
    setRuleOpen(true)
  }
  const openEditRule = (r: AlertRule) => {
    setEditingRule({ ...r })
    setRuleOpen(true)
  }
  const saveRule = () => {
    if (!editingRule || !editingRule.name?.trim()) return showToast('规则名称必填', 'error')
    if (editingRule.id) {
      setRules(prev => prev.map(r => r.id === editingRule.id ? { ...r, ...editingRule } as AlertRule : r))
    } else {
      setRules(prev => [...prev, { ...editingRule, id: String(Date.now()) } as AlertRule])
    }
    setRuleOpen(false)
    showToast('告警规则已保存', 'success')
  }
  const deleteRule = (id: string) => {
    setRules(prev => prev.filter(r => r.id !== id))
    showToast('规则已删除', 'info')
  }

  const sendTest = async () => {
    if (!testSubject || !testBody) return showToast('请填写主题和内容', 'error')
    setTestSending(true)
    try {
      await api.mailSend({
        to: ['ops@example.com'],
        subject: testSubject,
        text: testBody
      })
      showToast('测试邮件已发送', 'success')
      setTestOpen(false)
    } catch (e: any) {
      showToast(e.message || '发送失败', 'error')
    } finally {
      setTestSending(false)
    }
  }

  const metricLabel = (m: RuleMetric) => t(`alertEmail.metrics.${m}`)

  const ruleCols: TableColumn<AlertRule>[] = [
    {
      key: 'enabled',
      title: '启用',
      width: 60,
      align: 'center',
      render: (r) => (
        <label style={{
          display: 'inline-block', position: 'relative', width: 36, height: 20,
          background: r.enabled ? 'var(--brand-primary)' : 'var(--bg-active)',
          borderRadius: 999, cursor: 'pointer', transition: 'all 0.2s'
        }}>
          <input type="checkbox" checked={r.enabled}
            style={{ opacity: 0, width: 0, height: 0 }}
            onChange={() => toggleRule(r.id)} />
          <span style={{
            position: 'absolute', top: 2, left: r.enabled ? 18 : 2,
            width: 16, height: 16, borderRadius: '50%',
            background: 'white', transition: 'all 0.2s', boxShadow: 'var(--shadow-xs)'
          }} />
        </label>
      )
    },
    { key: 'name', title: t('alertEmail.ruleName'), dataIndex: 'name', sortable: true },
    { key: 'brand', title: t('alertEmail.ruleBrand'), dataIndex: 'brand' },
    {
      key: 'metric',
      title: t('alertEmail.ruleMetric'),
      render: (r) => metricLabel(r.metric)
    },
    {
      key: 'th',
      title: t('alertEmail.ruleThreshold'),
      align: 'center',
      render: (r) => (
        <span style={{ fontWeight: 600, color: 'var(--status-warning)' }}>
          {'< '}{r.threshold}
        </span>
      )
    },
    {
      key: 'recipients',
      title: t('alertEmail.ruleRecipients'),
      render: (r) => <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>{r.recipients.join(', ')}</span>
    },
    {
      key: 'actions',
      title: '操作',
      align: 'right',
      render: (r) => (
        <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
          <Button size="xs" variant="ghost" onClick={() => openEditRule(r)}>编辑</Button>
          <Button size="xs" variant="danger" onClick={() => deleteRule(r.id)}>删除</Button>
        </div>
      )
    }
  ]

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('alertEmail.title')}</h1>
          <p className="page-subtitle">{t('alertEmail.subtitle')}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button variant="secondary" onClick={() => setTestOpen(true)}>🧪 {t('alertEmail.testEmail')}</Button>
          <Button onClick={openAddRule}>+ {t('alertEmail.addRule')}</Button>
        </div>
      </div>

      <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(2, 1fr)' }}>
        <Card title={t('alertEmail.status')} compact>
          <div style={{ display: 'flex', gap: 20, alignItems: 'center' }}>
            <div style={{
              width: 48, height: 48, borderRadius: '50%',
              display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 22,
              background: mailStatus?.enabled ? 'var(--status-success-bg)' : 'var(--status-error-bg)',
              color: mailStatus?.enabled ? 'var(--status-success)' : 'var(--status-error)'
            }}>📧</div>
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 16, fontWeight: 700, marginBottom: 4 }}>
                {mailStatus?.enabled ? t('alertEmail.enabled') : t('alertEmail.disabled')}
              </div>
              {mailStatus?.enabled && (
                <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                  {mailStatus.host}:{mailStatus.port} · {mailStatus.from}
                </div>
              )}
              {!mailStatus?.enabled && (
                <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                  配置 GEO_SMTP_HOST / PORT / USER / PASS / FROM 启用
                </div>
              )}
            </div>
          </div>
        </Card>

        <Card title={t('alertEmail.scheduler')} compact>
          <div style={{ display: 'flex', gap: 20, alignItems: 'center' }}>
            <div style={{
              width: 48, height: 48, borderRadius: '50%',
              display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 22,
              background: schedulerStatus?.enabled ? 'var(--status-info-bg)' : 'var(--bg-tertiary)',
              color: schedulerStatus?.enabled ? 'var(--status-info)' : 'var(--text-tertiary)'
            }}>⏰</div>
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 16, fontWeight: 700, marginBottom: 4 }}>
                {schedulerStatus?.enabled ? t('alertEmail.schedulerEnabled') : t('alertEmail.schedulerDisabled')}
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
                配置数 {schedulerStatus?.config_count ?? 0} · GEO_SCHEDULER_ENABLED=true 启用
              </div>
            </div>
            <Button
              size="sm"
              variant={schedulerStatus?.enabled ? 'primary' : 'secondary'}
              disabled={!schedulerStatus?.enabled}
              onClick={() => {
                if (!brands[0]) return showToast('请先创建品牌', 'warning')
                api.schedulerTrigger(brands[0].name).then(r => showToast(r.ok ? '调度器：已触发审计' : '失败', r.ok ? 'success' : 'error')).catch(e => showToast(e.message, 'error'))
              }}
            >
              ▶️ {t('alertEmail.schedulerTrigger')}
            </Button>
          </div>
        </Card>
      </div>

      <Card
        title={t('alertEmail.alertRules')}
        subtitle={`${rules.length} 条规则`}
        actions={<Button size="sm" variant="secondary" onClick={openAddRule}>+ {t('alertEmail.addRule')}</Button>}
        compact
        style={{ marginTop: 16 }}
      >
        <Table columns={ruleCols} dataSource={rules} rowKey="id" striped />
      </Card>

      <Card title={t('alertEmail.weeklyReport')} compact style={{ marginTop: 16 }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' }}>{t('alertEmail.weeklyEnabled')}</label>
              <label style={{
                display: 'inline-block', position: 'relative', width: 36, height: 20,
                background: weeklyEnabled ? 'var(--brand-primary)' : 'var(--bg-active)',
                borderRadius: 999, cursor: 'pointer', transition: 'all 0.2s'
              }}>
                <input type="checkbox" checked={weeklyEnabled}
                  style={{ opacity: 0, width: 0, height: 0 }}
                  onChange={(e) => setWeeklyEnabled(e.target.checked)} />
                <span style={{
                  position: 'absolute', top: 2,
                  left: weeklyEnabled ? 18 : 2,
                  width: 16, height: 16, borderRadius: '50%',
                  background: 'white', transition: 'all 0.2s', boxShadow: 'var(--shadow-xs)'
                }} />
              </label>
            </div>
            <div>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                {t('alertEmail.weeklyDay')}
              </label>
              <div style={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
                {dayKeys.map(d => (
                  <button key={d} type="button"
                    onClick={() => setWeeklyDay(d)}
                    style={{
                      padding: '4px 8px', fontSize: 11, borderRadius: 6,
                      border: weeklyDay === d ? '1px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: weeklyDay === d ? 'color-mix(in srgb, var(--brand-primary) 10%, var(--surface-primary))' : 'var(--surface-primary)',
                      color: weeklyDay === d ? 'var(--brand-primary)' : 'var(--text-primary)',
                      cursor: 'pointer'
                    }}>
                    {t(`alertEmail.days.${d}`)}
                  </button>
                ))}
              </div>
            </div>
            <Input label={t('alertEmail.weeklyTime')} type="time" value={weeklyTime}
              onChange={(e) => setWeeklyTime(e.target.value)} />
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, gridColumn: 'span 2' }}>
            <Input
              label={t('alertEmail.weeklyRecipients') + '（逗号分隔）'}
              value={weeklyRecipients}
              onChange={(e) => setWeeklyRecipients(e.target.value)}
            />
            <div>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                {t('alertEmail.weeklyBrands')}
              </label>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                {brands.map(b => {
                  const checked = weeklyBrands.includes(b.name)
                  return (
                    <button key={b.name} type="button"
                      onClick={() => {
                        setWeeklyBrands(prev => checked ? prev.filter(n => n !== b.name) : [...prev, b.name])
                      }}
                      style={{
                        padding: '4px 10px', fontSize: 12, borderRadius: 999,
                        border: checked ? '1px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                        background: checked ? 'var(--brand-primary)' : 'var(--surface-primary)',
                        color: checked ? 'white' : 'var(--text-primary)',
                        cursor: 'pointer'
                      }}>
                      {checked ? '✓ ' : ''}{b.name}
                    </button>
                  )
                })}
              </div>
            </div>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <Button variant="secondary" onClick={() => showToast('预览告警模板：POST /api/v1/mail/send template=alert', 'info')}>
                👁️ {t('alertEmail.alertTemplate')}
              </Button>
              <Button variant="secondary" onClick={() => showToast('预览周报模板：POST /api/v1/mail/send template=weekly', 'info')}>
                👁️ {t('alertEmail.weeklyTemplate')}
              </Button>
              <Button onClick={() => showToast('周报配置已保存', 'success')}>💾 保存配置</Button>
            </div>
          </div>
        </div>
      </Card>

      <Modal
        open={ruleOpen}
        onClose={() => setRuleOpen(false)}
        title={editingRule?.id ? '编辑规则' : t('alertEmail.addRule')}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setRuleOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={saveRule}>{t('common.save')}</Button>
          </>
        }
      >
        {editingRule && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Input label={t('alertEmail.ruleName')} required value={editingRule.name || ''}
              onChange={(e) => setEditingRule({ ...editingRule, name: e.target.value })} />
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <div>
                <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                  {t('alertEmail.ruleBrand')}
                </label>
                <select value={editingRule.brand || ''}
                  onChange={(e) => setEditingRule({ ...editingRule, brand: e.target.value })}
                  style={{
                    width: '100%', padding: '8px 12px', borderRadius: 8,
                    border: '1px solid var(--border-primary)',
                    background: 'var(--surface-primary)', color: 'var(--text-primary)', fontSize: 13
                  }}>
                  {brands.map(b => <option key={b.name} value={b.name}>{b.name}</option>)}
                </select>
              </div>
              <div>
                <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)', marginBottom: 4, display: 'block' }}>
                  {t('alertEmail.ruleMetric')}
                </label>
                <select value={editingRule.metric || 'bvs'}
                  onChange={(e) => setEditingRule({ ...editingRule, metric: e.target.value as RuleMetric })}
                  style={{
                    width: '100%', padding: '8px 12px', borderRadius: 8,
                    border: '1px solid var(--border-primary)',
                    background: 'var(--surface-primary)', color: 'var(--text-primary)', fontSize: 13
                  }}>
                  {(['bvs', 'mentionRate', 'citationRate', 'positiveRate', 'sov', 'negativeMentions'] as RuleMetric[]).map(m => (
                    <option key={m} value={m}>{metricLabel(m)}</option>
                  ))}
                </select>
              </div>
            </div>
            <Input
              label={t('alertEmail.ruleThreshold')}
              hint="低于此值时触发告警（负面提及数则是超过此值）"
              type="number"
              value={editingRule.threshold ?? 0}
              onChange={(e) => setEditingRule({ ...editingRule, threshold: Number(e.target.value) })}
            />
            <Input
              label={t('alertEmail.ruleRecipients') + '（逗号分隔）'}
              value={(editingRule.recipients || []).join(', ')}
              onChange={(e) => setEditingRule({
                ...editingRule,
                recipients: e.target.value.split(/[,，]/).map(s => s.trim()).filter(Boolean)
              })}
              placeholder="ops@x.com, mgr@x.com"
            />
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' }}>
                {t('alertEmail.ruleEnabled')}
              </label>
              <label style={{
                display: 'inline-block', position: 'relative', width: 36, height: 20,
                background: editingRule.enabled ? 'var(--brand-primary)' : 'var(--bg-active)',
                borderRadius: 999, cursor: 'pointer', transition: 'all 0.2s'
              }}>
                <input type="checkbox" checked={!!editingRule.enabled}
                  style={{ opacity: 0, width: 0, height: 0 }}
                  onChange={(e) => setEditingRule({ ...editingRule, enabled: e.target.checked })} />
                <span style={{
                  position: 'absolute', top: 2,
                  left: editingRule.enabled ? 18 : 2,
                  width: 16, height: 16, borderRadius: '50%',
                  background: 'white', transition: 'all 0.2s', boxShadow: 'var(--shadow-xs)'
                }} />
              </label>
            </div>
          </div>
        )}
      </Modal>

      <Modal
        open={testOpen}
        onClose={() => setTestOpen(false)}
        title={t('alertEmail.testEmail')}
        size="md"
        footer={
          <>
            <Button variant="secondary" onClick={() => setTestOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={sendTest} loading={testSending}>📧 发送</Button>
          </>
        }
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {!mailStatus?.enabled && (
            <div style={{ padding: 12, borderRadius: 8, background: 'var(--status-warning-bg)', color: 'var(--status-warning)', fontSize: 13 }}>
              ⚠️ 邮件服务未启用，发送将失败
            </div>
          )}
          <Input
            label={t('alertEmail.testSubject')}
            value={testSubject}
            onChange={(e) => setTestSubject(e.target.value)}
          />
          <textarea
            value={testBody}
            onChange={(e) => setTestBody(e.target.value)}
            rows={6}
            style={{
              width: '100%', padding: 12, borderRadius: 8,
              border: '1px solid var(--border-primary)',
              background: 'var(--surface-primary)', color: 'var(--text-primary)',
              fontSize: 13, resize: 'vertical', fontFamily: 'inherit'
            }}
          />
          <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>
            收件人：ops@example.com（可改为 POST /api/v1/mail/send 指定任意地址）
          </div>
        </div>
      </Modal>
    </div>
  )
}

export default AlertEmail

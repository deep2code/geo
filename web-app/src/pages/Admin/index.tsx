import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Input, Textarea } from '@/components/Input'
import { Table, type TableColumn } from '@/components/Table'
import { Tabs, TabPane } from '@/components/Tabs'
import SettingsTab from './SettingsTab'
import ExternalSubmissions from './ExternalSubmissions'
import { useAppStore } from '@/store/useAppStore'
import api, { getApiAuthToken } from '@/services/api'
import '../Dashboard/Dashboard.scss'
import './Admin.scss'

// 租户数据类型
interface TenantRow {
  id: string
  name: string
  plan: string
  status: string
  brand_count: number
  audit_count: number
  email_count: number
  last_active: string
}

// 公告数据类型
interface AnnouncementRow {
  id: string
  title: string
  content: string
  type: string
  expire_at: string
  created_at: string
}

// 默认租户模拟数据（API 不可用时回退）
const mockTenants: TenantRow[] = [
  { id: '1', name: '示例科技', plan: 'pro', status: 'active', brand_count: 12, audit_count: 348, email_count: 56, last_active: new Date(Date.now() - 3600_000).toISOString() },
  { id: '2', name: '智云数据', plan: 'enterprise', status: 'active', brand_count: 28, audit_count: 1024, email_count: 210, last_active: new Date(Date.now() - 7200_000).toISOString() },
  { id: '3', name: '星辰互联', plan: 'free', status: 'suspended', brand_count: 3, audit_count: 18, email_count: 2, last_active: new Date(Date.now() - 86400_000 * 5).toISOString() },
  { id: '4', name: '蓝海传媒', plan: 'pro', status: 'active', brand_count: 8, audit_count: 156, email_count: 32, last_active: new Date(Date.now() - 86400_000).toISOString() },
  { id: '5', name: '锐捷网络', plan: 'enterprise', status: 'pending', brand_count: 15, audit_count: 420, email_count: 88, last_active: new Date(Date.now() - 86400_000 * 3).toISOString() }
]

const mockAnnouncements: AnnouncementRow[] = [
  { id: '1', title: '系统维护通知', content: '本周日凌晨 2:00-4:00 进行系统维护升级', type: 'system', expire_at: new Date(Date.now() + 86400_000 * 7).toISOString(), created_at: new Date(Date.now() - 86400_000).toISOString() },
  { id: '2', title: '新功能上线', content: '品牌审计支持更多 AI 引擎，欢迎体验', type: 'feature', expire_at: new Date(Date.now() + 86400_000 * 30).toISOString(), created_at: new Date(Date.now() - 86400_000 * 3).toISOString() }
]

const Admin: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)

  const hasApiToken = Boolean(getApiAuthToken())

  // 租户管理状态
  const [tenants, setTenants] = useState<TenantRow[]>(mockTenants)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [page, setPage] = useState(1)
  const pageSize = 10

  // 用量统计状态
  const [usage, setUsage] = useState<any>(null)

  // 公告管理状态
  const [announcements, setAnnouncements] = useState<AnnouncementRow[]>(mockAnnouncements)
  const [announceForm, setAnnounceForm] = useState({
    title: '',
    content: '',
    type: 'system',
    expire_at: ''
  })
  const [submitting, setSubmitting] = useState(false)

  // 系统信息状态
  const [systemInfo, setSystemInfo] = useState<any>(null)

  // 加载用量统计
  const loadUsage = async () => {
    try {
      const res = await api.admin.usage()
      setUsage(res)
    } catch {
      // API 不可用，使用默认空值
      setUsage(null)
    }
  }

  // 加载系统信息
  const loadSystem = async () => {
    try {
      const res = await api.admin.system()
      setSystemInfo(res)
    } catch {
      setSystemInfo(null)
    }
  }

  // 加载公告列表
  const loadAnnouncements = async () => {
    try {
      const res = await api.admin.announcements()
      if (res?.items) setAnnouncements(res.items)
    } catch {
      // 保留默认模拟数据
    }
  }

  // 加载租户列表
  const loadTenants = async () => {
    try {
      const res = await api.admin.tenants({ status: statusFilter || undefined, page, limit: pageSize })
      if (res?.items) setTenants(res.items)
    } catch {
      // 保留默认模拟数据
    }
  }

  useEffect(() => {
    loadUsage()
    loadSystem()
    loadAnnouncements()
  }, [])

  useEffect(() => {
    loadTenants()
  }, [statusFilter, page])

  // 过滤后的租户列表（用于前端兜底分页）
  const filteredTenants = statusFilter
    ? tenants.filter(t => t.status === statusFilter)
    : tenants

  // 切换租户状态
  const handleToggleTenantStatus = async (tenant: TenantRow) => {
    const newStatus = tenant.status === 'active' ? 'suspended' : 'active'
    try {
      await api.admin.updateTenantStatus(tenant.id, newStatus)
      showToast(`${tenant.name} 已${newStatus === 'active' ? '激活' : '暂停'}`, 'success')
      setTenants(prev => prev.map(t => t.id === tenant.id ? { ...t, status: newStatus } : t))
    } catch {
      // API 不可用时仍更新本地状态
      showToast(`${tenant.name} 已${newStatus === 'active' ? '激活' : '暂停'}（本地）`, 'info')
      setTenants(prev => prev.map(t => t.id === tenant.id ? { ...t, status: newStatus } : t))
    }
  }

  // 创建公告
  const handleCreateAnnouncement = async () => {
    if (!announceForm.title || !announceForm.content) {
      showToast(t('admin.announceTitleContentRequired'), 'warning')
      return
    }
    setSubmitting(true)
    try {
      await api.admin.createAnnouncement({
        ...announceForm,
        expire_at: announceForm.expire_at || undefined
      })
      showToast(t('admin.announceCreated'), 'success')
      const newAnnouncement: AnnouncementRow = {
        id: String(Date.now()),
        title: announceForm.title,
        content: announceForm.content,
        type: announceForm.type,
        expire_at: announceForm.expire_at || new Date(Date.now() + 86400_000 * 30).toISOString(),
        created_at: new Date().toISOString()
      }
      setAnnouncements(prev => [newAnnouncement, ...prev])
      setAnnounceForm({ title: '', content: '', type: 'system', expire_at: '' })
      loadAnnouncements()
    } catch {
      // 本地追加
      showToast(t('admin.announceCreatedLocal'), 'info')
      const newAnnouncement: AnnouncementRow = {
        id: String(Date.now()),
        title: announceForm.title,
        content: announceForm.content,
        type: announceForm.type,
        expire_at: announceForm.expire_at || new Date(Date.now() + 86400_000 * 30).toISOString(),
        created_at: new Date().toISOString()
      }
      setAnnouncements(prev => [newAnnouncement, ...prev])
      setAnnounceForm({ title: '', content: '', type: 'system', expire_at: '' })
    } finally {
      setSubmitting(false)
    }
  }

  // 删除公告
  const handleDeleteAnnouncement = async (id: string) => {
    if (!confirm(t('admin.announceDeleteConfirm'))) return
    try {
      await api.admin.deleteAnnouncement(id)
      showToast(t('admin.announceDeleted'), 'success')
    } catch {
      showToast(t('admin.announceDeletedLocal'), 'info')
    }
    setAnnouncements(prev => prev.filter(a => a.id !== id))
  }

  // 状态徽章渲染
  const renderStatusBadge = (status: string) => {
    const cls = `admin-status-badge admin-status-badge-${status}`
    const label = status === 'active' ? t('admin.tenantActive') : status === 'suspended' ? t('admin.tenantSuspended') : t('admin.tenantPending')
    return <span className={cls}>● {label}</span>
  }

  // 租户表格列定义
  const tenantColumns: TableColumn<TenantRow>[] = [
    { key: 'name', title: t('admin.tenantName'), dataIndex: 'name', sortable: true },
    {
      key: 'plan',
      title: t('admin.tenantPlan'),
      width: 110,
      render: (r) => (
        <span style={{
          fontSize: 11,
          padding: '2px 8px',
          borderRadius: 4,
          fontWeight: 600,
          background: r.plan === 'enterprise' ? 'var(--status-info-bg)' : r.plan === 'pro' ? 'var(--status-success-bg)' : 'var(--bg-tertiary)',
          color: r.plan === 'enterprise' ? 'var(--status-info)' : r.plan === 'pro' ? 'var(--status-success)' : 'var(--text-tertiary)',
          textTransform: 'uppercase'
        }}>{r.plan}</span>
      )
    },
    { key: 'status', title: t('admin.tenantStatus'), width: 110, render: (r) => renderStatusBadge(r.status) },
    { key: 'brand_count', title: t('admin.tenantBrandCount'), width: 100, align: 'right', sortable: true, render: (r) => r.brand_count },
    { key: 'audit_count', title: t('admin.tenantAuditCount'), width: 100, align: 'right', sortable: true, render: (r) => r.audit_count },
    { key: 'email_count', title: t('admin.tenantEmailCount'), width: 100, align: 'right', sortable: true, render: (r) => r.email_count },
    { key: 'last_active', title: t('admin.tenantLastActive'), width: 160, render: (r) => new Date(r.last_active).toLocaleString() },
    {
      key: 'action',
      title: t('admin.tenantAction'),
      width: 110,
      align: 'right',
      render: (r) => (
        <Button
          size="sm"
          variant={r.status === 'active' ? 'danger' : 'success'}
          onClick={(e) => {
            e.stopPropagation()
            handleToggleTenantStatus(r)
          }}
        >
          {r.status === 'active' ? t('admin.tenantSuspend') : t('admin.tenantActivate')}
        </Button>
      )
    }
  ]

  // 公告表格列定义
  const announcementColumns: TableColumn<AnnouncementRow>[] = [
    { key: 'title', title: t('admin.announceTitle'), dataIndex: 'title' },
    { key: 'content', title: t('admin.announceContent'), render: (r) => <span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{r.content}</span> },
    {
      key: 'type',
      title: t('admin.announceType'),
      width: 100,
      render: (r) => (
        <span style={{ fontSize: 11, padding: '2px 8px', borderRadius: 4, background: 'var(--surface-tertiary)', color: 'var(--text-secondary)' }}>{r.type}</span>
      )
    },
    { key: 'expire_at', title: t('admin.announceExpireAt'), width: 160, render: (r) => new Date(r.expire_at).toLocaleString() },
    {
      key: 'action',
      title: '',
      width: 80,
      align: 'right',
      render: (r) => (
        <Button size="sm" variant="danger" onClick={(e) => { e.stopPropagation(); handleDeleteAnnouncement(r.id) }}>
          {t('common.delete')}
        </Button>
      )
    }
  ]

  // 用量统计卡片数据
  const statsData = usage?.stats ?? {
    total_brands: tenants.reduce((s, t) => s + t.brand_count, 0),
    total_audits: tenants.reduce((s, t) => s + t.audit_count, 0),
    total_emails: tenants.reduce((s, t) => s + t.email_count, 0),
    active_tenants: tenants.filter(t => t.status === 'active').length
  }

  // 系统信息数据
  const sysInfo = systemInfo ?? {
    go_version: 'go1.22.0',
    build_version: 'dev',
    build_commit: 'none',
    build_at: '',
    build_os: '',
    memory_alloc: '128MB',
    goroutines: 42,
    start_time: new Date(Date.now() - 86400_000 * 7).toISOString(),
    cpu_usage: 23,
    memory_usage: 45,
    disk_usage: 32
  }

  // 资源使用率渲染
  const renderResourceBar = (percent: number, label: string) => {
    const cls = percent < 60 ? 'ok' : percent < 85 ? 'warn' : 'danger'
    return (
      <div key={label} style={{ marginBottom: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, marginBottom: 4 }}>
          <span style={{ color: 'var(--text-secondary)' }}>{label}</span>
          <strong>{percent}%</strong>
        </div>
        <div className="admin-resource-bar">
          <div className={`admin-resource-bar-fill admin-resource-bar-fill-${cls}`} style={{ width: `${percent}%` }} />
        </div>
      </div>
    )
  }

  return (
    <div className="admin-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('admin.title')}</h1>
          <p className="page-subtitle">{t('admin.subtitle')}</p>
          <div style={{ marginTop: 8, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            <span
              style={{
                fontSize: 12,
                padding: '2px 10px',
                borderRadius: 999,
                background: hasApiToken ? 'var(--status-success-bg)' : 'var(--status-danger-bg)',
                color: hasApiToken ? 'var(--status-success)' : 'var(--status-danger)'
              }}
            >
              {hasApiToken ? '✓ 已登录（账号体系）' : '⚠️ 未登录：管理功能不可用（需 Owner/Admin）'}
            </span>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button variant="secondary" size="sm" onClick={() => navigate('/admin/login', { state: { from: '/admin' } })}>
            🔑 登录
          </Button>
        </div>
      </div>

      <Tabs variant="pills">
        {/* Tab 1: 租户管理 */}
        <TabPane tabKey="tenants" tab={`🏢 ${t('admin.tabTenants')}`}>
          <div className="admin-filter-bar">
            <div className="admin-filter-item">
              <label>{t('admin.filterStatus')}</label>
              <select value={statusFilter} onChange={(e) => { setStatusFilter(e.target.value); setPage(1) }}>
                <option value="">{t('common.all')}</option>
                <option value="active">{t('admin.tenantActive')}</option>
                <option value="suspended">{t('admin.tenantSuspended')}</option>
                <option value="pending">{t('admin.tenantPending')}</option>
              </select>
            </div>
            <Button variant="secondary" size="sm" onClick={loadTenants}>🔄 {t('common.refresh')}</Button>
          </div>

          <Card compact>
            <Table
              columns={tenantColumns}
              dataSource={filteredTenants}
              rowKey="id"
              striped
              pagination
              pageSize={pageSize}
              emptyText={t('common.noData')}
            />
          </Card>
        </TabPane>

        {/* Tab 2: 用量统计 */}
        <TabPane tabKey="usage" tab={`📊 ${t('admin.tabUsage')}`}>
          <div className="admin-stats-grid">
            <div className="admin-stat-card">
              <div className="admin-stat-icon">🏢</div>
              <div className="admin-stat-label">{t('admin.usageTotalBrands')}</div>
              <div className="admin-stat-value">{statsData.total_brands}</div>
            </div>
            <div className="admin-stat-card">
              <div className="admin-stat-icon">🔍</div>
              <div className="admin-stat-label">{t('admin.usageTotalAudits')}</div>
              <div className="admin-stat-value">{statsData.total_audits}</div>
            </div>
            <div className="admin-stat-card">
              <div className="admin-stat-icon">📧</div>
              <div className="admin-stat-label">{t('admin.usageTotalEmails')}</div>
              <div className="admin-stat-value">{statsData.total_emails}</div>
            </div>
            <div className="admin-stat-card">
              <div className="admin-stat-icon">✅</div>
              <div className="admin-stat-label">{t('admin.usageActiveTenants')}</div>
              <div className="admin-stat-value">{statsData.active_tenants}</div>
            </div>
          </div>

          <Card title={t('admin.sysInfoTitle')} compact>
            <div className="admin-sysinfo-list">
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysBuildVersion')}</span>
                <span className="admin-sysinfo-val">{sysInfo.build_version}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysBuildCommit')}</span>
                <span className="admin-sysinfo-val" title={sysInfo.build_commit}>{sysInfo.build_commit}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysBuildTime')}</span>
                <span className="admin-sysinfo-val">{sysInfo.build_at ? new Date(sysInfo.build_at).toLocaleString() : '-'}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysBuildOS')}</span>
                <span className="admin-sysinfo-val">{sysInfo.build_os || '-'}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysGoVersion')}</span>
                <span className="admin-sysinfo-val">{sysInfo.go_version}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysMemory')}</span>
                <span className="admin-sysinfo-val">{sysInfo.memory_alloc}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysGoroutines')}</span>
                <span className="admin-sysinfo-val">{sysInfo.goroutines}</span>
              </div>
              <div className="admin-sysinfo-row">
                <span className="admin-sysinfo-key">{t('admin.sysStartTime')}</span>
                <span className="admin-sysinfo-val">{new Date(sysInfo.start_time).toLocaleString()}</span>
              </div>
            </div>
          </Card>
        </TabPane>

        {/* Tab 3: 公告管理 */}
        <TabPane tabKey="announcements" tab={`📢 ${t('admin.tabAnnouncements')}`}>
          <Card title={t('admin.announceCreateTitle')} compact>
            <div className="admin-announce-form">
              <Input
                label={t('admin.announceTitle')}
                value={announceForm.title}
                onChange={(e) => setAnnounceForm({ ...announceForm, title: e.target.value })}
                placeholder={t('admin.announceTitlePlaceholder')}
              />
              <div className="admin-filter-item">
                <label style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{t('admin.announceType')}</label>
                <select
                  value={announceForm.type}
                  onChange={(e) => setAnnounceForm({ ...announceForm, type: e.target.value })}
                  style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border-primary)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14 }}
                >
                  <option value="system">{t('admin.announceTypeSystem')}</option>
                  <option value="feature">{t('admin.announceTypeFeature')}</option>
                  <option value="maintenance">{t('admin.announceTypeMaintenance')}</option>
                </select>
              </div>
              <div className="admin-announce-form-full">
                <Textarea
                  label={t('admin.announceContent')}
                  value={announceForm.content}
                  onChange={(e) => setAnnounceForm({ ...announceForm, content: e.target.value })}
                  placeholder={t('admin.announceContentPlaceholder')}
                  rows={3}
                />
              </div>
              <Input
                label={t('admin.announceExpireAt')}
                type="datetime-local"
                value={announceForm.expire_at}
                onChange={(e) => setAnnounceForm({ ...announceForm, expire_at: e.target.value })}
              />
              <div style={{ display: 'flex', alignItems: 'flex-end' }}>
                <Button onClick={handleCreateAnnouncement} loading={submitting}>
                  📢 {t('admin.announceCreateBtn')}
                </Button>
              </div>
            </div>
          </Card>

          <Card title={t('admin.announceListTitle')} compact style={{ marginTop: 16 }}>
            <Table
              columns={announcementColumns}
              dataSource={announcements}
              rowKey="id"
              striped
              emptyText={t('common.noData')}
            />
          </Card>
        </TabPane>

        {/* Tab 4: 系统信息 */}
        <TabPane tabKey="system" tab={`🩺 ${t('admin.tabSystem')}`}>
          <div className="admin-grid-2">
            <Card title={t('admin.sysInfoTitle')} compact>
              <div className="admin-sysinfo-list">
                <div className="admin-sysinfo-row">
                  <span className="admin-sysinfo-key">{t('admin.sysGoVersion')}</span>
                  <span className="admin-sysinfo-val">{sysInfo.go_version}</span>
                </div>
                <div className="admin-sysinfo-row">
                  <span className="admin-sysinfo-key">{t('admin.sysMemory')}</span>
                  <span className="admin-sysinfo-val">{sysInfo.memory_alloc}</span>
                </div>
                <div className="admin-sysinfo-row">
                  <span className="admin-sysinfo-key">{t('admin.sysGoroutines')}</span>
                  <span className="admin-sysinfo-val">{sysInfo.goroutines}</span>
                </div>
                <div className="admin-sysinfo-row">
                  <span className="admin-sysinfo-key">{t('admin.sysStartTime')}</span>
                  <span className="admin-sysinfo-val">{new Date(sysInfo.start_time).toLocaleString()}</span>
                </div>
              </div>
            </Card>

            <Card title={t('admin.sysResourceTitle')} compact>
              {renderResourceBar(sysInfo.cpu_usage, t('admin.sysCpuUsage'))}
              {renderResourceBar(sysInfo.memory_usage, t('admin.sysMemoryUsage'))}
              {renderResourceBar(sysInfo.disk_usage, t('admin.sysDiskUsage'))}
            </Card>
          </div>
        </TabPane>

        {/* Tab 5: 系统设置（DB 变量存储，管理后台可改） */}
        <TabPane tabKey="settings" tab={`⚙️ ${t('admin.tabSettings')}`}>
          <SettingsTab />
        </TabPane>

        {/* Tab 6: 外部提交分析（外部系统提交的大模型对话 + 定时分析） */}
        <TabPane tabKey="ext-submissions" tab={`📥 ${t('admin.tabExternalSubmissions')}`}>
          <ExternalSubmissions />
        </TabPane>
      </Tabs>
    </div>
  )
}

export default Admin

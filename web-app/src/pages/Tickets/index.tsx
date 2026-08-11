import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Input, Textarea } from '@/components/Input'
import { Table, type TableColumn } from '@/components/Table'
import { Tabs, TabPane } from '@/components/Tabs'
import { Modal } from '@/components/Modal'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import '../Dashboard/Dashboard.scss'
import './Tickets.scss'

// 工单数据类型
interface TicketRow {
  id: string
  ticket_no: string
  subject: string
  category: string
  priority: string
  status: string
  created_at: string
  content?: string
  contact?: string
}

// 回复数据类型
interface ReplyItem {
  id: string
  author: string
  is_staff: boolean
  content: string
  created_at: string
}

// 模拟工单数据
const mockTickets: TicketRow[] = [
  { id: '1', ticket_no: 'TK-2026-001', subject: '审计报告导出 PDF 失败', category: 'bug', priority: 'high', status: 'open', created_at: new Date(Date.now() - 3600_000).toISOString(), content: '点击导出 PDF 后页面空白，控制台报错。', contact: 'user@example.com' },
  { id: '2', ticket_no: 'TK-2026-002', subject: '如何配置告警邮件收件人', category: 'usage', priority: 'medium', status: 'pending', created_at: new Date(Date.now() - 86400_000).toISOString(), content: '想在 BVS 评分低于阈值时收到邮件提醒。', contact: 'admin@example.com' },
  { id: '3', ticket_no: 'TK-2026-003', subject: '增加对 Kimi 引擎的支持', category: 'feature', priority: 'low', status: 'resolved', created_at: new Date(Date.now() - 86400_000 * 3).toISOString(), content: '希望审计能覆盖 Kimi 引擎。', contact: 'pm@example.com' },
  { id: '4', ticket_no: 'TK-2026-004', subject: '账单与套餐升级咨询', category: 'billing', priority: 'medium', status: 'closed', created_at: new Date(Date.now() - 86400_000 * 7).toISOString(), content: '从专业版升级到企业版的费用。', contact: 'finance@example.com' }
]

// 模拟回复数据
const mockReplies: Record<string, ReplyItem[]> = {
  '1': [
    { id: 'r1', author: 'user@example.com', is_staff: false, content: '点击导出 PDF 后页面空白，控制台报错。', created_at: new Date(Date.now() - 3600_000).toISOString() },
    { id: 'r2', author: '客服小张', is_staff: true, content: '您好，已收到您的反馈。请问使用的是哪个浏览器？建议先尝试 HTML 报告导出作为替代方案。', created_at: new Date(Date.now() - 1800_000).toISOString() }
  ]
}

const Tickets: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)

  // 工单列表状态
  const [tickets, setTickets] = useState<TicketRow[]>(mockTickets)
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [categoryFilter, setCategoryFilter] = useState<string>('')

  // 工单详情 Modal 状态
  const [detailVisible, setDetailVisible] = useState(false)
  const [currentTicket, setCurrentTicket] = useState<TicketRow | null>(null)
  const [replies, setReplies] = useState<ReplyItem[]>([])
  const [replyContent, setReplyContent] = useState('')
  const [replying, setReplying] = useState(false)

  // 提交工单表单状态
  const [createForm, setCreateForm] = useState({
    subject: '',
    category: 'bug',
    priority: 'medium',
    content: '',
    contact: ''
  })
  const [submitting, setSubmitting] = useState(false)

  // 加载工单列表
  const loadTickets = async () => {
    try {
      const res = await api.tickets.list({
        status: statusFilter || undefined,
        category: categoryFilter || undefined
      })
      if (res?.items) setTickets(res.items)
    } catch {
      // 保留模拟数据
    }
  }

  useEffect(() => {
    loadTickets()
  }, [statusFilter, categoryFilter])

  // 前端过滤
  const filteredTickets = tickets.filter(t => {
    if (statusFilter && t.status !== statusFilter) return false
    if (categoryFilter && t.category !== categoryFilter) return false
    return true
  })

  // 打开工单详情
  const handleOpenDetail = async (ticket: TicketRow) => {
    setCurrentTicket(ticket)
    setDetailVisible(true)
    setReplyContent('')
    try {
      const res = await api.tickets.detail(ticket.id)
      if (res?.replies) {
        setReplies(res.replies)
        return
      }
    } catch {
      // 回退到模拟数据
    }
    setReplies(mockReplies[ticket.id] ?? [])
  }

  // 提交回复
  const handleReply = async () => {
    if (!currentTicket || !replyContent.trim()) {
      showToast(t('tickets.replyEmpty'), 'warning')
      return
    }
    setReplying(true)
    const newReply: ReplyItem = {
      id: `r${Date.now()}`,
      author: 'me',
      is_staff: false,
      content: replyContent,
      created_at: new Date().toISOString()
    }
    try {
      await api.tickets.reply(currentTicket.id, replyContent)
      showToast(t('tickets.replySuccess'), 'success')
    } catch {
      showToast(t('tickets.replySuccessLocal'), 'info')
    }
    setReplies(prev => [...prev, newReply])
    setReplyContent('')
    setReplying(false)
  }

  // 更新工单状态
  const handleUpdateStatus = async (status: string) => {
    if (!currentTicket) return
    try {
      await api.tickets.updateStatus(currentTicket.id, status)
      showToast(t('tickets.statusUpdated'), 'success')
    } catch {
      showToast(t('tickets.statusUpdatedLocal'), 'info')
    }
    setTickets(prev => prev.map(t => t.id === currentTicket.id ? { ...t, status } : t))
    setCurrentTicket(prev => prev ? { ...prev, status } : prev)
  }

  // 提交工单
  const handleCreateTicket = async () => {
    if (!createForm.subject || !createForm.content) {
      showToast(t('tickets.createRequired'), 'warning')
      return
    }
    setSubmitting(true)
    const newTicket: TicketRow = {
      id: String(Date.now()),
      ticket_no: `TK-2026-${String(tickets.length + 1).padStart(3, '0')}`,
      subject: createForm.subject,
      category: createForm.category,
      priority: createForm.priority,
      status: 'open',
      created_at: new Date().toISOString(),
      content: createForm.content,
      contact: createForm.contact
    }
    try {
      await api.tickets.create(createForm)
      showToast(t('tickets.createSuccess'), 'success')
    } catch {
      showToast(t('tickets.createSuccessLocal'), 'info')
    }
    setTickets(prev => [newTicket, ...prev])
    setCreateForm({ subject: '', category: 'bug', priority: 'medium', content: '', contact: '' })
    setSubmitting(false)
  }

  // 状态徽章渲染
  const renderStatusBadge = (status: string) => {
    const labelMap: Record<string, string> = {
      open: t('tickets.statusOpen'),
      pending: t('tickets.statusPending'),
      resolved: t('tickets.statusResolved'),
      closed: t('tickets.statusClosed')
    }
    return <span className={`tickets-status-badge tickets-status-badge-${status}`}>● {labelMap[status] ?? status}</span>
  }

  // 优先级徽章渲染
  const renderPriorityBadge = (priority: string) => {
    const labelMap: Record<string, string> = {
      high: t('tickets.priorityHigh'),
      medium: t('tickets.priorityMedium'),
      low: t('tickets.priorityLow')
    }
    return <span className={`tickets-priority-badge tickets-priority-badge-${priority}`}>{labelMap[priority] ?? priority}</span>
  }

  // 工单列表列定义
  const columns: TableColumn<TicketRow>[] = [
    { key: 'ticket_no', title: t('tickets.colNo'), width: 140, dataIndex: 'ticket_no', sortable: true },
    { key: 'subject', title: t('tickets.colSubject'), dataIndex: 'subject' },
    {
      key: 'category',
      title: t('tickets.colCategory'),
      width: 100,
      render: (r) => (
        <span style={{ fontSize: 11, padding: '2px 8px', borderRadius: 4, background: 'var(--surface-tertiary)', color: 'var(--text-secondary)' }}>{r.category}</span>
      )
    },
    { key: 'priority', title: t('tickets.colPriority'), width: 90, render: (r) => renderPriorityBadge(r.priority) },
    { key: 'status', title: t('tickets.colStatus'), width: 110, render: (r) => renderStatusBadge(r.status) },
    { key: 'created_at', title: t('tickets.colCreatedAt'), width: 160, sortable: true, render: (r) => new Date(r.created_at).toLocaleString() },
    {
      key: 'action',
      title: '',
      width: 90,
      align: 'right',
      render: (r) => (
        <Button size="sm" variant="ghost" onClick={(e) => { e.stopPropagation(); handleOpenDetail(r) }}>
          {t('common.view')} →
        </Button>
      )
    }
  ]

  return (
    <div className="tickets-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('tickets.title')}</h1>
          <p className="page-subtitle">{t('tickets.subtitle')}</p>
        </div>
      </div>

      <Tabs variant="pills">
        {/* Tab 1: 我的工单 */}
        <TabPane tabKey="list" tab={`📋 ${t('tickets.tabMyTickets')}`}>
          <div className="tickets-filter-bar">
            <div className="tickets-filter-item">
              <label>{t('tickets.filterStatus')}</label>
              <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
                <option value="">{t('common.all')}</option>
                <option value="open">{t('tickets.statusOpen')}</option>
                <option value="pending">{t('tickets.statusPending')}</option>
                <option value="resolved">{t('tickets.statusResolved')}</option>
                <option value="closed">{t('tickets.statusClosed')}</option>
              </select>
            </div>
            <div className="tickets-filter-item">
              <label>{t('tickets.filterCategory')}</label>
              <select value={categoryFilter} onChange={(e) => setCategoryFilter(e.target.value)}>
                <option value="">{t('common.all')}</option>
                <option value="bug">{t('tickets.categoryBug')}</option>
                <option value="usage">{t('tickets.categoryUsage')}</option>
                <option value="feature">{t('tickets.categoryFeature')}</option>
                <option value="billing">{t('tickets.categoryBilling')}</option>
              </select>
            </div>
            <Button variant="secondary" size="sm" onClick={loadTickets}>🔄 {t('common.refresh')}</Button>
          </div>

          <Card compact>
            <Table
              columns={columns}
              dataSource={filteredTickets}
              rowKey="id"
              striped
              pagination
              pageSize={10}
              onRowClick={handleOpenDetail}
              emptyText={t('common.noData')}
            />
          </Card>
        </TabPane>

        {/* Tab 2: 提交工单 */}
        <TabPane tabKey="create" tab={`✏️ ${t('tickets.tabCreate')}`}>
          <Card title={t('tickets.createTitle')} subtitle={t('tickets.createSubtitle')} compact>
            <div className="tickets-form">
              <div className="tickets-form-full">
                <Input
                  label={t('tickets.colSubject')}
                  value={createForm.subject}
                  onChange={(e) => setCreateForm({ ...createForm, subject: e.target.value })}
                  placeholder={t('tickets.subjectPlaceholder')}
                  required
                />
              </div>
              <div className="tickets-filter-item">
                <label style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{t('tickets.colCategory')}</label>
                <select
                  value={createForm.category}
                  onChange={(e) => setCreateForm({ ...createForm, category: e.target.value })}
                  style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border-primary)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14 }}
                >
                  <option value="bug">{t('tickets.categoryBug')}</option>
                  <option value="usage">{t('tickets.categoryUsage')}</option>
                  <option value="feature">{t('tickets.categoryFeature')}</option>
                  <option value="billing">{t('tickets.categoryBilling')}</option>
                </select>
              </div>
              <div className="tickets-filter-item">
                <label style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{t('tickets.colPriority')}</label>
                <select
                  value={createForm.priority}
                  onChange={(e) => setCreateForm({ ...createForm, priority: e.target.value })}
                  style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid var(--border-primary)', background: 'var(--bg-primary)', color: 'var(--text-primary)', fontSize: 14 }}
                >
                  <option value="high">{t('tickets.priorityHigh')}</option>
                  <option value="medium">{t('tickets.priorityMedium')}</option>
                  <option value="low">{t('tickets.priorityLow')}</option>
                </select>
              </div>
              <div className="tickets-form-full">
                <Textarea
                  label={t('tickets.createContent')}
                  value={createForm.content}
                  onChange={(e) => setCreateForm({ ...createForm, content: e.target.value })}
                  placeholder={t('tickets.contentPlaceholder')}
                  rows={5}
                  required
                />
              </div>
              <Input
                label={t('tickets.createContact')}
                value={createForm.contact}
                onChange={(e) => setCreateForm({ ...createForm, contact: e.target.value })}
                placeholder={t('tickets.contactPlaceholder')}
              />
            </div>
            <div style={{ marginTop: 16, display: 'flex', justifyContent: 'flex-end' }}>
              <Button onClick={handleCreateTicket} loading={submitting}>
                ✉️ {t('tickets.createBtn')}
              </Button>
            </div>
          </Card>
        </TabPane>
      </Tabs>

      {/* 工单详情 Modal */}
      <Modal
        open={detailVisible}
        onClose={() => setDetailVisible(false)}
        title={currentTicket ? `#${currentTicket.ticket_no} ${currentTicket.subject}` : ''}
        size="lg"
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <div style={{ display: 'flex', gap: 8 }}>
              {currentTicket && currentTicket.status !== 'closed' && (
                <>
                  {currentTicket.status !== 'resolved' && (
                    <Button size="sm" variant="success" onClick={() => handleUpdateStatus('resolved')}>
                      {t('tickets.markResolved')}
                    </Button>
                  )}
                  <Button size="sm" variant="secondary" onClick={() => handleUpdateStatus('closed')}>
                    {t('tickets.markClosed')}
                  </Button>
                </>
              )}
            </div>
            <Button variant="secondary" onClick={() => setDetailVisible(false)}>
              {t('common.close')}
            </Button>
          </div>
        }
      >
        {currentTicket && (
          <>
            {/* 工单元信息 */}
            <div className="tickets-detail-meta">
              <div className="tickets-detail-meta-item">
                <span className="tickets-detail-meta-key">{t('tickets.colCategory')}</span>
                <span className="tickets-detail-meta-val">{currentTicket.category}</span>
              </div>
              <div className="tickets-detail-meta-item">
                <span className="tickets-detail-meta-key">{t('tickets.colPriority')}</span>
                <span>{renderPriorityBadge(currentTicket.priority)}</span>
              </div>
              <div className="tickets-detail-meta-item">
                <span className="tickets-detail-meta-key">{t('tickets.colStatus')}</span>
                <span>{renderStatusBadge(currentTicket.status)}</span>
              </div>
              <div className="tickets-detail-meta-item">
                <span className="tickets-detail-meta-key">{t('tickets.colCreatedAt')}</span>
                <span className="tickets-detail-meta-val">{new Date(currentTicket.created_at).toLocaleString()}</span>
              </div>
            </div>

            {/* 工单原始内容 */}
            {currentTicket.content && (
              <div style={{ padding: 12, borderRadius: 8, background: 'var(--surface-secondary)', marginBottom: 16, fontSize: 14, color: 'var(--text-primary)' }}>
                {currentTicket.content}
              </div>
            )}

            {/* 回复列表 */}
            <div className="tickets-replies">
              {replies.length === 0 ? (
                <div style={{ textAlign: 'center', color: 'var(--text-tertiary)', padding: 24 }}>{t('tickets.noReplies')}</div>
              ) : (
                replies.map(r => (
                  <div key={r.id} className={`tickets-reply-item ${r.is_staff ? 'is-staff' : ''}`}>
                    <div className="tickets-reply-header">
                      <span className="tickets-reply-author">
                        {r.is_staff ? '🎧 ' : '👤 '}{r.author}
                        {r.is_staff && <span style={{ marginLeft: 6, fontSize: 10, padding: '1px 6px', borderRadius: 4, background: 'var(--brand-primary)', color: 'white' }}>{t('tickets.staffBadge')}</span>}
                      </span>
                      <span>{new Date(r.created_at).toLocaleString()}</span>
                    </div>
                    <div className="tickets-reply-content">{r.content}</div>
                  </div>
                ))
              )}
            </div>

            {/* 回复输入框 */}
            {currentTicket.status !== 'closed' && (
              <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
                <div style={{ flex: 1 }}>
                  <Textarea
                    label={t('tickets.replyLabel')}
                    value={replyContent}
                    onChange={(e) => setReplyContent(e.target.value)}
                    placeholder={t('tickets.replyPlaceholder')}
                    rows={3}
                  />
                </div>
                <Button onClick={handleReply} loading={replying}>
                  💬 {t('tickets.replyBtn')}
                </Button>
              </div>
            )}
          </>
        )}
      </Modal>
    </div>
  )
}

export default Tickets

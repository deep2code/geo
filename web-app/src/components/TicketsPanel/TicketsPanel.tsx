import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import { Table, type TableColumn } from '@/components/Table'
import { Tabs, TabPane } from '@/components/Tabs'
import api from '@/services/api'
import './TicketsPanel.scss'

interface TicketRow {
  id: string
  ticket_no: string
  subject: string
  category: string
  priority: string
  status: string
  created_at: string
}

interface TicketsPanelProps {
  open: boolean
  onClose: () => void
}

const statusColors: Record<string, string> = {
  open: 'var(--status-info-bg)',
  pending: 'var(--status-warning-bg)',
  resolved: 'var(--status-success-bg)',
  closed: 'var(--text-tertiary)'
}

const statusText: Record<string, string> = {
  open: '待处理',
  pending: '处理中',
  resolved: '已解决',
  closed: '已关闭'
}

export const TicketsPanel: React.FC<TicketsPanelProps> = ({ open, onClose }) => {
  const { t } = useTranslation()
  const [tickets, setTickets] = useState<TicketRow[]>([])
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState('all')

  useEffect(() => {
    if (open) {
      loadTickets()
    }
  }, [open])

  const loadTickets = async () => {
    setLoading(true)
    try {
      const res = await api.tickets.list().catch(() => null)
      if (res?.tickets) {
        setTickets(res.tickets)
      }
    } finally {
      setLoading(false)
    }
  }

  const filteredTickets = activeTab === 'all' 
    ? tickets 
    : tickets.filter(t => t.status === activeTab)

  const columns: TableColumn<TicketRow>[] = [
    { key: 'ticket_no', title: '工单号', dataIndex: 'ticket_no', width: 120 },
    { key: 'subject', title: '主题', dataIndex: 'subject' },
    { key: 'priority', title: '优先级', dataIndex: 'priority', width: 80 },
    { 
      key: 'status', 
      title: '状态', 
      dataIndex: 'status', 
      width: 100,
      render: (r) => (
        <span style={{ 
          padding: '2px 8px', 
          borderRadius: 4, 
          fontSize: 12,
          background: statusColors[r.status] || 'var(--surface-secondary)',
          color: 'var(--text-primary)'
        }}>
          {statusText[r.status] || r.status}
        </span>
      )
    },
    { 
      key: 'created_at', 
      title: '创建时间', 
      dataIndex: 'created_at',
      width: 140,
      render: (r) => new Date(r.created_at).toLocaleString()
    },
  ]

  if (!open) return null

  return (
    <div className="tickets-panel-overlay" onClick={onClose}>
      <div className="tickets-panel" onClick={(e) => e.stopPropagation()}>
        <div className="tickets-panel-header">
          <h2 className="tickets-panel-title">🎫 工单中心</h2>
          <button className="tickets-panel-close" onClick={onClose}>×</button>
        </div>
        
        <div className="tickets-panel-content">
          <Tabs activeKey={activeTab} onChange={setActiveTab} variant="pills" size="sm">
            <TabPane tabKey="all" tab="全部" />
            <TabPane tabKey="open" tab="待处理" />
            <TabPane tabKey="pending" tab="处理中" />
            <TabPane tabKey="resolved" tab="已解决" />
          </Tabs>

          <div className="tickets-panel-table">
            <Table 
              columns={columns} 
              dataSource={filteredTickets} 
              rowKey="id" 
              striped
              loading={loading}
            />
          </div>

          {filteredTickets.length === 0 && !loading && (
            <div className="tickets-panel-empty">
              <span className="tickets-panel-empty-icon">📭</span>
              <p>暂无工单</p>
            </div>
          )}
        </div>

        <div className="tickets-panel-footer">
          <Button onClick={onClose} variant="ghost">关闭</Button>
        </div>
      </div>
    </div>
  )
}

export default TicketsPanel

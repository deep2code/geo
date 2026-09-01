import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { Modal } from '@/components/Modal'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'

// 配置项结构（与后端 config.Setting 对齐）
interface SettingItem {
  key: string
  value: string
  default_value: string
  description: string
  category: string
  type: string // string / bool / int / float / secret
  is_secret: boolean
  is_bootstrap: boolean
  requires_restart: boolean
  source: string // db / env / default / unset
}

interface CategoryInfo {
  name: string
  count: number
}

// 分类中文名（无 i18n 时兜底）
const CATEGORY_LABELS: Record<string, string> = {
  auth: '账号 / 认证',
  billing: '计费 / 支付',
  queue: '队列 / Redis',
  llm: 'LLM 引擎',
  engines: 'AI 引擎 API',
  chinacheck: '工商核验',
  offline: '离线工商库',
  history: '审计历史',
  admin: '系统管理',
  whitelabel: '白标定制',
  mail: '邮件',
  mcp: 'MCP',
  storage: '存储',
  server: '服务',
  test: '测试',
  general: '通用'
}

const SettingsTab: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)

  const [settings, setSettings] = useState<SettingItem[]>([])
  const [categories, setCategories] = useState<CategoryInfo[]>([])
  const [category, setCategory] = useState('')
  const [q, setQ] = useState('')
  const [loading, setLoading] = useState(false)

  // 编辑弹窗
  const [editing, setEditing] = useState<SettingItem | null>(null)
  const [editValue, setEditValue] = useState('')
  const [saving, setSaving] = useState(false)
  // 请求序号：丢弃过期响应，防搜索输入快照竞态
  const loadSeq = useRef(0)

  const load = useCallback(async () => {
    // 竞态保护：快速输入时多个请求并发，慢的旧响应可能后返回并覆盖新结果
    const reqId = ++loadSeq.current
    setLoading(true)
    try {
      const res = await api.admin.settings({ category: category || undefined, q: q || undefined })
      if (loadSeq.current !== reqId) return
      setSettings(res?.settings ?? [])
      setCategories(res?.categories ?? [])
    } catch {
      if (loadSeq.current !== reqId) return
      setSettings([])
      setCategories([])
    } finally {
      if (loadSeq.current === reqId) setLoading(false)
    }
  }, [category, q])

  useEffect(() => {
    load()
  }, [load])

  const openEdit = (item: SettingItem) => {
    setEditing(item)
    setEditValue('')
  }

  const handleSave = async () => {
    if (!editing) return
    setSaving(true)
    try {
      const res = await api.admin.updateSetting(editing.key, editValue)
      showToast(
        res?.unchanged ? t('admin.settingsUnchanged') : t('admin.settingsSaved'),
        'success'
      )
      if (res?.restart_required) {
        showToast(t('admin.settingsRestartHint'), 'info')
      }
      setEditing(null)
      load()
    } catch (e: any) {
      showToast(e?.message || t('admin.settingsSaveFailed'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleReset = async (item: SettingItem) => {
    if (!window.confirm(t('admin.settingsResetConfirm').replace('{key}', item.key))) return
    try {
      await api.admin.resetSetting(item.key)
      showToast(t('admin.settingsResetOk'), 'success')
      load()
    } catch (e: any) {
      showToast(e?.message || t('admin.settingsSaveFailed'), 'error')
    }
  }

  const sourceLabel = (s: string) => {
    const map: Record<string, string> = {
      db: t('admin.settingsSourceDb'),
      env: t('admin.settingsSourceEnv'),
      default: t('admin.settingsSourceDefault'),
      unset: t('admin.settingsSourceUnset')
    }
    return map[s] ?? s
  }

  const typeLabel = (s: string) => {
    const map: Record<string, string> = {
      secret: t('admin.settingsTypeSecret'),
      bool: t('admin.settingsTypeBool'),
      int: t('admin.settingsTypeInt'),
      float: t('admin.settingsTypeFloat'),
      string: t('admin.settingsTypeString')
    }
    return map[s] ?? s
  }

  // 按分类分组
  const groups: { name: string; items: SettingItem[] }[] = []
  for (const item of settings) {
    let g = groups.find(g => g.name === item.category)
    if (!g) {
      g = { name: item.category, items: [] }
      groups.push(g)
    }
    g.items.push(item)
  }

  return (
    <div className="admin-settings">
      <Card title={t('admin.settingsTitle')} compact>
        {/* 筛选区 */}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
          <Button
            size="sm"
            variant={category === '' ? 'primary' : 'ghost'}
            onClick={() => setCategory('')}
          >
            {t('admin.settingsCategoryAll')}
          </Button>
          {categories.map(c => (
            <Button
              key={c.name}
              size="sm"
              variant={category === c.name ? 'primary' : 'ghost'}
              onClick={() => setCategory(c.name)}
            >
              {CATEGORY_LABELS[c.name] ?? c.name} ({c.count})
            </Button>
          ))}
        </div>
        <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
          <Input
            placeholder={t('admin.settingsSearch')}
            value={q}
            onChange={e => setQ(e.target.value)}
            style={{ maxWidth: 320 }}
          />
          <Button size="sm" onClick={load}>{t('admin.settingsSearchBtn')}</Button>
        </div>

        {loading ? (
          <div className="admin-settings-empty">{t('admin.settingsLoading')}</div>
        ) : groups.length === 0 ? (
          <div className="admin-settings-empty">{t('admin.settingsEmpty')}</div>
        ) : (
          groups.map(g => (
            <div key={g.name} style={{ marginBottom: 20 }}>
              <h4 style={{ margin: '12px 0 8px', fontSize: 14, opacity: 0.75 }}>
                {CATEGORY_LABELS[g.name] ?? g.name}
              </h4>
              <table className="admin-settings-table">
                <thead>
                  <tr>
                    <th>{t('admin.settingsKey')}</th>
                    <th>{t('admin.settingsDescription')}</th>
                    <th>{t('admin.settingsValue')}</th>
                    <th>{t('admin.settingsSource')}</th>
                    <th style={{ width: 150 }}>{t('admin.settingsActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {g.items.map(item => (
                    <tr key={item.key}>
                      <td>
                        <code>{item.key}</code>
                        {item.is_bootstrap && (
                          <span className="admin-settings-tag" title={t('admin.settingsBootstrapTip')}>
                            {t('admin.settingsBootstrap')}
                          </span>
                        )}
                        {item.requires_restart && (
                          <span className="admin-settings-tag" title={t('admin.settingsRestartTip')}>
                            {t('admin.settingsRestart')}
                          </span>
                        )}
                      </td>
                      <td>
                        <div>{item.description || '—'}</div>
                        <div style={{ fontSize: 12, opacity: 0.6 }}>
                          {t('admin.settingsType')}: {typeLabel(item.type)}
                        </div>
                      </td>
                      <td>
                        {item.value ? (
                          <span className="admin-settings-val">{item.value}</span>
                        ) : (
                          <span style={{ opacity: 0.5 }}>—</span>
                        )}
                        {item.source === 'default' && (
                          <div style={{ fontSize: 12, opacity: 0.6 }}>
                            {t('admin.settingsDefaultVal')}: {item.default_value || '—'}
                          </div>
                        )}
                      </td>
                      <td>
                        <span className={`admin-settings-src src-${item.source}`}>
                          {sourceLabel(item.source)}
                        </span>
                      </td>
                      <td>
                        <Button size="sm" variant="ghost" onClick={() => openEdit(item)} disabled={item.is_bootstrap}>
                          {t('admin.settingsEdit')}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => handleReset(item)} disabled={item.is_bootstrap}>
                          {t('admin.settingsReset')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))
        )}
      </Card>

      {/* 编辑弹窗 */}
      <Modal
        open={!!editing}
        title={t('admin.settingsEditTitle')}
        onClose={() => setEditing(null)}
      >
        {editing && (
          <div>
            <div style={{ marginBottom: 8 }}>
              <code>{editing.key}</code>
            </div>
            <div style={{ fontSize: 13, opacity: 0.7, marginBottom: 12 }}>
              {editing.description || '—'}
            </div>
            <Input
              placeholder={
                editing.is_secret
                  ? t('admin.settingsSecretPlaceholder')
                  : editing.default_value || t('admin.settingsValuePlaceholder')
              }
              value={editValue}
              onChange={e => setEditValue(e.target.value)}
              type={editing.is_secret ? 'password' : 'text'}
              autoFocus
            />
            {editing.requires_restart && (
              <div style={{ marginTop: 8, fontSize: 12, color: '#f59e0b' }}>
                ⚠️ {t('admin.settingsRestartHint')}
              </div>
            )}
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
              <Button variant="ghost" onClick={() => setEditing(null)}>
                {t('admin.settingsCancel')}
              </Button>
              <Button variant="primary" onClick={handleSave} disabled={saving}>
                {saving ? t('admin.settingsSaving') : t('admin.settingsSave')}
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

export default SettingsTab

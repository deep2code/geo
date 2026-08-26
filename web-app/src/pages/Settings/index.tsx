import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Input, Button, Tabs, TabPane } from '@/components'
import { useAppStore, type ThemeMode, type UIDensity } from '@/store/useAppStore'
import { LANGUAGES, getCurrentLanguage, changeLanguage, type LanguageCode } from '@/i18n'
import api from '@/services/api'
import type { ReadyCheck } from '@/types/api'
import '../Dashboard/Dashboard.scss'

const Settings: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const theme = useAppStore(s => s.theme)
  const density = useAppStore(s => s.density)
  const apiBaseUrl = useAppStore(s => s.apiBaseUrl)
  const apiTimeout = useAppStore(s => s.apiTimeout)
  const setTheme = useAppStore(s => s.setTheme)
  const setDensity = useAppStore(s => s.setDensity)
  const setApiBaseUrl = useAppStore(s => s.setApiBaseUrl)
  const setApiTimeout = useAppStore(s => s.setApiTimeout)
  const resetSettings = useAppStore(s => s.resetSettings)
  const showToast = useAppStore(s => s.showToast)

  const [ready, setReady] = useState<ReadyCheck | null>(null)
  const [baseUrlInput, setBaseUrlInput] = useState(apiBaseUrl)
  const [timeoutInput, setTimeoutInput] = useState(apiTimeout)
  const currentLang = getCurrentLanguage()

  useEffect(() => {
    api.ready().then(setReady).catch(() => null)
  }, [])

  const densities: { v: UIDensity; icon: string; l: string }[] = [
    { v: 'compact', icon: '📐', l: t('settings.densities.compact') },
    { v: 'comfortable', icon: '🙂', l: t('settings.densities.comfortable') },
    { v: 'spacious', icon: '🌿', l: t('settings.densities.spacious') }
  ]

  const systemItems: { key: string; label: string; value?: string }[] = [
    { key: 'brand_engine', label: t('settings.brandEngine'), value: ready?.checks.brand_engine?.status },
    { key: 'history_db', label: t('settings.historyDB'), value: ready?.checks.history_db?.status },
    { key: 'offline_db', label: t('settings.offlineDB'), value: ready?.checks.offline_db?.status }
  ]
  const valueColor = (v?: string) => {
    if (v === 'ok') return 'success'
    if (v === 'disabled') return 'neutral'
    if (v === 'warn') return 'warning'
    if (v === 'fail') return 'error'
    if (v === 'unavailable') return 'error'
    return 'neutral'
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('settings.title')}</h1>
          <p className="page-subtitle">{t('settings.subtitle')}</p>
        </div>
      </div>

      <Tabs variant="pills">
        <TabPane tabKey="appearance" tab={`🎨 ${t('settings.appearance')}`}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Card title={t('settings.appearanceTheme')} compact>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
                {(['light', 'dark', 'brand'] as ThemeMode[]).map(tm => (
                  <button key={tm} type="button"
                    onClick={() => setTheme(tm)}
                    style={{
                      padding: 16, borderRadius: 12,
                      border: theme === tm ? '2px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: theme === tm ? 'color-mix(in srgb, var(--brand-primary) 8%, var(--surface-primary))' : 'var(--surface-secondary)',
                      cursor: 'pointer',
                      transition: 'all 0.2s'
                    }}>
                    <div style={{
                      width: '100%', height: 48,
                      borderRadius: 8,
                      marginBottom: 8,
                      background: tm === 'light'
                        ? 'linear-gradient(135deg, #ffffff 0%, #f8fafc 100%)'
                        : tm === 'dark'
                          ? 'linear-gradient(135deg, #0f172a 0%, #1e293b 100%)'
                          : 'linear-gradient(135deg, #1e1b4b 0%, #312e81 100%)',
                      border: '1px solid var(--border-primary)'
                    }} />
                    <div style={{ fontWeight: 600 }}>
                      {tm === 'light' ? '☀️ ' : tm === 'dark' ? '🌙 ' : '💎 '}
                      {t(`common.theme.${tm}`)}
                    </div>
                    {theme === tm && (
                      <div style={{ fontSize: 11, color: 'var(--brand-primary)', marginTop: 4 }}>✓ 当前</div>
                    )}
                  </button>
                ))}
              </div>
            </Card>

            <Card title={t('settings.appearanceLanguage')} compact>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
                {LANGUAGES.map(lng => (
                  <button key={lng.code} type="button"
                    onClick={() => changeLanguage(lng.code)}
                    style={{
                      padding: 16, borderRadius: 12, textAlign: 'center',
                      border: currentLang === lng.code ? '2px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: currentLang === lng.code ? 'color-mix(in srgb, var(--brand-primary) 8%, var(--surface-primary))' : 'var(--surface-secondary)',
                      cursor: 'pointer',
                      transition: 'all 0.2s'
                    }}>
                    <div style={{ fontSize: 28, marginBottom: 4 }}>{lng.flag}</div>
                    <div style={{ fontWeight: 600 }}>{lng.label}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>{lng.code}</div>
                    {currentLang === lng.code && (
                      <div style={{ fontSize: 11, color: 'var(--brand-primary)', marginTop: 4 }}>✓ 当前</div>
                    )}
                  </button>
                ))}
              </div>
            </Card>

            <Card title={t('settings.appearanceDensity')} compact style={{ gridColumn: 'span 2' }}>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
                {densities.map(d => (
                  <button key={d.v} type="button"
                    onClick={() => setDensity(d.v)}
                    style={{
                      padding: 16, borderRadius: 12,
                      border: density === d.v ? '2px solid var(--brand-primary)' : '1px solid var(--border-primary)',
                      background: density === d.v ? 'color-mix(in srgb, var(--brand-primary) 8%, var(--surface-primary))' : 'var(--surface-secondary)',
                      cursor: 'pointer',
                      transition: 'all 0.2s'
                    }}>
                    <div style={{ fontSize: 24, marginBottom: 6 }}>{d.icon}</div>
                    <div style={{ fontWeight: 600, marginBottom: 4 }}>{d.l}</div>
                    <div style={{ height: 32, display: 'flex', flexDirection: 'column', gap: d.v === 'compact' ? 1 : d.v === 'comfortable' ? 3 : 6, padding: 4, background: 'var(--surface-primary)', borderRadius: 6 }}>
                      <div style={{ height: 6, borderRadius: 3, background: 'var(--brand-primary)' }} />
                      <div style={{ height: 6, borderRadius: 3, background: 'var(--bg-active)' }} />
                      <div style={{ height: 6, borderRadius: 3, background: 'var(--bg-active)' }} />
                    </div>
                  </button>
                ))}
              </div>
            </Card>
          </div>
        </TabPane>

        <TabPane tabKey="api" tab="🔌 API">
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Card title={t('settings.api')} compact>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <Input
                  label={t('settings.apiBaseUrl')}
                  value={baseUrlInput}
                  onChange={(e) => setBaseUrlInput(e.target.value)}
                  placeholder="http://localhost:7070 （留空使用相对路径 /api）"
                />
                <Input
                  label={t('settings.apiTimeout')}
                  type="number"
                  value={timeoutInput}
                  onChange={(e) => setTimeoutInput(Number(e.target.value))}
                  suffix="秒"
                />
                <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                  <Button variant="secondary" onClick={() => { setBaseUrlInput(apiBaseUrl); setTimeoutInput(apiTimeout) }}>
                    撤销
                  </Button>
                  <Button onClick={() => {
                    setApiBaseUrl(baseUrlInput)
                    setApiTimeout(Math.max(10, timeoutInput || 120))
                    showToast('API 配置已保存', 'success')
                  }}>
                    💾 保存
                  </Button>
                </div>
              </div>
            </Card>

            <Card title={t('settings.apiEngines')} compact subtitle="在后端 GEO_*_KEY 环境变量中配置">
              <div style={{ overflow: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead style={{ background: 'var(--surface-secondary)' }}>
                    <tr>
                      <th style={{ textAlign: 'left', padding: '8px 10px', borderBottom: '1px solid var(--border-primary)' }}>{t('settings.engine')}</th>
                      <th style={{ textAlign: 'left', padding: '8px 10px', borderBottom: '1px solid var(--border-primary)' }}>Env Key</th>
                      <th style={{ textAlign: 'center', padding: '8px 10px', borderBottom: '1px solid var(--border-primary)' }}>{t('settings.configured')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {[
                      { e: 'ChatGPT', env: 'GEO_CHATGPT_KEY' },
                      { e: 'Claude', env: 'GEO_CLAUDE_KEY' },
                      { e: 'Perplexity', env: 'GEO_PERPLEXITY_KEY' },
                      { e: 'Gemini', env: 'GEO_GEMINI_KEY' },
                      { e: '通义千问', env: 'GEO_QWEN_KEY' },
                      { e: '智谱 GLM', env: 'GEO_GLM_KEY' },
                      { e: 'DeepSeek', env: 'GEO_DEEPSEEK_KEY' },
                      { e: 'Kimi', env: 'GEO_KIMI_KEY' },
                      { e: '文心一言', env: 'GEO_WENXIN_KEY' },
                      { e: '豆包', env: 'GEO_DOUBAO_KEY' },
                      { e: '小米', env: 'GEO_XIAOMI_KEY' },
                      { e: '讯飞星火', env: 'GEO_XUNFEI_KEY' },
                      { e: '元宝', env: 'GEO_YUANBAO_KEY' }
                    ].map((row, i) => (
                      <tr key={i}
                        onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--surface-secondary)')}
                        onMouseLeave={(e) => (e.currentTarget.style.background = '')}
                      >
                        <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--border-primary)', fontWeight: 500 }}>{row.e}</td>
                        <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--border-primary)', fontFamily: 'var(--font-family-mono)', fontSize: 11 }}>{row.env}</td>
                        <td style={{ padding: '8px 10px', borderBottom: '1px solid var(--border-primary)', textAlign: 'center' }}>
                          <span style={{
                            padding: '2px 8px', borderRadius: 999, fontSize: 11,
                            background: i % 3 === 0 ? 'var(--status-success-bg)' : 'var(--bg-tertiary)',
                            color: i % 3 === 0 ? 'var(--status-success)' : 'var(--text-tertiary)'
                          }}>
                            {i % 3 === 0 ? '✓ 已配置' : '未配置'}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          </div>
        </TabPane>

        <TabPane tabKey="pwa" tab="📱 PWA">
          <Card title={t('settings.pwa')} compact>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, alignItems: 'start' }}>
              <div style={{
                padding: 24, borderRadius: 12,
                border: '1px solid var(--border-primary)',
                background: 'linear-gradient(135deg, color-mix(in srgb, var(--brand-primary) 8%, var(--surface-primary)), var(--surface-secondary))'
              }}>
                <div style={{
                  width: 72, height: 72, borderRadius: 16,
                  background: 'linear-gradient(135deg, var(--brand-primary), var(--brand-secondary))',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  color: 'white', fontSize: 32, fontWeight: 700,
                  marginBottom: 16, boxShadow: 'var(--shadow-md)'
                }}>G</div>
                <div style={{ fontSize: 18, fontWeight: 700, marginBottom: 4 }}>崛起GEO</div>
                <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 16 }}>
                  AI 平台
                </div>
                <Button onClick={() => showToast('点击浏览器地址栏安装图标（→）安装', 'info')}>
                  🖥️ {t('settings.pwaInstall')}
                </Button>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <div style={{ padding: 12, borderRadius: 8, background: 'var(--status-success-bg)', color: 'var(--status-success)', fontSize: 13 }}>
                  ✓ PWA 已启用（vite-plugin-pwa，autoUpdate 模式）
                </div>
                <div style={{ padding: 12, borderRadius: 8, background: 'var(--surface-secondary)', fontSize: 13 }}>
                  <div style={{ fontWeight: 600, marginBottom: 6 }}>{t('settings.pwaUpdate')}</div>
                  <div style={{ color: 'var(--text-secondary)' }}>
                    新版本会自动在后台下载并激活。发现新版本时点击横幅刷新立即生效。
                  </div>
                </div>
                <div style={{ padding: 12, borderRadius: 8, background: 'var(--surface-secondary)', fontSize: 13 }}>
                  <div style={{ fontWeight: 600, marginBottom: 6 }}>Manifest</div>
                  <ul style={{ margin: 0, paddingLeft: 20, color: 'var(--text-secondary)', fontSize: 12 }}>
                    <li>独立模式 (standalone)</li>
                    <li>Theme-color 跟随系统深浅色（浅 #6366f1 / 深 #1e1b4b）</li>
                    <li>多尺寸 PWA 图标（/icons/*）</li>
                  </ul>
                </div>
              </div>
            </div>
          </Card>
        </TabPane>

        <TabPane tabKey="system" tab="🩺 系统">
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Card title={t('settings.system')} compact>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {systemItems.map(it => (
                  <div key={it.key} style={{
                    display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                    padding: 10, borderRadius: 8,
                    background: 'var(--surface-secondary)',
                    border: '1px solid var(--border-primary)'
                  }}>
                    <div style={{ fontWeight: 500 }}>{it.label}</div>
                    <span style={{
                      padding: '3px 10px', borderRadius: 999, fontSize: 11, fontWeight: 600,
                      background: valueColor(it.value) === 'success' ? 'var(--status-success-bg)'
                        : valueColor(it.value) === 'error' ? 'var(--status-error-bg)'
                          : valueColor(it.value) === 'warning' ? 'var(--status-warning-bg)'
                            : 'var(--bg-tertiary)',
                      color: valueColor(it.value) === 'success' ? 'var(--status-success)'
                        : valueColor(it.value) === 'error' ? 'var(--status-error)'
                          : valueColor(it.value) === 'warning' ? 'var(--status-warning)'
                            : 'var(--text-secondary)'
                    }}>
                      {it.value === 'ok' ? t('settings.ok') : it.value === 'disabled' ? t('settings.disabled') : it.value === 'warn' ? t('settings.warn') : it.value === 'fail' || it.value === 'unavailable' ? t('settings.unavailable') : '-'}
                    </span>
                  </div>
                ))}
                <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
                  <Button variant="secondary" size="sm" onClick={() => api.health().then(r => showToast(`${r.service} v${r.version}: ${r.status}`, 'success'))}>
                    🩺 {t('settings.systemHealth')}
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => api.ready().then(r => { setReady(r); showToast(r.status, r.status === 'ready' ? 'success' : 'warning') }) }>
                    ✅ {t('settings.systemReady')}
                  </Button>
                </div>
              </div>
            </Card>

            <Card title={t('settings.about')} compact>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <div style={{ display: 'flex', gap: 14, alignItems: 'center' }}>
                  <div style={{
                    width: 56, height: 56, borderRadius: 14,
                    background: 'linear-gradient(135deg, var(--brand-primary), var(--brand-secondary))',
                    color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 24, fontWeight: 700
                  }}>G</div>
                  <div>
                    <div style={{ fontSize: 18, fontWeight: 700 }}>崛起GEO</div>
                    <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>{t('settings.aboutVersion')}: 1.0.0 · {t('settings.aboutLicense')}: MIT</div>
                  </div>
                </div>
                <div style={{
                  padding: 12, borderRadius: 8,
                  background: 'var(--surface-secondary)',
                  color: 'var(--text-secondary)',
                  fontSize: 13, lineHeight: 1.6
                }}>
                  {t('settings.aboutDescription')}
                </div>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 6 }}>{t('settings.aboutLinks')}</div>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    {[
                      { k: 'src', l: t('settings.sourceCode'), icon: '💾' },
                      { k: 'docs', l: t('settings.docs'), icon: '📖' },
                      { k: 'issues', l: t('settings.issues'), icon: '🐛' }
                    ].map(l => (
                      <button key={l.k} type="button"
                        onClick={() => showToast(`${l.l} 链接（占位）`, 'info')}
                        style={{
                          padding: '6px 12px', borderRadius: 8, fontSize: 12,
                          border: '1px solid var(--border-primary)',
                          background: 'var(--surface-primary)',
                          color: 'var(--text-primary)',
                          cursor: 'pointer'
                        }}>
                        {l.icon} {l.l}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            </Card>
          </div>
        </TabPane>

        <TabPane tabKey="data-rights" tab="🛡️ 数据权利">
          <Card title="数据权利" subtitle="根据适用法律法规，您可对您的个人数据行使以下权利" compact>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16 }}>
              <div style={{
                padding: 20, borderRadius: 12,
                border: '1px solid var(--border-primary)',
                background: 'var(--surface-secondary)',
                display: 'flex', flexDirection: 'column', gap: 12
              }}>
                <div style={{ fontSize: 32 }}>🔍</div>
                <div style={{ fontSize: 16, fontWeight: 700 }}>访问我的数据</div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6, flex: 1 }}>
                  查询我们持有您的哪些个人信息，包括账户数据、品牌画像、使用记录等。
                </div>
                <Button onClick={async () => {
                  try {
                    const r = await api.legal.requestDataAccess()
                    showToast(
                      `✅ 请求受理成功 · 单号 ${r.request_id} · SLA ${r.sla}\n${r.note}`,
                      'info'
                    )
                  } catch (e: any) {
                    showToast(`提交失败：${e.message || e}`, 'error')
                  }
                }}>
                  🔍 提交访问请求
                </Button>
              </div>

              <div style={{
                padding: 20, borderRadius: 12,
                border: '1px solid var(--border-primary)',
                background: 'var(--surface-secondary)',
                display: 'flex', flexDirection: 'column', gap: 12
              }}>
                <div style={{ fontSize: 32 }}>📦</div>
                <div style={{ fontSize: 16, fontWeight: 700 }}>导出我的数据</div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6, flex: 1 }}>
                  以机器可读格式（JSON）导出您的全部个人数据副本，可迁移至其他服务。
                </div>
                <Button variant="secondary" onClick={async () => {
                  try {
                    const r = await api.legal.requestDataExport()
                    showToast(
                      `✅ 导出任务已创建 · 单号 ${r.request_id} · SLA ${r.sla}\n${r.note}`,
                      'success'
                    )
                  } catch (e: any) {
                    showToast(`提交失败：${e.message || e}`, 'error')
                  }
                }}>
                  📦 开始导出
                </Button>
              </div>

              <div style={{
                padding: 20, borderRadius: 12,
                border: '1px solid var(--border-primary)',
                background: 'linear-gradient(135deg, color-mix(in srgb, var(--status-error) 6%, var(--surface-secondary)), var(--surface-secondary))',
                display: 'flex', flexDirection: 'column', gap: 12
              }}>
                <div style={{ fontSize: 32 }}>🗑️</div>
                <div style={{ fontSize: 16, fontWeight: 700 }}>删除我的数据</div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.6, flex: 1 }}>
                  永久删除您的账户及全部关联数据（法律法规要求保留的除外）。此操作不可撤销。
                </div>
                <Button variant="danger" onClick={async () => {
                  if (!confirm('确定要删除您的全部个人数据吗？此操作不可撤销，账户将被永久注销。')) return
                  try {
                    const r = await api.legal.requestDataDelete()
                    showToast(
                      `⚠️ 删除请求已受理 · 单号 ${r.request_id} · SLA ${r.sla}\n${r.note}`,
                      'warning'
                    )
                  } catch (e: any) {
                    showToast(`提交失败：${e.message || e}`, 'error')
                  }
                }}>
                  🗑️ 请求删除
                </Button>
              </div>
            </div>
          </Card>
        </TabPane>
      </Tabs>

      <div style={{ marginTop: 32, paddingTop: 24, borderTop: '1px solid var(--border-primary)' }}>
        <div style={{ display: 'flex', justifyContent: 'center', gap: 16, marginBottom: 24, flexWrap: 'wrap' }}>
          <button
            type="button"
            onClick={() => navigate('/terms')}
            style={{
              background: 'none', border: 'none', padding: 0,
              color: 'var(--text-tertiary)', fontSize: 13, cursor: 'pointer'
            }}
          >
            服务条款
          </button>
          <button
            type="button"
            onClick={() => navigate('/privacy')}
            style={{
              background: 'none', border: 'none', padding: 0,
              color: 'var(--text-tertiary)', fontSize: 13, cursor: 'pointer'
            }}
          >
            隐私政策
          </button>
          <button
            type="button"
            onClick={() => navigate('/dpa')}
            style={{
              background: 'none', border: 'none', padding: 0,
              color: 'var(--text-tertiary)', fontSize: 13, cursor: 'pointer'
            }}
          >
            数据处理协议 (DPA)
          </button>
        </div>

        <div style={{ marginTop: 24, display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <Button variant="danger" onClick={() => {
            if (confirm('恢复默认设置？（主题、语言、API 配置等将被重置）')) {
              resetSettings()
              showToast('已恢复默认设置', 'success')
            }
          }}>
            ♻️ {t('settings.resetSettings')}
          </Button>
          <Button onClick={() => showToast('全部设置已保存', 'success')}>
            💾 {t('settings.saveSettings')}
          </Button>
        </div>
      </div>
    </div>
  )
}

export default Settings

import React, { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAppStore, type ThemeMode } from '@/store/useAppStore'
import { LANGUAGES, getCurrentLanguage, changeLanguage, type LanguageCode } from '@/i18n'
import { Button } from '@/components/Button'
import { Tabs, TabPane } from '@/components/Tabs'
import './Layout.scss'

const navItems: { key: string; to: string; icon: string; labelKey: string }[] = [
  { key: 'dashboard', to: '/dashboard', icon: '📊', labelKey: 'nav.dashboard' },
  { key: 'compare', to: '/compare', icon: '⚖️', labelKey: 'nav.compare' },
  { key: 'leaderboard', to: '/leaderboard', icon: '🏆', labelKey: 'nav.leaderboard' },
  { key: 'content', to: '/content-optimizer', icon: '✍️', labelKey: 'nav.contentOptimizer' },
  { key: 'brand-mgmt', to: '/brand-management', icon: '🏢', labelKey: 'nav.brandManagement' },
  { key: 'brand-audit', to: '/brand-audit', icon: '🔍', labelKey: 'nav.brandAudit' },
  { key: 'keywords', to: '/keyword-discovery', icon: '🔑', labelKey: 'nav.keywordDiscovery' },
  { key: 'report', to: '/report-export', icon: '📄', labelKey: 'nav.reportExport' },
  { key: 'alerts', to: '/alert-email', icon: '📧', labelKey: 'nav.alertEmail' },
  { key: 'settings', to: '/settings', icon: '⚙️', labelKey: 'nav.settings' },
  // 新增模块
  { key: 'help', to: '/help', icon: '❓', labelKey: 'nav.help' },
  { key: 'tickets', to: '/tickets', icon: '🎫', labelKey: 'nav.tickets' },
  { key: 'admin', to: '/admin', icon: '🛡️', labelKey: 'nav.admin' }
]

export const Layout: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const sidebarOpen = useAppStore(s => s.sidebarOpen)
  const setSidebarOpen = useAppStore(s => s.setSidebarOpen)
  const toast = useAppStore(s => s.toast)
  const clearToast = useAppStore(s => s.clearToast)
  const whitelabel = useAppStore(s => s.whitelabel)

  const currentLang = getCurrentLanguage()
  const [activeTabKey, setActiveTabKey] = useState('/dashboard')

  useEffect(() => {
    const current = navItems.find(n => location.pathname.startsWith(n.to))
    if (current) {
      setActiveTabKey(current.to)
    }
  }, [location.pathname])

  const handleThemeToggle = (t: ThemeMode) => {
    setTheme(t)
  }

  const handleLangChange = (code: LanguageCode) => {
    changeLanguage(code)
  }

  const handleTabChange = (key: string) => {
    setActiveTabKey(key)
    navigate(key)
  }

  return (
    <div className="app-layout">
      {sidebarOpen && (
        <div
          className="app-sidebar-backdrop"
          onClick={() => setSidebarOpen(false)}
          aria-hidden="true"
        />
      )}
      <aside className={`app-sidebar ${sidebarOpen ? 'is-open' : 'is-collapsed'}`}>
        <div className="app-sidebar-brand">
          <div className="app-logo">
            {whitelabel.logo_url ? (
              <img src={whitelabel.logo_url} alt={whitelabel.brand_name} className="app-logo-img" />
            ) : (
              <span className="app-logo-icon" style={{ background: whitelabel.primary_color }}>
                {whitelabel.brand_name.charAt(0)}
              </span>
            )}
            <span className="app-logo-text">{whitelabel.brand_name}</span>
          </div>
          <button
            type="button"
            className="app-sidebar-toggle"
            onClick={() => setSidebarOpen(!sidebarOpen)}
            aria-label="Toggle sidebar"
          >
            {sidebarOpen ? '«' : '»'}
          </button>
        </div>
        <nav className="app-sidebar-nav">
          {navItems.map(item => (
            <NavLink
              key={item.key}
              to={item.to}
              className={({ isActive }) => `app-nav-item ${isActive ? 'is-active' : ''}`}
              onClick={() => navigate(item.to)}
            >
              <span className="app-nav-icon">{item.icon}</span>
              {sidebarOpen && <span className="app-nav-label">{t(item.labelKey)}</span>}
            </NavLink>
          ))}
        </nav>
        <div className="app-sidebar-footer">
          <div className="app-sidebar-version">v1.0.0</div>
        </div>
      </aside>

      <div className="app-main">
        <header className="app-header">
          <div className="app-header-left">
            <button
              type="button"
              className="app-menu-toggle"
              onClick={() => setSidebarOpen(!sidebarOpen)}
              aria-label="Menu"
            >
              ☰
            </button>
            <h1 className="app-header-title" style={{ display: 'none' }}>{whitelabel.brand_name}</h1>
            <Tabs
              activeKey={activeTabKey}
              onChange={handleTabChange}
              variant="pills"
              size="sm"
              className="app-header-tabs"
            >
              {navItems.slice(0, 7).map(item => (
                <TabPane
                  key={item.to}
                  tabKey={item.to}
                  tab={<span><span style={{ marginRight: 4 }}>{item.icon}</span>{t(item.labelKey)}</span>}
                />
              ))}
            </Tabs>
          </div>
          <div className="app-header-right">
            <div className="app-theme-switcher" role="group" aria-label="Theme switcher">
              {(['light', 'dark', 'brand'] as ThemeMode[]).map(tm => (
                <button
                  key={tm}
                  type="button"
                  className={`app-theme-btn ${theme === tm ? 'is-active' : ''}`}
                  onClick={() => handleThemeToggle(tm)}
                  title={t(`common.theme.${tm}`)}
                >
                  {tm === 'light' ? '☀️' : tm === 'dark' ? '🌙' : '💎'}
                </button>
              ))}
            </div>
            <div className="app-lang-switcher">
              {LANGUAGES.map(lng => (
                <button
                  key={lng.code}
                  type="button"
                  className={`app-lang-btn ${currentLang === lng.code ? 'is-active' : ''}`}
                  onClick={() => handleLangChange(lng.code)}
                  title={lng.label}
                >
                  <span className="app-lang-flag">{lng.flag}</span>
                  <span className="app-lang-code">{lng.code.split('-')[0].toUpperCase()}</span>
                </button>
              ))}
            </div>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => navigate('/settings')}
              icon="⚙️"
            />
          </div>
        </header>

        <main className="app-content">
          <Outlet />
        </main>
      </div>

      {toast && (
        <div
          className={`app-toast app-toast-${toast.type}`}
          role="status"
          onClick={clearToast}
        >
          <span className="app-toast-icon">
            {toast.type === 'success' ? '✅' : toast.type === 'error' ? '❌' : toast.type === 'warning' ? '⚠️' : 'ℹ️'}
          </span>
          <span className="app-toast-message">{toast.message}</span>
          <button type="button" className="app-toast-close" onClick={clearToast}>×</button>
        </div>
      )}
    </div>
  )
}

export default Layout

import React from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/Button'
import './PublicShell.scss'

/**
 * 公开页外壳：Landing 首页风格的导航 + 内容区 + 页脚。
 * 用于帮助中心 / 服务条款 / 隐私政策 / DPA / 工单等公开页面，
 * 避免它们落入管理后台（Layout 侧边栏）布局。
 */
const PublicShell: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <div className="public-shell">
      {/* 顶部导航（同 Landing） */}
      <nav className="public-nav">
        <div className="public-nav-brand">
          <div className="public-nav-logo">G</div>
          <span>崛起GEO</span>
        </div>
        <div className="public-nav-links">
          <a href="/#features">{t('landing.navFeatures')}</a>
          <a href="/#how">{t('landing.navHow')}</a>
          <a href="/#pricing">{t('landing.navPricing')}</a>
          <a href="/help">{t('landing.navHelp')}</a>
        </div>
        <Button size="sm" onClick={() => navigate('/admin/login')}>{t('landing.navLogin')}</Button>
      </nav>

      {/* 内容区 */}
      <main className="public-content">{children}</main>

      {/* 页脚（同 Landing） */}
      <footer className="public-footer">
        <div className="public-footer-content">
          <div className="public-footer-copy">
            © 2026 崛起GEO. {t('landing.footerRights')}
          </div>
          <div className="public-footer-links">
            <a href="/#features">{t('landing.navFeatures')}</a>
            <a href="/#how">{t('landing.navHow')}</a>
            <a href="/#pricing">{t('landing.navPricing')}</a>
            <a href="/help">{t('landing.navHelp')}</a>
            <a href="/tickets">{t('landing.footerSupport')}</a>
            <a href="/terms">服务条款</a>
            <a href="/privacy">隐私政策</a>
            <a href="/dpa">DPA</a>
            <a href="/landing">{t('landing.footerAbout')}</a>
          </div>
        </div>
      </footer>
    </div>
  )
}

export default PublicShell

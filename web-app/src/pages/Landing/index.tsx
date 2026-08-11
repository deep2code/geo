import React, { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import api from '@/services/api'
import './Landing.scss'

// 功能亮点定义
interface Feature {
  icon: string
  titleKey: string
  descKey: string
}

const features: Feature[] = [
  { icon: '🎯', titleKey: 'landing.featureBvsTitle', descKey: 'landing.featureBvsDesc' },
  { icon: '🔍', titleKey: 'landing.featureAuditTitle', descKey: 'landing.featureAuditDesc' },
  { icon: '✍️', titleKey: 'landing.featureOptimizeTitle', descKey: 'landing.featureOptimizeDesc' },
  { icon: '🔑', titleKey: 'landing.featureKeywordTitle', descKey: 'landing.featureKeywordDesc' },
  { icon: '📊', titleKey: 'landing.featureCompareTitle', descKey: 'landing.featureCompareDesc' },
  { icon: '🏆', titleKey: 'landing.featureLeaderboardTitle', descKey: 'landing.featureLeaderboardDesc' },
  { icon: '📧', titleKey: 'landing.featureAlertTitle', descKey: 'landing.featureAlertDesc' },
  { icon: '📄', titleKey: 'landing.featureReportTitle', descKey: 'landing.featureReportDesc' }
]

// 定价方案定义
interface PricingPlan {
  id: string
  nameKey: string
  price: string
  unitKey: string
  descKey: string
  features: string[]
  featured: boolean
  ctaKey: string
}

const plans: PricingPlan[] = [
  {
    id: 'free',
    nameKey: 'landing.planFreeName',
    price: '¥0',
    unitKey: 'landing.planUnit',
    descKey: 'landing.planFreeDesc',
    features: ['3 个品牌', '每月 50 次审计', '基础报告导出', '社区支持'],
    featured: false,
    ctaKey: 'landing.planCtaStart'
  },
  {
    id: 'pro',
    nameKey: 'landing.planProName',
    price: '¥299',
    unitKey: 'landing.planUnit',
    descKey: 'landing.planProDesc',
    features: ['50 个品牌', '每月 2000 次审计', '全格式报告导出', '告警邮件监控', '内容优化器', '邮件工单支持'],
    featured: true,
    ctaKey: 'landing.planCtaStart'
  },
  {
    id: 'enterprise',
    nameKey: 'landing.planEnterpriseName',
    price: '¥定制',
    unitKey: 'landing.planCustom',
    descKey: 'landing.planEnterpriseDesc',
    features: ['无限品牌', '无限审计次数', '私有化部署', '专属客户经理', 'API 接入', '7×24 电话支持'],
    featured: false,
    ctaKey: 'landing.planCtaContact'
  }
]

const Landing: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [stats, setStats] = useState({
    brand_count: 12000,
    audit_count: 580000,
    engine_count: 13,
    user_count: 3500
  })

  // 加载平台数据
  const loadStats = async () => {
    try {
      const res = await api.landing.stats()
      if (res?.stats) setStats(res.stats)
    } catch {
      // 保留默认数据
    }
  }

  useEffect(() => {
    loadStats()
  }, [])

  // 跳转到控制台
  const handleStartFree = () => {
    navigate('/dashboard')
  }

  // 锚点滚动到定价区
  const handleScrollToPricing = () => {
    document.querySelector('#pricing')?.scrollIntoView({ behavior: 'smooth' })
  }

  return (
    <div className="landing-page">
      {/* 顶部导航栏 */}
      <nav className="landing-nav">
        <div className="landing-nav-brand">
          <div className="landing-nav-logo">G</div>
          <span>MyGEO</span>
        </div>
        <div className="landing-nav-links">
          <a href="#features">{t('landing.navFeatures')}</a>
          <a href="#pricing">{t('landing.navPricing')}</a>
          <a href="/help">{t('landing.navHelp')}</a>
        </div>
        <Button size="sm" onClick={handleStartFree}>{t('landing.navLogin')}</Button>
      </nav>

      {/* Hero 区域 */}
      <section className="landing-hero">
        <div className="landing-hero-badge">
          🚀 {t('landing.heroBadge')}
        </div>
        <h1 className="landing-hero-title">{t('landing.heroTitle')}</h1>
        <p className="landing-hero-subtitle">{t('landing.heroSubtitle')}</p>
        <div className="landing-hero-cta">
          <Button size="lg" onClick={handleStartFree}>✨ {t('landing.heroCtaStart')}</Button>
          <Button size="lg" variant="outline" onClick={handleScrollToPricing}>💰 {t('landing.heroCtaPricing')}</Button>
        </div>
      </section>

      {/* 平台数据统计 */}
      <section className="landing-stats">
        <div className="landing-stat-item">
          <div className="landing-stat-value">{(stats.brand_count / 1000).toFixed(1)}K+</div>
          <div className="landing-stat-label">{t('landing.statBrands')}</div>
        </div>
        <div className="landing-stat-item">
          <div className="landing-stat-value">{(stats.audit_count / 10000).toFixed(1)}万+</div>
          <div className="landing-stat-label">{t('landing.statAudits')}</div>
        </div>
        <div className="landing-stat-item">
          <div className="landing-stat-value">{stats.engine_count}+</div>
          <div className="landing-stat-label">{t('landing.statEngines')}</div>
        </div>
        <div className="landing-stat-item">
          <div className="landing-stat-value">{(stats.user_count / 1000).toFixed(1)}K+</div>
          <div className="landing-stat-label">{t('landing.statUsers')}</div>
        </div>
      </section>

      {/* 功能亮点 */}
      <section id="features" className="landing-features">
        <h2 className="landing-section-title">{t('landing.featuresTitle')}</h2>
        <p className="landing-section-subtitle">{t('landing.featuresSubtitle')}</p>
        <div className="landing-features-grid">
          {features.map((f, i) => (
            <div key={i} className="landing-feature-card">
              <div className="landing-feature-icon">{f.icon}</div>
              <div className="landing-feature-title">{t(f.titleKey)}</div>
              <div className="landing-feature-desc">{t(f.descKey)}</div>
            </div>
          ))}
        </div>
      </section>

      {/* 定价方案 */}
      <section id="pricing" className="landing-pricing">
        <h2 className="landing-section-title">{t('landing.pricingTitle')}</h2>
        <p className="landing-section-subtitle">{t('landing.pricingSubtitle')}</p>
        <div className="landing-pricing-grid">
          {plans.map(plan => (
            <div key={plan.id} className={`landing-pricing-card ${plan.featured ? 'is-featured' : ''}`}>
              {plan.featured && (
                <div className="landing-pricing-badge">{t('landing.planRecommended')}</div>
              )}
              <div className="landing-pricing-name">{t(plan.nameKey)}</div>
              <div className="landing-pricing-price">
                {plan.price}<span className="landing-pricing-price-unit"> / {t(plan.unitKey)}</span>
              </div>
              <div className="landing-pricing-desc">{t(plan.descKey)}</div>
              <ul className="landing-pricing-features">
                {plan.features.map((feat, i) => (
                  <li key={i}>{feat}</li>
                ))}
              </ul>
              <Button
                variant={plan.featured ? 'primary' : 'outline'}
                onClick={plan.id === 'enterprise' ? () => navigate('/tickets') : handleStartFree}
                style={{ width: '100%' }}
              >
                {t(plan.ctaKey)}
              </Button>
            </div>
          ))}
        </div>
      </section>

      {/* Footer */}
      <footer className="landing-footer">
        <div className="landing-footer-content">
          <div className="landing-footer-copy">
            © 2026 MyGEO. {t('landing.footerRights')}
          </div>
          <div className="landing-footer-links">
            <a href="#features">{t('landing.navFeatures')}</a>
            <a href="#pricing">{t('landing.navPricing')}</a>
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

export default Landing

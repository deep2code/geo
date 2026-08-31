import React, { useEffect, useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import api from '@/services/api'
import './Landing.scss'

// 自定义 CSS 变量注入（TS 安全）
const cssVars = (vars: Record<string, string | number>): React.CSSProperties =>
  vars as unknown as React.CSSProperties

// 滚动入场动画包裹
const Reveal: React.FC<{ children: React.ReactNode; className?: string; id?: string }> = ({
  children,
  className,
  id
}) => {
  const ref = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const obs = new IntersectionObserver(
      (entries) => entries.forEach((e) => {
        if (e.isIntersecting) {
          setShown(true)
          obs.disconnect()
        }
      }),
      { threshold: 0.15 }
    )
    obs.observe(el)
    return () => obs.disconnect()
  }, [])
  return (
    <div ref={ref} id={id} className={`${className ?? ''} ${shown ? 'reveal-in' : ''}`.trim()}>
      {children}
    </div>
  )
}

// 定价方案
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

// GEO 动作流程步骤
const geoSteps = [
  { key: 'crawl', titleKey: 'landing.stepCrawlTitle', descKey: 'landing.stepCrawlDesc', icon: '📡' },
  { key: 'analyze', titleKey: 'landing.stepAnalyzeTitle', descKey: 'landing.stepAnalyzeDesc', icon: '🔬' },
  { key: 'optimize', titleKey: 'landing.stepOptimizeTitle', descKey: 'landing.stepOptimizeDesc', icon: '⚡' },
  { key: 'monitor', titleKey: 'landing.stepMonitorTitle', descKey: 'landing.stepMonitorDesc', icon: '📊' }
]

// 功能亮点
const features = [
  { icon: '🔍', titleKey: 'landing.featureAuditTitle', descKey: 'landing.featureAuditDesc' },
  { icon: '📝', titleKey: 'landing.featureOptimizerTitle', descKey: 'landing.featureOptimizerDesc' },
  { icon: '📊', titleKey: 'landing.featureReportTitle', descKey: 'landing.featureReportDesc' },
  { icon: '🔔', titleKey: 'landing.featureAlertTitle', descKey: 'landing.featureAlertDesc' },
  { icon: '🏆', titleKey: 'landing.featureLeaderboardTitle', descKey: 'landing.featureLeaderboardDesc' },
  { icon: '🔗', titleKey: 'landing.featureIntegrationsTitle', descKey: 'landing.featureIntegrationsDesc' }
]

// GEO 步骤动画图标
const GeoStepGlyph: React.FC<{ kind: string }> = ({ kind }) => {
  switch (kind) {
    case 'crawl':
      return (
        <svg viewBox="0 0 64 64" className="geo-glyph">
          <circle cx="32" cy="32" r="26" className="geo-ring" />
          <circle cx="32" cy="32" r="16" className="geo-ring geo-ring--2" />
          <line x1="32" y1="32" x2="32" y2="8" className="geo-sweep" />
          <circle cx="32" cy="32" r="3" className="geo-dot" />
        </svg>
      )
    case 'analyze':
      return (
        <svg viewBox="0 0 64 64" className="geo-glyph">
          <rect x="14" y="34" width="10" height="18" className="geo-bar geo-bar--1" />
          <rect x="27" y="24" width="10" height="28" className="geo-bar geo-bar--2" />
          <rect x="40" y="16" width="10" height="36" className="geo-bar geo-bar--3" />
        </svg>
      )
    case 'optimize':
      return (
        <svg viewBox="0 0 64 64" className="geo-glyph">
          <path d="M32 8 L56 32 L32 56 L8 32 Z" className="geo-diamond" />
          <path d="M20 32 L28 40 L44 24" className="geo-check" />
        </svg>
      )
    case 'monitor':
      return (
        <svg viewBox="0 0 64 64" className="geo-glyph">
          <circle cx="32" cy="32" r="24" className="geo-ring" />
          <circle cx="32" cy="32" r="4" className="geo-dot" />
          <path d="M32 8 A24 24 0 0 1 56 32" className="geo-arc" />
        </svg>
      )
    default:
      return null
  }
}

const Landing: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [stats, setStats] = useState({
    brand_count: 12000,
    audit_count: 580000,
    engine_count: 13,
    user_count: 3500
  })
  const [meta, setMeta] = useState<{ build_version: string; build_commit: string; build_at: string } | null>(null)

  const loadStats = async () => {
    try {
      const res = await api.landing.stats()
      if (res?.stats) setStats(res.stats)
    } catch {}
  }

  const loadMeta = async () => {
    try {
      const res = await api.metaSystem()
      if (res?.build_at || res?.build_commit) {
        setMeta({
          build_version: res.build_version || '',
          build_commit: res.build_commit || '',
          build_at: res.build_at || ''
        })
      }
    } catch {}
  }

  useEffect(() => {
    loadStats()
    loadMeta()
  }, [])

  const handleStartFree = () => navigate('/dashboard')
  const handleScrollTo = (id: string) => {
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth' })
  }

  return (
    <div className="landing-page">
      {/* 顶部导航栏 */}
      <nav className="landing-nav">
        <div className="landing-nav-brand">
          <div className="landing-nav-logo">G</div>
          <span>崛起GEO</span>
        </div>
        <div className="landing-nav-links">
          <a href="#features" onClick={(e) => { e.preventDefault(); handleScrollTo('features') }}>{t('landing.navFeatures')}</a>
          <a href="#how" onClick={(e) => { e.preventDefault(); handleScrollTo('how') }}>{t('landing.navHow')}</a>
          <a href="#pricing" onClick={(e) => { e.preventDefault(); handleScrollTo('pricing') }}>{t('landing.navPricing')}</a>
          <a href="#contact" onClick={(e) => { e.preventDefault(); handleScrollTo('contact') }}>{t('landing.navContact')}</a>
          <a href="/help">{t('landing.navHelp')}</a>
        </div>
        <Button size="sm" onClick={handleStartFree}>{t('landing.navLogin')}</Button>
      </nav>

      {/* Hero 区域 */}
      <section className="landing-hero">
        <div className="landing-hero-bg" />
        <div className="landing-hero-content">
          <div className="landing-hero-badge">🚀 {t('landing.heroBadge')}</div>
          <h1 className="landing-hero-title">{t('landing.heroTitle')}</h1>
          <p className="landing-hero-subtitle">{t('landing.heroSubtitle')}</p>
          <div className="landing-hero-cta">
            <Button size="lg" onClick={handleStartFree}>✨ {t('landing.heroCtaStart')}</Button>
            <Button size="lg" variant="outline" onClick={() => handleScrollTo('pricing')}>💰 {t('landing.heroCtaPricing')}</Button>
          </div>
        </div>
      </section>

      {/* 平台数据统计 */}
      <section className="landing-stats">
        <div className="landing-stats-inner">
          <div className="landing-stat-item">
            <div className="landing-stat-value">{(stats.brand_count / 1000).toFixed(1)}K+</div>
            <div className="landing-stat-label">{t('landing.statBrands')}</div>
          </div>
          <div className="landing-stat-divider" />
          <div className="landing-stat-item">
            <div className="landing-stat-value">{(stats.audit_count / 10000).toFixed(1)}万+</div>
            <div className="landing-stat-label">{t('landing.statAudits')}</div>
          </div>
          <div className="landing-stat-divider" />
          <div className="landing-stat-item">
            <div className="landing-stat-value">{stats.engine_count}+</div>
            <div className="landing-stat-label">{t('landing.statEngines')}</div>
          </div>
          <div className="landing-stat-divider" />
          <div className="landing-stat-item">
            <div className="landing-stat-value">{(stats.user_count / 1000).toFixed(1)}K+</div>
            <div className="landing-stat-label">{t('landing.statUsers')}</div>
          </div>
        </div>
      </section>

      {/* 功能亮点 */}
      <Reveal id="features" className="landing-features">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.featuresTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.featuresSubtitle')}</p>
        </div>
        <div className="landing-features-grid">
          {features.map((f, i) => (
            <div key={i} className="landing-feature-card">
              <div className="landing-feature-icon">{f.icon}</div>
              <div className="landing-feature-title">{t(f.titleKey)}</div>
              <div className="landing-feature-desc">{t(f.descKey)}</div>
            </div>
          ))}
        </div>
      </Reveal>

      {/* GEO 运作流程 */}
      <Reveal id="how" className="landing-how">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.howTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.howSubtitle')}</p>
        </div>
        <div className="landing-how-flow">
          {geoSteps.map((s, i) => (
            <React.Fragment key={s.key}>
              <div className="landing-how-step">
                <div className="landing-how-step-num">{i + 1}</div>
                <div className="landing-how-step-icon">{s.icon}</div>
                <div className="landing-how-step-title">{t(s.titleKey)}</div>
                <div className="landing-how-step-desc">{t(s.descKey)}</div>
              </div>
              {i < geoSteps.length - 1 && (
                <div className="landing-how-arrow" aria-hidden>→</div>
              )}
            </React.Fragment>
          ))}
        </div>
        <div className="landing-how-visual">
          <div className="landing-how-query">
            <span className="landing-how-query-label">{t('landing.howQueryLabel')}</span>
            <span className="landing-how-query-text">{t('landing.howQueryText')}</span>
          </div>
          <div className="landing-how-pulse" aria-hidden>
            <span className="landing-how-pulse-dot" />
            <span className="landing-how-pulse-ring" />
            <span className="landing-how-pulse-ring landing-how-pulse-ring--2" />
          </div>
          <div className="landing-how-result">
            <span className="landing-how-result-brand">崛起GEO</span>
            <span className="landing-how-result-text">{t('landing.howResultText')}</span>
          </div>
        </div>
      </Reveal>

      {/* 优化前后对比 */}
      <Reveal className="landing-beforeafter">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.baTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.baSubtitle')}</p>
        </div>
        <div className="landing-ba-grid">
          <div className="landing-ba-card landing-ba-card--before">
            <div className="landing-ba-tag">{t('landing.beforeLabel')}</div>
            <div className="landing-ba-query">{t('landing.baQuery')}</div>
            <div className="landing-ba-answer">{t('landing.baAnswerBefore')}</div>
            <div className="landing-ba-metric">
              <div className="landing-ba-metric-head">
                <span>{t('landing.baMention')}</span>
                <span className="landing-ba-metric-value">18%</span>
              </div>
              <div className="landing-ba-bar">
                <span className="landing-ba-bar-fill landing-ba-bar-fill--before" style={cssVars({ '--target': '18%' })} />
              </div>
            </div>
          </div>
          <div className="landing-ba-card landing-ba-card--after">
            <div className="landing-ba-tag">{t('landing.afterLabel')}</div>
            <div className="landing-ba-query">{t('landing.baQuery')}</div>
            <div className="landing-ba-answer">
              {t('landing.baAnswerAfterPrefix')}
              <mark className="geo-cite">示例科技</mark>
              {t('landing.baAnswerAfterSuffix')}
            </div>
            <div className="landing-ba-metric">
              <div className="landing-ba-metric-head">
                <span>{t('landing.baMention')}</span>
                <span className="landing-ba-metric-value">92%</span>
              </div>
              <div className="landing-ba-bar">
                <span className="landing-ba-bar-fill landing-ba-bar-fill--after" style={cssVars({ '--target': '92%' })} />
              </div>
            </div>
          </div>
        </div>
        <div className="landing-ba-uplift">{t('landing.baUplift')}</div>
      </Reveal>

      {/* AI 引用友好数据 */}
      <Reveal className="landing-facts">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.factsTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.factsSubtitle')}</p>
        </div>
        <blockquote className="landing-facts-quote">
          "{t('landing.factsQuote')}"
        </blockquote>
        <div className="landing-facts-table-wrap">
          <table className="landing-facts-table">
            <thead>
              <tr>
                <th>{t('landing.factsTactic')}</th>
                <th>{t('landing.factsGain')}</th>
              </tr>
            </thead>
            <tbody>
              <tr><td>{t('landing.factsT1')}</td><td><strong>{t('landing.factsG41')}</strong></td></tr>
              <tr><td>{t('landing.factsT2')}</td><td><strong>{t('landing.factsG33')}</strong></td></tr>
              <tr><td>{t('landing.factsT3')}</td><td><strong>{t('landing.factsG29')}</strong></td></tr>
              <tr><td>{t('landing.factsT4')}</td><td><strong>{t('landing.factsG27')}</strong></td></tr>
              <tr><td>{t('landing.factsT5')}</td><td><strong>{t('landing.factsG25')}</strong></td></tr>
              <tr><td>{t('landing.factsT6')}</td><td><strong>{t('landing.factsG24')}</strong></td></tr>
              <tr><td>{t('landing.factsT7')}</td><td><strong>{t('landing.factsG22')}</strong></td></tr>
              <tr><td>{t('landing.factsT8')}</td><td><strong>{t('landing.factsG20')}</strong></td></tr>
              <tr><td>{t('landing.factsT9')}</td><td><strong>{t('landing.factsG18')}</strong></td></tr>
            </tbody>
          </table>
          <div className="landing-facts-bvs">
            <div className="landing-facts-bvs-title">{t('landing.factsBvsTitle')}</div>
            <p className="landing-facts-bvs-text">{t('landing.factsBvsText')}</p>
          </div>
        </div>
      </Reveal>

      {/* 定价方案 */}
      <section id="pricing" className="landing-pricing">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.pricingTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.pricingSubtitle')}</p>
        </div>
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

      {/* 联系我们 */}
      <section id="contact" className="landing-contact">
        <div className="landing-section-header">
          <h2 className="landing-section-title">{t('landing.contactTitle')}</h2>
          <p className="landing-section-subtitle">{t('landing.contactSubtitle')}</p>
        </div>
        <div className="landing-contact-card">
          <div className="landing-contact-item">
            <div className="landing-contact-icon">📧</div>
            <div className="landing-contact-label">{t('landing.contactEmail')}</div>
            <a href="mailto:deep2code@aliyun.com" className="landing-contact-value">deep2code@aliyun.com</a>
          </div>
          <div className="landing-contact-item">
            <div className="landing-contact-icon">🔧</div>
            <div className="landing-contact-label">{t('landing.contactCustom')}</div>
            <div className="landing-contact-value">{t('landing.contactCustomDesc')}</div>
          </div>
          <div className="landing-contact-item">
            <div className="landing-contact-icon">📋</div>
            <div className="landing-contact-label">{t('landing.contactVersion')}</div>
            <div className="landing-contact-value">
              {meta ? (
                <>版本 {meta.build_version} · 编译 {meta.build_at}</>
              ) : (
                '加载中...'
              )}
            </div>
          </div>
        </div>
        <Button size="lg" onClick={() => window.location.href = 'mailto:deep2code@aliyun.com'}>
          ✉️ {t('landing.contactCta')}
        </Button>
      </section>

      {/* Footer */}
      <footer className="landing-footer">
        <div className="landing-footer-content">
          <div className="landing-footer-copy">
            © 2026 崛起GEO. {t('landing.footerRights')}
          </div>
          <div className="landing-footer-links">
            <a href="#features" onClick={(e) => { e.preventDefault(); handleScrollTo('features') }}>{t('landing.navFeatures')}</a>
            <a href="#how" onClick={(e) => { e.preventDefault(); handleScrollTo('how') }}>{t('landing.navHow')}</a>
            <a href="#pricing" onClick={(e) => { e.preventDefault(); handleScrollTo('pricing') }}>{t('landing.navPricing')}</a>
            <a href="/help">{t('landing.navHelp')}</a>
            <a href="/support">{t('landing.footerSupport')}</a>
            <a href="/terms">服务条款</a>
            <a href="/privacy">隐私政策</a>
            <a href="#contact" onClick={(e) => { e.preventDefault(); handleScrollTo('contact') }}>{t('landing.footerContact')}</a>
          </div>
        </div>
        {meta && (
          <div className="landing-footer-version">
            <span className="landing-footer-version-item">版本 {meta.build_version || '-'}</span>
            <span className="landing-footer-version-item">编译 {meta.build_at || '-'}</span>
            <span className="landing-footer-version-item">
              commit{' '}
              <code className="landing-footer-version-commit">
                {meta.build_commit ? meta.build_commit.slice(0, 12) : '-'}
              </code>
            </span>
          </div>
        )}
      </footer>
    </div>
  )
}

export default Landing

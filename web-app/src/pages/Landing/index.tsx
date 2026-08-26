import React, { useEffect, useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import api from '@/services/api'
import './Landing.scss'

// 自定义 CSS 变量注入（TS 安全）
const cssVars = (vars: Record<string, string | number>): React.CSSProperties =>
  vars as unknown as React.CSSProperties

// 滚动入场动画包裹：进入视口后添加 reveal-in，触发内部 CSS 动画
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

// GEO 动作流程步骤
interface GeoStep {
  key: string
  titleKey: string
  descKey: string
}

const geoSteps: GeoStep[] = [
  { key: 'crawl', titleKey: 'landing.stepCrawlTitle', descKey: 'landing.stepCrawlDesc' },
  { key: 'analyze', titleKey: 'landing.stepAnalyzeTitle', descKey: 'landing.stepAnalyzeDesc' },
  { key: 'optimize', titleKey: 'landing.stepOptimizeTitle', descKey: 'landing.stepOptimizeDesc' },
  { key: 'monitor', titleKey: 'landing.stepMonitorTitle', descKey: 'landing.stepMonitorDesc' }
]

// 报告示例引擎覆盖样例
const sampleEngines = [
  { name: 'ChatGPT', mention: 92, citation: 78 },
  { name: 'Perplexity', mention: 88, citation: 81 },
  { name: 'Gemini', mention: 85, citation: 73 },
  { name: 'Claude', mention: 80, citation: 69 },
  { name: 'DeepSeek', mention: 90, citation: 84 },
  { name: '豆包', mention: 76, citation: 65 }
]

// 报告示例查询命中
const samplePrompts = [
  { prompt: '最好的 CRM 软件推荐', cited: true },
  { prompt: '中小企业客户管理系统对比', cited: true },
  { prompt: '国内 SaaS CRM 排行榜', cited: false }
]

// 报告示例行动建议
const sampleActions = [
  '为「客户案例」页补充结构化数据（JSON-LD），提升实体识别',
  '在「价格」页增加对比维度，增强 AI 引用说服力',
  '发布《中小企业 CRM 选型指南》长文，覆盖高频查询'
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
          <path d="M32 12 l4 14 14 4 -14 4 -4 14 -4 -14 -14 -4 14 -4z" className="geo-spark geo-spark--1" />
          <path d="M48 38 l2 7 7 2 -7 2 -2 7 -2 -7 -7 -2 7 -2z" className="geo-spark geo-spark--2" />
        </svg>
      )
    case 'monitor':
      return (
        <svg viewBox="0 0 64 64" className="geo-glyph">
          <path d="M32 10 L52 18 V34 C52 46 42 54 32 58 C22 54 12 46 12 34 V18 Z" className="geo-shield" />
          <circle cx="32" cy="32" r="5" className="geo-pulse" />
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
          <span>崛起GEO</span>
        </div>
        <div className="landing-nav-links">
          <a href="#features">{t('landing.navFeatures')}</a>
          <a href="#how">{t('landing.navHow')}</a>
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

      {/* GEO 动作流程动画 */}
      <Reveal id="how" className="landing-how">
        <h2 className="landing-section-title">{t('landing.howTitle')}</h2>
        <p className="landing-section-subtitle">{t('landing.howSubtitle')}</p>
        <div className="landing-how-flow">
          {geoSteps.map((s, i) => (
            <React.Fragment key={s.key}>
              <div className="landing-how-step">
                <div className="landing-how-icon">
                  <GeoStepGlyph kind={s.key} />
                </div>
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

      {/* GEO 优化前 / 优化后对比 */}
      <Reveal className="landing-beforeafter">
        <h2 className="landing-section-title">{t('landing.baTitle')}</h2>
        <p className="landing-section-subtitle">{t('landing.baSubtitle')}</p>
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

      {/* 报告示例 */}
      <Reveal className="landing-report">
        <h2 className="landing-section-title">{t('landing.reportTitle')}</h2>
        <p className="landing-section-subtitle">{t('landing.reportSubtitle')}</p>
        <div className="landing-report-card">
          <div className="landing-report-head">
            <div className="landing-report-brand">
              <div className="landing-report-brand-name">{t('landing.reportBrandDemo')}</div>
              <div className="landing-report-brand-sub">{t('landing.reportSubDemo')}</div>
            </div>
            <div className="landing-report-gauge">
              <svg viewBox="0 0 120 120" className="gauge-svg">
                <circle cx="60" cy="60" r="52" className="gauge-bg" />
                <circle cx="60" cy="60" r="52" className="gauge-fg" transform="rotate(-90 60 60)" style={cssVars({ '--c': 326.7, '--pct': 82 })} />
              </svg>
              <div className="gauge-center">
                <div className="gauge-num">82</div>
                <div className="gauge-grade">{t('landing.reportGrade')}</div>
              </div>
            </div>
          </div>

          <div className="landing-report-section">
            <div className="landing-report-section-title">{t('landing.reportEngineTitle')}</div>
            <div className="landing-report-engines">
              {sampleEngines.map((e) => (
                <div className="landing-report-engine" key={e.name}>
                  <div className="landing-report-engine-name">{e.name}</div>
                  <div className="landing-report-engine-bars">
                    <div className="landing-report-bar landing-report-bar--mention">
                      <span style={cssVars({ '--target': `${e.mention}%` })} />
                      <i>{t('landing.reportMention')} {e.mention}%</i>
                    </div>
                    <div className="landing-report-bar landing-report-bar--citation">
                      <span style={cssVars({ '--target': `${e.citation}%` })} />
                      <i>{t('landing.reportCitation')} {e.citation}%</i>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="landing-report-section">
            <div className="landing-report-section-title">{t('landing.reportPromptTitle')}</div>
            <div className="landing-report-prompts">
              {samplePrompts.map((p) => (
                <div className="landing-report-prompt" key={p.prompt}>
                  <span className="landing-report-prompt-text">{p.prompt}</span>
                  <span className={`landing-report-badge ${p.cited ? 'is-cited' : 'is-miss'}`}>
                    {p.cited ? t('landing.reportCited') : t('landing.reportMiss')}
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div className="landing-report-section">
            <div className="landing-report-section-title">{t('landing.reportActionsTitle')}</div>
            <ul className="landing-report-actions">
              {sampleActions.map((a) => (
                <li key={a}>{a}</li>
              ))}
            </ul>
          </div>

          <Button variant="outline" onClick={handleStartFree}>{t('landing.reportCta')}</Button>
        </div>
      </Reveal>

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
            © 2026 崛起GEO. {t('landing.footerRights')}
          </div>
          <div className="landing-footer-links">
            <a href="#features">{t('landing.navFeatures')}</a>
            <a href="#how">{t('landing.navHow')}</a>
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

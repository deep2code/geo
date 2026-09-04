import React from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import './Support.scss'

const Support: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <div className="support-page">
      {/* Hero 区域 */}
      <section className="support-hero">
        <div className="support-hero-badge">🔧 技术支持</div>
        <h1 className="support-hero-title">{t('support.title')}</h1>
        <p className="support-hero-subtitle">{t('support.subtitle')}</p>
      </section>

      {/* 联系方式 */}
      <section className="support-contact">
        <div className="support-contact-card">
          <div className="support-contact-icon">📧</div>
          <div className="support-contact-label">{t('support.emailLabel')}</div>
          <a href="mailto:deep2code@aliyun.com" className="support-contact-value">
            deep2code@aliyun.com
          </a>
          <div className="support-contact-desc">{t('support.emailDesc')}</div>
        </div>

        <div className="support-contact-card">
          <div className="support-contact-icon">💬</div>
          <div className="support-contact-label">{t('support.ticketLabel')}</div>
          <button className="support-contact-btn" onClick={() => navigate('/tickets')}>
            {t('support.ticketBtn')}
          </button>
          <div className="support-contact-desc">{t('support.ticketDesc')}</div>
        </div>

        <div className="support-contact-card">
          <div className="support-contact-icon">📚</div>
          <div className="support-contact-label">{t('support.helpLabel')}</div>
          <button className="support-contact-btn" onClick={() => navigate('/help')}>
            {t('support.helpBtn')}
          </button>
          <div className="support-contact-desc">{t('support.helpDesc')}</div>
        </div>
      </section>

      {/* 常见问题 */}
      <section className="support-faq">
        <h2 className="support-section-title">{t('support.faqTitle')}</h2>
        <div className="support-faq-list">
          <div className="support-faq-item">
            <div className="support-faq-q">{t('support.faq1Q')}</div>
            <div className="support-faq-a">{t('support.faq1A')}</div>
          </div>
          <div className="support-faq-item">
            <div className="support-faq-q">{t('support.faq2Q')}</div>
            <div className="support-faq-a">{t('support.faq2A')}</div>
          </div>
          <div className="support-faq-item">
            <div className="support-faq-q">{t('support.faq3Q')}</div>
            <div className="support-faq-a">{t('support.faq3A')}</div>
          </div>
        </div>
      </section>

      {/* 返回首页 */}
      <div className="support-footer">
        <button className="support-back-btn" onClick={() => navigate('/')}>
          ← {t('support.backHome')}
        </button>
      </div>
    </div>
  )
}

export default Support

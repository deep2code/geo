import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import '../Dashboard/Dashboard.scss'
import './Help.scss'

// 文章数据类型
interface ArticleItem {
  id: string
  title: string
  summary: string
  content: string
  category: string
}

// 默认文章（API 不可用时的兜底；分类与后端保持一致）
const mockArticles: ArticleItem[] = [
  { id: 'overview', title: '平台功能总览', summary: '一张图看懂 崛起GEO 能为你做什么', category: 'quickstart', content: '崛起GEO 是面向生成式引擎优化（GEO）的平台：让品牌在 ChatGPT、文心、通义、DeepSeek 等 AI 回答中被正确提及与引用。核心模块：品牌管理、品牌审计、内容优化器、关键词发现、竞品对标、引擎来源研究、外部提交分析、报告导出、告警邮件、管理后台。' },
  { id: 'getting-started', title: '5 分钟快速开始', summary: '创建品牌 → 执行审计 → 查看报告', category: 'quickstart', content: '四步完成首次审计：1) 品牌管理创建品牌；2) 补充查询词；3) 品牌审计选择引擎开始；4) 报告导出查看 BVS 与建议。' },
  { id: 'run-audit', title: '执行品牌审计', summary: '模拟真实提问，检测 AI 中的品牌可见度', category: 'audit', content: '在品牌审计页面选择品牌与引擎，点击开始审计。系统针对提示词模拟提问，检测提及率、引用率、情感与位置，汇总为 BVS 分数。' },
  { id: 'understand-bvs', title: '理解 BVS 分数', summary: 'BVS 评分维度与 A-F 等级说明', category: 'audit', content: 'BVS 范围 0-100，对应 A-F 等级。由提及率、引用率、引用位置、情感、实体识别等加权计算，区分「被提及」与「被引用」。' },
  { id: 'external-submissions', title: '外部提交分析', summary: '采集真实用户与 AI 的对话', category: 'features', content: '外部系统提交大模型对话，后台定时抽取情感、主题、来源域名等，反哺优化。在管理后台「外部提交分析」Tab 查看与触发。' },
  { id: 'content-optimizer', title: '内容优化器', summary: '针对单篇内容给出优化建议', category: 'features', content: '粘贴内容并选择目标引擎，系统给出提升 AI 可见度的结构、实体、引用信号等建议。' },
  { id: 'export-report', title: '导出与分享报告', summary: 'HTML / PDF 在线报告与邮件发送', category: 'report', content: '审计完成后在报告导出页面查看 HTML 报告、下载 PDF 或邮件发送，包含分数、引擎对比与优化建议。' },
  { id: 'api-keys', title: '配置 AI 引擎 API Key', summary: '在服务端环境变量配置各引擎密钥', category: 'settings', content: '在服务端环境变量配置 GEO_CHATGPT_KEY、GEO_GLM_KEY、GEO_DEEPSEEK_KEY 等。未配置的引擎审计时自动跳过。' },
  { id: 'faq', title: '常见问题解答', summary: '审计时长、数据存储、引擎支持等', category: 'faq', content: '审计通常 1-3 分钟；数据存于 MySQL；支持 GLM、通义、Doubao、文心、DeepSeek、ChatGPT 等主流引擎。' }
]

// 新手引导步骤定义
const onboardingSteps: { step: number; title: string; desc: string; route: string; icon: string }[] = [
  { step: 1, title: '添加品牌', desc: '在品牌管理页面添加你的第一个品牌', route: '/brand-management', icon: '🏢' },
  { step: 2, title: '执行审计', desc: '对品牌进行首次 AI 可见度审计', route: '/brand-audit', icon: '🔍' },
  { step: 3, title: '优化内容', desc: '使用内容优化器提升单篇内容可见度', route: '/content-optimizer', icon: '✍️' },
  { step: 4, title: '导出报告', desc: '导出审计报告并分享给团队', route: '/report-export', icon: '📄' }
]

// 精通路线：5 个阶段，每阶段含目标 / 操作步骤（可跳转）/ 验收标准
const journeyStages: {
  stage: number
  icon: string
  titleKey: string
  goalKey: string
  acceptanceKey: string
  stepsKey: string
  routes: string[]
}[] = [
  { stage: 1, icon: '🚀', titleKey: 'help.journeyS1Title', goalKey: 'help.journeyS1Goal', acceptanceKey: 'help.journeyS1Acceptance', stepsKey: 'help.journeyS1Steps', routes: ['/content-optimizer', '/system-check', '/content-optimizer', '/system-check'] },
  { stage: 2, icon: '🤖', titleKey: 'help.journeyS2Title', goalKey: 'help.journeyS2Goal', acceptanceKey: 'help.journeyS2Acceptance', stepsKey: 'help.journeyS2Steps', routes: ['/settings', '/content-optimizer', '/content-optimizer', '/content-optimizer'] },
  { stage: 3, icon: '🎯', titleKey: 'help.journeyS3Title', goalKey: 'help.journeyS3Goal', acceptanceKey: 'help.journeyS3Acceptance', stepsKey: 'help.journeyS3Steps', routes: ['/brand-management', '/brand-audit', '/brand-audit', '/rules'] },
  { stage: 4, icon: '🧩', titleKey: 'help.journeyS4Title', goalKey: 'help.journeyS4Goal', acceptanceKey: 'help.journeyS4Acceptance', stepsKey: 'help.journeyS4Steps', routes: ['/brand-db-import', '/keyword-discovery', '/compare', '/alert-email'] },
  { stage: 5, icon: '🛠️', titleKey: 'help.journeyS5Title', goalKey: 'help.journeyS5Goal', acceptanceKey: 'help.journeyS5Acceptance', stepsKey: 'help.journeyS5Steps', routes: ['/system-check', '/brand-audit', '/settings', '/alert-email'] }
]

const Help: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)

  const [articles, setArticles] = useState<ArticleItem[]>(mockArticles)
  const [selectedArticle, setSelectedArticle] = useState<ArticleItem | null>(null)
  const [onboardingState, setOnboardingState] = useState<Record<number, boolean>>({
    1: false, 2: false, 3: false, 4: false
  })

  // 加载文章列表（全部，不分分类）
  const loadArticles = async () => {
    try {
      const res = await api.help.articles()
      const list = (res as any)?.articles ?? (res as any)?.items
      if (Array.isArray(list) && list.length > 0) setArticles(list)
    } catch {
      // API 不可用时使用本地兜底
    }
  }

  // 加载文章详情
  const loadArticleDetail = async (id: string) => {
    try {
      const res = await api.help.articleDetail(id)
      if (res) {
        setSelectedArticle(res)
        return
      }
    } catch {
      // 回退到列表中的数据
    }
    setSelectedArticle(articles.find(a => a.id === id) ?? null)
  }

  // 加载新手引导进度
  const loadOnboarding = async () => {
    try {
      const res = await api.help.onboarding()
      const steps = (res as any)?.steps
      if (Array.isArray(steps)) {
        const state: Record<number, boolean> = {}
        steps.forEach((s: { step: number; completed?: boolean }) => { state[s.step] = !!s.completed })
        setOnboardingState(state)
      }
    } catch {
      // 保留默认状态
    }
  }

  useEffect(() => {
    loadArticles()
  }, [])

  useEffect(() => {
    loadOnboarding()
  }, [])

  // 点击文章
  const handleArticleClick = (article: ArticleItem) => {
    loadArticleDetail(article.id)
  }

  // 完成引导步骤
  const handleStepClick = async (step: number, route: string) => {
    if (!onboardingState[step]) {
      try {
        await api.help.completeStep(step)
      } catch {
        // 忽略错误
      }
      setOnboardingState(prev => ({ ...prev, [step]: true }))
      showToast(`${t('help.onboardingStepCompleted')}: ${onboardingSteps.find(s => s.step === step)?.title}`, 'success')
    }
    navigate(route)
  }

  // 渲染文章正文（简单 markdown 解析）
  const renderArticleContent = (content: string) => {
    return content.split('\n').map((line, i) => {
      if (line.startsWith('## ')) {
        return <h3 key={i} style={{ marginTop: 20, marginBottom: 8 }}>{line.slice(3)}</h3>
      }
      if (line.startsWith('- ')) {
        return <div key={i} style={{ paddingLeft: 20, marginBottom: 4 }}>• {line.slice(2)}</div>
      }
      if (line.trim() === '') return <div key={i} style={{ height: 8 }} />
      return <p key={i}>{line}</p>
    })
  }

  return (
    <div className="help-page help-page--linear">
      <div className="page-header">
        <h1 className="page-title">{t('help.title')}</h1>
        <p className="page-subtitle">{t('help.subtitle')}</p>
      </div>

      {/* ① 新手引导（快速上手） */}
      <section className="help-block">
        <h2 className="help-block-title">{t('help.onboardingTitle')}</h2>
        <div className="help-onboarding-grid">
          {onboardingSteps.map(item => {
            const completed = !!onboardingState[item.step]
            return (
              <div
                key={item.step}
                className={`help-onboarding-card ${completed ? 'is-completed' : ''}`}
                onClick={() => handleStepClick(item.step, item.route)}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') handleStepClick(item.step, item.route) }}
              >
                <div className="help-onboarding-step">
                  {completed ? '✓' : item.step}
                </div>
                <div style={{ fontSize: 28, marginBottom: 8 }}>{item.icon}</div>
                <div className="help-onboarding-title">{item.title}</div>
                <div className="help-onboarding-desc">{item.desc}</div>
                <span className={`help-onboarding-status help-onboarding-status-${completed ? 'done' : 'todo'}`}>
                  {completed ? `✓ ${t('help.onboardingDone')}` : t('help.onboardingTodo')}
                </span>
              </div>
            )
          })}
        </div>
      </section>

      {/* ② 精通路线 */}
      <section className="help-block">
        <h2 className="help-block-title">{t('help.journeyTitle')}</h2>
        <div className="help-journey-list">
          {journeyStages.map(stage => {
            const steps: string[] = Array.isArray(t(stage.stepsKey)) ? t(stage.stepsKey) as unknown as string[] : []
            return (
              <div key={stage.stage} className="help-journey-stage">
                <div className="help-journey-stage-head">
                  <div className="help-journey-stage-badge">{stage.icon}</div>
                  <div>
                    <div className="help-journey-stage-title">{t(stage.titleKey)}</div>
                    <div className="help-journey-stage-goal">{t(stage.goalKey)}</div>
                  </div>
                </div>
                <div className="help-journey-stage-body">
                  <ol className="help-journey-steps-list">
                    {steps.map((step, i) => (
                      <li key={i} className="help-journey-step-item">
                        <button
                          type="button"
                          className="help-journey-step-link"
                          onClick={() => navigate(stage.routes[i] ?? '/help')}
                        >
                          {step}
                          <span className="help-journey-step-arrow">→</span>
                        </button>
                      </li>
                    ))}
                  </ol>
                  <div className="help-journey-acceptance">
                    <span className="help-journey-acceptance-label">🎯 {t('help.journeyAcceptance')}</span>
                    <div className="help-journey-acceptance-text">{t(stage.acceptanceKey)}</div>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </section>

      {/* ③ 帮助文章 */}
      <section className="help-block">
        <h2 className="help-block-title">{t('help.tabArticles')}</h2>
        {selectedArticle ? (
          <div className="help-article-content">
            <Button variant="ghost" size="sm" onClick={() => setSelectedArticle(null)} style={{ marginBottom: 16 }}>
              ← {t('help.backToList')}
            </Button>
            <h3 className="help-article-content-title">{selectedArticle.title}</h3>
            <div className="help-article-content-body">
              {renderArticleContent(selectedArticle.content)}
            </div>
          </div>
        ) : articles.length > 0 ? (
          <div className="help-article-list">
            {articles.map(article => (
              <button
                key={article.id}
                type="button"
                className="help-article-item"
                onClick={() => handleArticleClick(article)}
              >
                <div className="help-article-title">{article.title}</div>
                <div className="help-article-summary">{article.summary}</div>
              </button>
            ))}
          </div>
        ) : (
          <div className="help-empty">
            <div className="help-empty-icon">📭</div>
            <div>{t('common.noData')}</div>
          </div>
        )}
      </section>
    </div>
  )
}

export default Help

import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Tabs, TabPane } from '@/components/Tabs'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import '../Dashboard/Dashboard.scss'
import './Help.scss'

// 分类定义
interface Category {
  code: string
  name: string
  icon: string
}

// 文章数据类型
interface ArticleItem {
  id: string
  title: string
  summary: string
  content: string
  category: string
}

// 默认分类列表
const defaultCategories: Category[] = [
  { code: 'quickstart', name: '快速开始', icon: '🚀' },
  { code: 'audit', name: '审计', icon: '🔍' },
  { code: 'report', name: '报告', icon: '📄' },
  { code: 'settings', name: '设置', icon: '⚙️' },
  { code: 'faq', name: 'FAQ', icon: '❓' }
]

// 默认文章模拟数据
const mockArticles: ArticleItem[] = [
  {
    id: '1',
    title: '5 分钟快速上手 MyGEO',
    summary: '了解 MyGEO 平台核心概念，完成首次品牌审计',
    category: 'quickstart',
    content: '欢迎使用 MyGEO！本指南帮助你在 5 分钟内完成首次品牌审计。\n\n## 1. 添加品牌\n进入「品牌管理」页面，点击「添加品牌」并填写品牌名称、域名等信息。\n\n## 2. 配置查询词\n为品牌添加业务查询词（Prompts），这些是客户可能向 AI 提问的问题。\n\n## 3. 开始审计\n进入「品牌审计」页面，选择品牌并点击「开始审计」，系统将在 30-60 秒内完成多引擎可见度检测。\n\n## 4. 查看报告\n审计完成后可查看 BVS 评分、引擎统计、内容缺口等详细分析。'
  },
  {
    id: '2',
    title: '理解 BVS 品牌可见度评分',
    summary: 'BVS 评分的 7 个维度与计算逻辑详解',
    category: 'audit',
    content: 'BVS（Brand Visibility Score）是 MyGEO 的核心评分体系，从 7 个维度衡量品牌在 AI 搜索引擎中的可见度。\n\n## 七大维度\n- 内容质量（权重 20%）\n- 技术 SEO（权重 15%）\n- 站内 SEO（权重 15%）\n- 结构化数据（权重 10%）\n- 页面性能（权重 10%）\n- AI 就绪（权重 20%）\n- 图像优化（权重 10%）\n\n## 评级标准\n- A（90+）：头部品牌\n- B（80-89）：中坚品牌\n- C（70-79）：潜力品牌\n- D（60-69）：待优化\n- F（<60）：长尾品牌'
  },
  {
    id: '3',
    title: '如何导出审计报告',
    summary: '支持 HTML / PDF 两种格式，可邮件发送',
    category: 'report',
    content: '审计完成后，你可以通过「报告导出」页面生成报告。\n\n## 支持格式\n- HTML 报告：浏览器直接查看\n- PDF 报告：A4 格式，适合打印归档\n\n## 邮件发送\n填写收件人后可一键发送，支持同时附上 PDF 和 HTML。'
  },
  {
    id: '4',
    title: '配置 AI 引擎 API Key',
    summary: '在后端环境变量中配置各引擎的 API Key',
    category: 'settings',
    content: 'MyGEO 支持多种 AI 引擎，需在服务端配置对应的 API Key。\n\n## 环境变量\n- GEO_CHATGPT_KEY\n- GEO_CLAUDE_KEY\n- GEO_PERPLEXITY_KEY\n- GEO_GEMINI_KEY\n- GEO_QWEN_KEY\n- GEO_GLM_KEY\n- GEO_DEEPSEEK_KEY\n\n在「设置 > API」页面可查看各引擎的配置状态。'
  },
  {
    id: '5',
    title: '常见问题解答',
    summary: '审计失败、报告导出异常等问题排查',
    category: 'faq',
    content: '## 审计失败怎么办？\n请检查目标引擎的 API Key 是否配置正确，网络是否能正常访问对应引擎。\n\n## 报告导出为空？\n确保对应品牌已完成至少一次审计，且审计历史库可正常访问。\n\n## 邮件发送失败？\n检查 SMTP 配置（host/port/from），在「告警邮件」页面可查看邮件服务状态。'
  }
]

// 新手引导步骤定义
const onboardingSteps: { step: number; title: string; desc: string; route: string; icon: string }[] = [
  { step: 1, title: '添加品牌', desc: '在品牌管理页面添加你的第一个品牌', route: '/brand-management', icon: '🏢' },
  { step: 2, title: '执行审计', desc: '对品牌进行首次 AI 可见度审计', route: '/brand-audit', icon: '🔍' },
  { step: 3, title: '优化内容', desc: '使用内容优化器提升单篇内容可见度', route: '/content-optimizer', icon: '✍️' },
  { step: 4, title: '导出报告', desc: '导出审计报告并分享给团队', route: '/report-export', icon: '📄' }
]

const Help: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const showToast = useAppStore(s => s.showToast)

  const [activeCategory, setActiveCategory] = useState('quickstart')
  const [articles, setArticles] = useState<ArticleItem[]>(mockArticles)
  const [selectedArticle, setSelectedArticle] = useState<ArticleItem | null>(null)
  const [onboardingState, setOnboardingState] = useState<Record<number, boolean>>({
    1: false, 2: false, 3: false, 4: false
  })

  // 加载文章列表
  const loadArticles = async (category: string) => {
    try {
      const res = await api.help.articles(category)
      if (res?.items) setArticles(res.items)
    } catch {
      // API 不可用时使用本地过滤
      setArticles(mockArticles.filter(a => a.category === category))
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
      if (res?.completed_steps) {
        const state: Record<number, boolean> = {}
        res.completed_steps.forEach((s: number) => { state[s] = true })
        setOnboardingState(state)
      }
    } catch {
      // 保留默认状态
    }
  }

  useEffect(() => {
    loadArticles(activeCategory)
  }, [activeCategory])

  useEffect(() => {
    loadOnboarding()
  }, [])

  // 当前分类的文章列表
  const currentArticles = articles.filter(a => a.category === activeCategory)
  const currentCategory = defaultCategories.find(c => c.code === activeCategory)

  // 点击文章
  const handleArticleClick = (article: ArticleItem) => {
    loadArticleDetail(article.id)
  }

  // 返回文章列表
  const handleBackToList = () => {
    setSelectedArticle(null)
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
    <div className="help-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('help.title')}</h1>
          <p className="page-subtitle">{t('help.subtitle')}</p>
        </div>
      </div>

      <Tabs variant="pills">
        {/* Tab 1: 帮助文章 */}
        <TabPane tabKey="articles" tab={`📖 ${t('help.tabArticles')}`}>
          <div className="help-layout">
            {/* 左侧分类导航 */}
            <nav className="help-category-nav">
              {defaultCategories.map(cat => {
                const count = mockArticles.filter(a => a.category === cat.code).length
                return (
                  <button
                    key={cat.code}
                    type="button"
                    className={`help-category-item ${activeCategory === cat.code ? 'is-active' : ''}`}
                    onClick={() => { setActiveCategory(cat.code); setSelectedArticle(null) }}
                  >
                    <span className="help-category-icon">{cat.icon}</span>
                    <span>{cat.name}</span>
                    <span className="help-category-count">{count}</span>
                  </button>
                )
              })}
            </nav>

            {/* 右侧文章内容 */}
            <div>
              {selectedArticle ? (
                <div className="help-article-content">
                  <Button variant="ghost" size="sm" onClick={handleBackToList} style={{ marginBottom: 16 }}>
                    ← {t('help.backToList')}
                  </Button>
                  <h2 className="help-article-content-title">{selectedArticle.title}</h2>
                  <div className="help-article-content-body">
                    {renderArticleContent(selectedArticle.content)}
                  </div>
                </div>
              ) : currentArticles.length > 0 ? (
                <div className="help-article-list">
                  {currentArticles.map(article => (
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
            </div>
          </div>
        </TabPane>

        {/* Tab 2: 新手引导 */}
        <TabPane tabKey="onboarding" tab={`🎓 ${t('help.tabOnboarding')}`}>
          <Card title={t('help.onboardingTitle')} subtitle={t('help.onboardingSubtitle')} compact>
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
          </Card>
        </TabPane>
      </Tabs>
    </div>
  )
}

export default Help

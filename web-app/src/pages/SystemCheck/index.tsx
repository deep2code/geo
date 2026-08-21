import React, { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '@/services/api'
import type { SelfCheckReport, SelfCheckCheck, SelfCheckSeverity } from '@/types/api'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import './SystemCheck.scss'

/**
 * 系统自检页（对标原 `geo doctor` CLI，但面向新手，纯前端界面驱动）。
 *
 * - 一键运行：调用 GET /api/v1/admin/selfcheck，按健康/隐患/问题分组展示；
 * - 需 Owner/Admin 角色（账号体系）访问；403 时页面内引导去登录页，而非被全局拦截器踢走；
 * - 对 warn/error 项给出"为什么 / 怎么修"的新手友好提示。
 */

// 新手友好修复建议：按后端检查项 Name 命中。
const FIX_HINTS: Record<string, string> = {
  '评分管线': '评分管线异常，请查看服务端日志或重启服务。',
  '内容分析管线': '分词异常，检查输入编码或重启服务。',
  '优化管线': '优化管线执行失败，详情见下方「Detail」；若依赖 LLM 请确认 Key 配置正确。',
  'LLM 改写业务': 'LLM 调用失败，检查 GEO_LLM_KEY / GEO_LLM_BASE / GEO_LLM_MODEL 是否配置，以及网络是否可达（Detail 含具体错误）。',
  '数据库可达性：离线工商库': '无法连接该 MySQL，检查对应 DSN 的 host:port、账号密码，以及库是否已创建（docker compose up 会自动建库）。',
  '数据库可达性：审计历史库': '无法连接该 MySQL，检查对应 DSN 的 host:port、账号密码，以及库是否已创建（docker compose up 会自动建库）。',
  '数据库可达性：China-Check 缓存库': '无法连接该 MySQL，检查对应 DSN 的 host:port、账号密码，以及库是否已创建（docker compose up 会自动建库）。',
  '服务端口': 'GEO_PORT 应为 1–65535 的整数（默认 8080）。',
  '日志级别': 'GEO_LOG_LEVEL 应为 debug / info / warn / error 之一。',
  '日志格式': 'GEO_LOG_FORMAT 应为 text / json 之一。',
  'LLM 月度预算': 'GEO_LLM_BUDGET_USD 应为非负浮点数（0 表示不限制预算熔断）。',
  '账号体系/鉴权配置': '鉴权配置未通过启动校验，见下方「Detail」；请修改默认弱密钥 / 示例口令。',
  '账号体系（JWT）': '未启用账号体系（GEO_AUTH_ENABLED=true）时 API 匿名放行、管理接口全部 403。生产请启用并配置 GEO_JWT_SECRET 与 GEO_ADMIN_EMAIL/PASSWORD（首次启动自动建管理员）。',
  'LLM 基础配置': '配置了 GEO_LLM_BASE 但未配置 GEO_LLM_KEY，LLM 仍不可用，请补上 Key。',
  '引擎 API Key': '未配置任何引擎 / LLM Key，品牌审计与智能补全将不可用；请在环境变量中配置对应引擎的 Key。',
  '数据库 DSN 格式': '对应模块 DSN 格式异常，应形如 user:pass@tcp(host:3306)/dbname。',
  '白标主题色': 'GEO_WL_PRIMARY_COLOR 应为合法 hex 颜色，如 #3B82F6。',
  '定时审计配置': '检查 GEO_SCHEDULER_ENABLED 与 GEO_SCHEDULER_CONFIG 路径是否正确。',
  '外部规则集': '规则集加载 / 校验失败，见下方「Detail」；可用规则集校验工具检查文件。'
}

const STATUS_META: Record<SelfCheckSeverity, { icon: string; label: string; tone: string }> = {
  ok: { icon: '✅', label: '正常', tone: 'ok' },
  info: { icon: 'ℹ️', label: '提示', tone: 'info' },
  warn: { icon: '⚠️', label: '隐患', tone: 'warn' },
  error: { icon: '❌', label: '问题', tone: 'error' }
}

const OVERALL_META: Record<SelfCheckSeverity, { icon: string; label: string; tone: string }> = {
  ok: { icon: '✅', label: '系统状态健康', tone: 'ok' },
  info: { icon: '💡', label: '系统状态正常（含提示项）', tone: 'info' },
  warn: { icon: '⚠️', label: '系统存在隐患', tone: 'warn' },
  error: { icon: '❌', label: '系统存在问题', tone: 'error' }
}

const SystemCheck: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [report, setReport] = useState<SelfCheckReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [forbidden, setForbidden] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const runCheck = useCallback(async () => {
    setLoading(true)
    setForbidden(false)
    setError(null)
    try {
      const data = await api.admin.selfCheck()
      setReport(data)
    } catch (err: any) {
      const status = (err && (err.status as number | undefined)) || 0
      if (status === 403) {
        setForbidden(true)
      } else {
        setError(
          (err && err.message ? String(err.message) : `请求失败（HTTP ${status || 'unknown'}）`)
        )
      }
      setReport(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    runCheck()
  }, [runCheck])

  const renderCheck = (c: SelfCheckCheck) => {
    const meta = STATUS_META[c.status]
    const hint = c.status === 'warn' || c.status === 'error' ? FIX_HINTS[c.name] : ''
    return (
      <div key={c.name} className={`sc-check sc-check-${meta.tone}`}>
        <div className="sc-check-head">
          <span className="sc-check-icon">{meta.icon}</span>
          <span className="sc-check-name">{c.name}</span>
          <span className="sc-check-tag">{meta.label}</span>
        </div>
        <div className="sc-check-msg">{c.message}</div>
        {c.detail ? <pre className="sc-check-detail">{c.detail}</pre> : null}
        {hint ? <div className="sc-check-hint">💡 建议：{hint}</div> : null}
      </div>
    )
  }

  const renderSection = (title: string, items: SelfCheckCheck[]) => (
    <section className="sc-section">
      <h3 className="sc-section-title">{title}</h3>
      {items.length === 0 ? (
        <div className="sc-empty">（无检查项）</div>
      ) : (
        <div className="sc-grid">{items.map(renderCheck)}</div>
      )}
    </section>
  )

  return (
    <div className="system-check-page">
      <div className="sc-page-head">
        <div>
          <h1 className="sc-page-title">🩺 {t('systemCheck.title', '系统自检')}</h1>
          <p className="sc-page-sub">
            {t('systemCheck.subtitle', '一键检测关键业务是否正常工作、属性/参数/配置是否有问题。无需命令行，新手也能看懂。')}
          </p>
        </div>
        <Button variant="primary" size="md" loading={loading} onClick={runCheck}>
          {loading ? '检测中…' : '🔄 运行系统自检'}
        </Button>
      </div>

      {forbidden && (
        <Card>
          <div className="sc-forbidden">
            <div style={{ fontSize: 32, marginBottom: 8 }}>🔐</div>
            <div className="sc-forbidden-title">需要管理员权限</div>
            <p className="sc-forbidden-text">
              系统自检属于管理后台能力，需以 <code>Owner/Admin</code> 账号登录。请在右上角「登录」输入部署时预置的管理员邮箱与密码（<code>GEO_ADMIN_EMAIL / GEO_ADMIN_PASSWORD</code>）。
            </p>
            <Button
              variant="primary"
              size="md"
              onClick={() => navigate('/admin/login?redirect=/system-check')}
            >
              前往登录
            </Button>
          </div>
        </Card>
      )}

      {error && !forbidden && (
        <Card>
          <div className="sc-error">
            <div style={{ fontSize: 28, marginBottom: 8 }}>⚠️</div>
            <div className="sc-error-title">自检请求失败</div>
            <p className="sc-error-text">{error}</p>
            <Button variant="secondary" size="md" onClick={runCheck}>重试</Button>
          </div>
        </Card>
      )}

      {report && (
        <>
          <div className={`sc-overall sc-overall-${OVERALL_META[report.overall].tone}`}>
            <div className="sc-overall-icon">{OVERALL_META[report.overall].icon}</div>
            <div className="sc-overall-body">
              <div className="sc-overall-label">{OVERALL_META[report.overall].label}</div>
              <div className="sc-overall-sub">
                正常 {report.summary.ok} · 提示 {report.summary.info} · 隐患 {report.summary.warn} · 问题 {report.summary.error}
              </div>
            </div>
            <div className="sc-overall-time">
              生成于 {new Date(report.generated_at).toLocaleString()}
            </div>
          </div>

          <div className="sc-runtime">
            <span className="sc-chip">Go {report.runtime.go_version}</span>
            <span className="sc-chip">{report.runtime.os}/{report.runtime.arch}</span>
            <span className="sc-chip">{report.runtime.num_cpu} CPU</span>
            <span className="sc-chip">{report.runtime.goroutines} goroutines</span>
            <span className="sc-chip">{report.runtime.alloc_mb.toFixed(1)} MB</span>
          </div>

          {renderSection('① 关键业务是否正常', report.business)}
          {renderSection('② 属性 / 参数 / 配置是否有问题', report.config)}

          <div className="sc-legend">
            状态说明：✅ 正常 · ℹ️ 提示（功能未启用或已跳过，不影响运行） · ⚠️ 隐患（配置/环境有问题但不阻断） · ❌ 问题（需处理）
          </div>
        </>
      )}
    </div>
  )
}

export default SystemCheck

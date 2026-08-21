import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import { useAppStore, DEFAULT_WHITELABEL } from '@/store/useAppStore'
import { api, setApiAuthToken } from '@/services/api'

/**
 * 登录入口（账号体系，JWT）：
 * - 401/403 触发后，全局拦截器会跳转到这里，带上 redirect（原页面路径）。
 * - 凭据为邮箱 + 密码（POST /api/v1/auth/login）；登录成功后保存 access_token。
 * - 管理后台/数据管理接口按角色（Owner/Admin）放行，普通接口任意登录用户可访问。
 * - 服务端未启用账号体系（GEO_AUTH_ENABLED=true）时登录接口返回 503，此处给出提示。
 */
const AdminLogin: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const [sp] = useSearchParams()
  const showToast = useAppStore(s => s.showToast)
  const brandName = useAppStore(s => s.whitelabel?.brand_name) || DEFAULT_WHITELABEL.brand_name

  // redirect 优先级：URL ?redirect= → location.state.from → /dashboard
  const redirect = (() => {
    const raw = sp.get('redirect')
    if (raw && /^\/[A-Za-z0-9_\-/?=&.%#]*$/.test(raw)) return raw
    const stateFrom = (location.state as { from?: string } | null)?.from
    if (stateFrom && /^\/[A-Za-z0-9_\-/?=&.%#]*$/.test(stateFrom)) return stateFrom
    return '/dashboard'
  })()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [errMsg, setErrMsg] = useState<string | null>(null)
  const [showPwd, setShowPwd] = useState(false)
  const emailInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    emailInputRef.current?.focus()
  }, [])

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setErrMsg(null)
    try {
      const em = email.trim().toLowerCase()
      if (!em || !password) {
        setErrMsg('请输入邮箱与密码。')
        return
      }
      let res
      try {
        res = await api.auth.login({ email: em, password })
      } catch (err: any) {
        const status = (err && (err.status as number | undefined)) || 0
        if (status === 503) {
          setErrMsg('服务端未启用账号体系（需设置 GEO_AUTH_ENABLED=true），无法登录。')
        } else if (status === 401 || status === 403) {
          setErrMsg('邮箱或密码错误。')
        } else {
          setErrMsg(
            `登录失败：${err && err.message ? String(err.message) : `HTTP ${status || 'unknown'}`}`
          )
        }
        return
      }
      setApiAuthToken(res.tokens.access_token)
      showToast && showToast('登录成功', 'success')
      navigate(redirect, { replace: true })
    } finally {
      setSubmitting(false)
    }
  }, [email, password, navigate, redirect, showToast])

  return (
    <div
      style={{
        minHeight: '100vh',
        width: '100%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '24px 16px',
        background:
          'radial-gradient(1200px 600px at 10% -10%, rgba(64,158,255,0.12), transparent 60%), ' +
          'radial-gradient(900px 500px at 110% 10%, rgba(139,92,246,0.12), transparent 60%), ' +
          'var(--bg-page)'
      }}
    >
      <div style={{ width: '100%', maxWidth: 440 }}>
        <div style={{ marginBottom: 16, textAlign: 'center' }}>
          <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>
            🔐 {brandName} · 登录
          </div>
          <div style={{ marginTop: 8, color: 'var(--text-tertiary)', fontSize: 13 }}>
            {t('adminLogin.subtitle', '登录后访问控制台；管理后台功能需 Owner/Admin 角色。')}
          </div>
        </div>

        <Card>
          <form onSubmit={handleSubmit} noValidate style={{ padding: 24 }}>
            {/* 邮箱 */}
            <label
              htmlFor="login-email"
              style={{
                display: 'block',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--text-primary)',
                marginBottom: 6
              }}
            >
              邮箱
            </label>
            <input
              ref={emailInputRef}
              id="login-email"
              type="email"
              autoComplete="username"
              spellCheck={false}
              value={email}
              onChange={e => setEmail(e.target.value)}
              placeholder="you@example.com"
              style={{
                width: '100%',
                height: 40,
                borderRadius: 8,
                border: '1px solid var(--border-default)',
                background: 'var(--bg-elevated)',
                color: 'var(--text-primary)',
                padding: '0 12px',
                outline: 'none',
                fontSize: 14,
                transition: 'border-color .15s ease, box-shadow .15s ease'
              }}
            />

            {/* 密码 */}
            <label
              htmlFor="login-password"
              style={{
                display: 'block',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--text-primary)',
                marginTop: 20,
                marginBottom: 6
              }}
            >
              密码
            </label>
            <div style={{ display: 'flex', gap: 8 }}>
              <input
                id="login-password"
                type={showPwd ? 'text' : 'password'}
                autoComplete="current-password"
                spellCheck={false}
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="••••••••"
                style={{
                  flex: 1,
                  height: 40,
                  borderRadius: 8,
                  border: '1px solid var(--border-default)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-primary)',
                  padding: '0 12px',
                  outline: 'none',
                  fontSize: 14,
                  transition: 'border-color .15s ease, box-shadow .15s ease'
                }}
              />
              <button
                type="button"
                onClick={() => setShowPwd(v => !v)}
                style={{
                  height: 40,
                  padding: '0 12px',
                  borderRadius: 8,
                  border: '1px solid var(--border-default)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-secondary)',
                  cursor: 'pointer'
                }}
                aria-label={showPwd ? '隐藏密码' : '显示密码'}
              >
                {showPwd ? '🙈' : '👁'}
              </button>
            </div>
            <div style={{ marginTop: 6, fontSize: 12, color: 'var(--text-tertiary)', lineHeight: 1.6 }}>
              管理员账号由部署时
              <code style={{ margin: '0 4px', padding: '1px 6px', background: 'var(--bg-elevated)', borderRadius: 4 }}>
                GEO_ADMIN_EMAIL / GEO_ADMIN_PASSWORD
              </code>
              预置（首次启动自动创建）。
            </div>

            {/* 错误提示 */}
            {errMsg && (
              <div
                role="alert"
                style={{
                  marginTop: 16,
                  padding: '10px 12px',
                  borderRadius: 8,
                  background: 'var(--status-danger-bg)',
                  color: 'var(--status-danger)',
                  fontSize: 13,
                  lineHeight: 1.6
                }}
              >
                {errMsg}
              </div>
            )}

            <div style={{ marginTop: 22, display: 'flex', gap: 8, justifyContent: 'space-between' }}>
              <Button
                type="button"
                variant="secondary"
                size="md"
                onClick={() => navigate('/dashboard', { replace: true })}
                disabled={submitting}
              >
                返回控制台
              </Button>
              <Button type="submit" size="md" loading={submitting} disabled={submitting}>
                登录
              </Button>
            </div>
          </form>
        </Card>

        <div style={{ marginTop: 12, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 12 }}>
          {t('adminLogin.securityTip', '⚠️ 生产部署请配合 HTTPS 使用；不要把账号密码提交到代码仓库。')}
        </div>
      </div>
    </div>
  )
}

export default AdminLogin

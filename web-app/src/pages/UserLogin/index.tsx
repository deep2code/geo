import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { Button } from '@/components/Button'
import { useAppStore, DEFAULT_WHITELABEL } from '@/store/useAppStore'
import { api, setApiAuthToken } from '@/services/api'
import './UserLogin.scss'

const UserLogin: React.FC = () => {
  const navigate = useNavigate()
  const location = useLocation()
  const [sp] = useSearchParams()
  const showToast = useAppStore(s => s.showToast)
  const setUserRole = useAppStore(s => s.setUserRole)
  const brandName = useAppStore(s => s.whitelabel?.brand_name) || DEFAULT_WHITELABEL.brand_name

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
  const [focused, setFocused] = useState<string | null>(null)
  const emailInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    emailInputRef.current?.focus()
  }, [])

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    const em = email.trim().toLowerCase()
    if (!em || !password) {
      setErrMsg('请输入邮箱与密码。')
      return
    }
    setSubmitting(true)
    setErrMsg(null)
    try {
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
          setErrMsg(`登录失败：${err && err.message ? String(err.message) : 'HTTP ' + (status || 'unknown')}`)
        }
        return
      }
      setApiAuthToken(res.tokens.access_token)
      if (res.workspaces && res.workspaces.length > 0) {
        const role = res.workspaces[0].role || 'viewer'
        setUserRole(role)
      }
      showToast && showToast('登录成功', 'success')
      navigate(redirect, { replace: true })
    } finally {
      setSubmitting(false)
    }
  }, [email, password, navigate, redirect, showToast])

  return (
    <div className="user-login-page">
      <div className="user-login-bg">
        <div className="user-login-bg-gradient" />
        <div className="user-login-bg-grid" />
      </div>

      <div className="user-login-container">
        <div className="user-login-card">
          <div className="user-login-header">
            <div className="user-login-logo">
              <span className="user-login-logo-text">G</span>
            </div>
            <h1 className="user-login-title">{brandName}</h1>
            <p className="user-login-subtitle">工作台登录</p>
          </div>

          <form onSubmit={handleSubmit} noValidate className="user-login-form">
            <div className={`user-login-field ${focused === 'email' || email ? 'is-focused' : ''}`}>
              <label htmlFor="login-email" className="user-login-field-label">邮箱</label>
              <div className="user-login-field-input-wrap">
                <span className="user-login-field-icon">📧</span>
                <input
                  ref={emailInputRef}
                  id="login-email"
                  type="email"
                  autoComplete="username"
                  spellCheck={false}
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  onFocus={() => setFocused('email')}
                  onBlur={() => setFocused(null)}
                  placeholder="you@example.com"
                  className="user-login-field-input"
                />
              </div>
            </div>

            <div className={`user-login-field ${focused === 'password' || password ? 'is-focused' : ''}`}>
              <label htmlFor="login-password" className="user-login-field-label">密码</label>
              <div className="user-login-field-input-wrap">
                <span className="user-login-field-icon">🔒</span>
                <input
                  id="login-password"
                  type={showPwd ? 'text' : 'password'}
                  autoComplete="current-password"
                  spellCheck={false}
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  onFocus={() => setFocused('password')}
                  onBlur={() => setFocused(null)}
                  placeholder="••••••••"
                  className="user-login-field-input"
                />
                <button
                  type="button"
                  onClick={() => setShowPwd(v => !v)}
                  className="user-login-field-toggle"
                  aria-label={showPwd ? '隐藏密码' : '显示密码'}
                >
                  {showPwd ? '🙈' : '👁'}
                </button>
              </div>
            </div>

            {errMsg && (
              <div className="user-login-error" role="alert">
                <span className="user-login-error-icon">⚠️</span>
                {errMsg}
              </div>
            )}

            <div className="user-login-actions">
              <Button
                type="button"
                variant="outline"
                size="md"
                onClick={() => navigate('/', { replace: true })}
                disabled={submitting}
                className="user-login-btn-secondary"
              >
                返回首页
              </Button>
              <Button type="submit" size="md" loading={submitting} disabled={submitting} className="user-login-btn-primary">
                登录工作台
              </Button>
            </div>
          </form>

          <div className="user-login-footer">
            <a href="/admin/login" className="user-login-admin-link">系统管理登录 →</a>
          </div>
        </div>
      </div>
    </div>
  )
}

export default UserLogin

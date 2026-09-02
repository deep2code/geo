import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { Button } from '@/components/Button'
import { useAppStore, DEFAULT_WHITELABEL } from '@/store/useAppStore'
import { api, setApiAuthToken } from '@/services/api'
import './AdminLogin.scss'

const AdminLogin: React.FC = () => {
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
    return '/admin'
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
          setErrMsg(
            `登录失败：${err && err.message ? String(err.message) : `HTTP ${status || 'unknown'}`}`
          )
        }
        return
      }
      setApiAuthToken(res.tokens.access_token)
      // 存储用户角色
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
    <div className="login-page">
      {/* 动画背景 */}
      <div className="login-bg">
        <div className="login-bg-gradient" />
        <div className="login-bg-grid" />
        <div className="login-bg-orbs">
          <div className="login-bg-orb login-bg-orb--1" />
          <div className="login-bg-orb login-bg-orb--2" />
          <div className="login-bg-orb login-bg-orb--3" />
        </div>
        <div className="login-bg-particles" aria-hidden>
          {Array.from({ length: 20 }).map((_, i) => (
            <div key={i} className="login-particle" style={{
              left: `${Math.random() * 100}%`,
              animationDelay: `${Math.random() * 8}s`,
              animationDuration: `${6 + Math.random() * 6}s`
            }} />
          ))}
        </div>
      </div>

      {/* 主内容 */}
      <div className="login-container">
        {/* 左侧品牌区 */}
        <div className="login-brand">
          <div className="login-brand-content">
            <div className="login-brand-logo">
              <span className="login-brand-logo-text">G</span>
              <div className="login-brand-logo-ring" />
            </div>
            <h1 className="login-brand-name">{brandName}</h1>
            <p className="login-brand-tagline">生成式引擎优化平台</p>
            <div className="login-brand-features">
              <div className="login-brand-feature">
                <span className="login-brand-feature-icon">🔍</span>
                <span>13+ AI 引擎覆盖</span>
              </div>
              <div className="login-brand-feature">
                <span className="login-brand-feature-icon">📊</span>
                <span>BVS 可见度评分</span>
              </div>
              <div className="login-brand-feature">
                <span className="login-brand-feature-icon">⚡</span>
                <span>实时优化建议</span>
              </div>
            </div>
          </div>
        </div>

        {/* 右侧登录表单 */}
        <div className="login-form-wrapper">
          <div className="login-form-card">
            <div className="login-form-header">
              <h2 className="login-form-title">系统管理</h2>
              <p className="login-form-subtitle">管理员登录</p>
            </div>

            <form onSubmit={handleSubmit} noValidate className="login-form">
              {/* 邮箱 */}
              <div className={`login-field ${focused === 'email' || email ? 'is-focused' : ''}`}>
                <label htmlFor="login-email" className="login-field-label">邮箱</label>
                <div className="login-field-input-wrap">
                  <span className="login-field-icon">📧</span>
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
                    className="login-field-input"
                  />
                </div>
              </div>

              {/* 密码 */}
              <div className={`login-field ${focused === 'password' || password ? 'is-focused' : ''}`}>
                <label htmlFor="login-password" className="login-field-label">密码</label>
                <div className="login-field-input-wrap">
                  <span className="login-field-icon">🔒</span>
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
                    className="login-field-input"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPwd(v => !v)}
                    className="login-field-toggle"
                    aria-label={showPwd ? '隐藏密码' : '显示密码'}
                  >
                    {showPwd ? '🙈' : '👁'}
                  </button>
                </div>
              </div>

              {/* 错误提示 */}
              {errMsg && (
                <div className="login-error" role="alert">
                  <span className="login-error-icon">⚠️</span>
                  {errMsg}
                </div>
              )}

              {/* 按钮 */}
              <div className="login-actions">
                <Button
                  type="button"
                  variant="outline"
                  size="md"
                  onClick={() => navigate('/', { replace: true })}
                  disabled={submitting}
                  className="login-btn-secondary"
                >
                  返回首页
                </Button>
                <Button type="submit" size="md" loading={submitting} disabled={submitting} className="login-btn-primary">
                  登录
                </Button>
              </div>
            </form>

            <div className="login-form-footer">
              <a href="/login" className="login-admin-link">工作台登录 →</a>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default AdminLogin

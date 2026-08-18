import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/Button'
import { Card } from '@/components/Card'
import { useAppStore, DEFAULT_WHITELABEL } from '@/store/useAppStore'
import { api, setAdminKey, setApiAuthToken } from '@/services/api'

/**
 * 管理员统一登录入口：
 * - 401/403 触发后，全局拦截器会跳转到这里，带上 redirect（原页面路径）。
 * - 支持两种凭据：
 *   1) 管理员 Key (X-Admin-Key) → 对应后端 GEO_ADMIN_KEY，用于管理后台操作/清数据
 *   2) API Token (Authorization: Bearer) → 对应后端 GEO_API_KEY（若部署了此鉴权）
 * 两者可选其一；但管理员 Key 必须验证通过后才会保存。
 */
const AdminLogin: React.FC = () => {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const [sp] = useSearchParams()
  const showToast = useAppStore(s => s.showToast)
  const brandName = useAppStore(s => s.whitelabel?.brand_name) || DEFAULT_WHITELABEL.brand_name

  // redirect 优先级：URL ?redirect= → location.state.from → /admin
  const redirect = (() => {
    const raw = sp.get('redirect')
    if (raw && /^\/[A-Za-z0-9_\-/?=&.%#]*$/.test(raw)) return raw
    const stateFrom = (location.state as { from?: string } | null)?.from
    if (stateFrom && /^\/[A-Za-z0-9_\-/?=&.%#]*$/.test(stateFrom)) return stateFrom
    return '/admin'
  })()

  const [adminKey, setAdminKeyState] = useState('')
  const [apiToken, setApiTokenState] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [errMsg, setErrMsg] = useState<string | null>(null)
  const [showAdminKey, setShowAdminKey] = useState(false)
  const [showApiToken, setShowApiToken] = useState(false)
  const adminInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    adminInputRef.current?.focus()
  }, [])

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    setErrMsg(null)
    try {
      const key = adminKey.trim()
      const tok = apiToken.trim()

      if (!key && !tok) {
        setErrMsg('请至少输入管理员 Key 或 API Token。')
        return
      }

      // 1) 如果填了管理员 Key：先做一次校验（打到一个需要管理员权限的小接口：scheduler/status）。
      //    避免把错误 key 写入 sessionStorage，然后其它接口 403 → 又跳回来，形成死循环。
      if (key) {
        try {
          await api.adminVerify(key)
        } catch (err: any) {
          const status = (err && (err.status as number | undefined)) || 0
          if (status === 401 || status === 403) {
            setErrMsg('管理员 Key 校验失败（HTTP 403）。请确认服务器 GEO_ADMIN_KEY 与你输入的一致。')
          } else if (status === 503) {
            // 503：scheduler 未启动，但鉴权本身通常是 OK 的；这里为了保守仍保存 key。
            // （scheduler 未启用不代表不是管理员。）
          } else {
            setErrMsg(
              `管理员 Key 校验失败：${err && err.message ? String(err.message) : `HTTP ${status || 'unknown'}`}`
            )
          }
          return
        }
        setAdminKey(key)
      }

      // 2) 如果填了 API Token：直接保存（不做预验证，因为公开接口不依赖它；
      //    真有 401 也会重新跳回这里，用户可再改）。
      if (tok) {
        setApiAuthToken(tok)
      }

      showToast && showToast('登录成功', 'success')
      navigate(redirect, { replace: true })
    } finally {
      setSubmitting(false)
    }
  }, [adminKey, apiToken, navigate, redirect, showToast])

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
      <div style={{ width: '100%', maxWidth: 480 }}>
        <div style={{ marginBottom: 16, textAlign: 'center' }}>
          <div style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>
            🔐 {brandName} · 管理员登录
          </div>
          <div style={{ marginTop: 8, color: 'var(--text-tertiary)', fontSize: 13 }}>
            {t('adminLogin.subtitle', '凭据仅保存在本机浏览器会话存储（关闭标签页即清除）；访问 /admin、/settings、调度器接口均会自动附带。')}
          </div>
        </div>

        <Card>
          <form onSubmit={handleSubmit} noValidate style={{ padding: 24 }}>
            {/* 管理员 Key */}
            <label
              htmlFor="admin-key"
              style={{
                display: 'block',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--text-primary)',
                marginBottom: 6
              }}
            >
              管理员 Key <span style={{ color: 'var(--text-tertiary)', fontWeight: 400 }}>(X-Admin-Key)</span>
            </label>
            <div style={{ display: 'flex', gap: 8 }}>
              <input
                ref={adminInputRef}
                id="admin-key"
                type={showAdminKey ? 'text' : 'password'}
                autoComplete="off"
                spellCheck={false}
                value={adminKey}
                onChange={e => setAdminKeyState(e.target.value)}
                placeholder="例如：openssl rand -hex 16 生成的值"
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
                onClick={() => setShowAdminKey(v => !v)}
                style={{
                  height: 40,
                  padding: '0 12px',
                  borderRadius: 8,
                  border: '1px solid var(--border-default)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-secondary)',
                  cursor: 'pointer'
                }}
                aria-label={showAdminKey ? '隐藏管理员 Key' : '显示管理员 Key'}
              >
                {showAdminKey ? '🙈' : '👁'}
              </button>
            </div>
            <div style={{ marginTop: 6, fontSize: 12, color: 'var(--text-tertiary)', lineHeight: 1.6 }}>
              部署建议：服务端启动前
              <code style={{ margin: '0 4px', padding: '1px 6px', background: 'var(--bg-elevated)', borderRadius: 4 }}>
                export GEO_ADMIN_KEY="$(openssl rand -hex 16)"
              </code>
              ；将该值粘贴此处即可进入管理员模式。
            </div>

            {/* API Token */}
            <label
              htmlFor="api-token"
              style={{
                display: 'block',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--text-primary)',
                marginTop: 20,
                marginBottom: 6
              }}
            >
              API Token <span style={{ color: 'var(--text-tertiary)', fontWeight: 400 }}>(可选，Bearer)</span>
            </label>
            <div style={{ display: 'flex', gap: 8 }}>
              <input
                id="api-token"
                type={showApiToken ? 'text' : 'password'}
                autoComplete="off"
                spellCheck={false}
                value={apiToken}
                onChange={e => setApiTokenState(e.target.value)}
                placeholder="若部署端启用 GEO_API_KEY 鉴权，请填入 Bearer Token"
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
                onClick={() => setShowApiToken(v => !v)}
                style={{
                  height: 40,
                  padding: '0 12px',
                  borderRadius: 8,
                  border: '1px solid var(--border-default)',
                  background: 'var(--bg-elevated)',
                  color: 'var(--text-secondary)',
                  cursor: 'pointer'
                }}
                aria-label={showApiToken ? '隐藏 API Token' : '显示 API Token'}
              >
                {showApiToken ? '🙈' : '👁'}
              </button>
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
                验证并进入
              </Button>
            </div>
          </form>
        </Card>

        <div style={{ marginTop: 12, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 12 }}>
          {t('adminLogin.securityTip', '⚠️ 生产部署请配合 HTTPS 使用；不要把管理员 Key 提交到代码仓库。')}
        </div>
      </div>
    </div>
  )
}

export default AdminLogin

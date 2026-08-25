import React, { lazy, Suspense, useEffect } from 'react'
import { Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Layout } from '@/components/Layout'
import { onAuthError, getApiAuthToken } from '@/services/api'

const Dashboard = lazy(() => import('@/pages/Dashboard'))
const ContentOptimizer = lazy(() => import('@/pages/ContentOptimizer'))
const BrandManagement = lazy(() => import('@/pages/BrandManagement'))
const BrandAudit = lazy(() => import('@/pages/BrandAudit'))
const KeywordDiscovery = lazy(() => import('@/pages/KeywordDiscovery'))
const ReportExport = lazy(() => import('@/pages/ReportExport'))
const AlertEmail = lazy(() => import('@/pages/AlertEmail'))
const Settings = lazy(() => import('@/pages/Settings'))
const Compare = lazy(() => import('@/pages/Compare'))
const Leaderboard = lazy(() => import('@/pages/Leaderboard'))
// 新增模块
const Admin = lazy(() => import('@/pages/Admin'))
const AdminLogin = lazy(() => import('@/pages/AdminLogin'))
const SystemCheck = lazy(() => import('@/pages/SystemCheck'))
const Rules = lazy(() => import('@/pages/Rules'))
const Evaluate = lazy(() => import('@/pages/Evaluate'))
const BrandDBImport = lazy(() => import('@/pages/BrandDBImport'))
const Integrations = lazy(() => import('@/pages/Integrations'))
const Help = lazy(() => import('@/pages/Help'))
const Tickets = lazy(() => import('@/pages/Tickets'))
const Landing = lazy(() => import('@/pages/Landing'))
const Terms = lazy(() => import('@/pages/Terms'))
const Privacy = lazy(() => import('@/pages/Privacy'))
const DPA = lazy(() => import('@/pages/DPA'))

const PageFallback: React.FC = () => (
  <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-tertiary)' }}>
    <div style={{ fontSize: '32px', marginBottom: '16px' }}>⏳</div>
    <div>加载中...</div>
  </div>
)

/**
 * ProtectedRoute：需要登录态的路由守卫。
 * 未登录时重定向到 /admin/login，保留原始路径用于登录后跳回。
 */
const ProtectedRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const navigate = useNavigate()
  const location = useLocation()

  const token = getApiAuthToken()
  if (!token) {
    // 未登录：保存当前路径，登录后跳回
    const qs = new URLSearchParams()
    qs.set('redirect', `${location.pathname}${location.search}`)
    navigate(`/admin/login?${qs.toString()}`, { replace: true })
    return null
  }

  return <>{children}</>
}

/**
 * 把全局 401/403 统一跳转到管理员登录入口，并附带 redirect。
 * 放在 AppRoutes 顶层可以直接拿到 useNavigate/useLocation。
 */
const AuthErrorRedirector: React.FC = () => {
  const navigate = useNavigate()
  const location = useLocation()

  useEffect(() => {
    const unsub = onAuthError(({ status }) => {
      const current = `${location.pathname}${location.search}`
      // 避免在登录页本身收到 401/403 时反复重定向
      if (location.pathname === '/admin/login') return
      const redirect = current === '/admin/login' ? '/admin' : current
      const qs = new URLSearchParams()
      qs.set('redirect', redirect)
      qs.set('reason', status === 401 ? 'unauthorized' : 'forbidden')
      navigate(`/admin/login?${qs.toString()}`, { replace: true })
    })
    return unsub
  }, [navigate, location])

  return null
}

// 后台业务页统一外壳：Layout（侧边导航）+ 登录守卫
const ProtectedPage: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Layout>
    <ProtectedRoute>{children}</ProtectedRoute>
  </Layout>
)

// 公开业务页（无需登录，如帮助/工单/法务）：仅 Layout 外壳
const PublicPage: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <Layout>{children}</Layout>
)

export const AppRoutes: React.FC = () => {
  return (
    <>
      <AuthErrorRedirector />
      <Routes>
        {/* 公开首页：根路径直接渲染 Landing（不做 /landing 跳转，URL 保持 /） */}
        <Route path="/" element={
          <Suspense fallback={<PageFallback />}><Landing /></Suspense>
        } />
        {/* 兼容直达 /landing（仍是同一首页） */}
        <Route path="/landing" element={
          <Suspense fallback={<PageFallback />}><Landing /></Suspense>
        } />
        {/* 管理员登录：独立全屏，不带 Layout */}
        <Route path="/admin/login" element={
          <Suspense fallback={<PageFallback />}><AdminLogin /></Suspense>
        } />

        {/* 后台业务页（需登录） */}
        <Route path="/dashboard" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Dashboard /></Suspense></ProtectedPage>
        } />
        <Route path="/compare" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Compare /></Suspense></ProtectedPage>
        } />
        <Route path="/leaderboard" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Leaderboard /></Suspense></ProtectedPage>
        } />
        <Route path="/content-optimizer" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><ContentOptimizer /></Suspense></ProtectedPage>
        } />
        <Route path="/brand-management" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><BrandManagement /></Suspense></ProtectedPage>
        } />
        <Route path="/brand-audit" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><BrandAudit /></Suspense></ProtectedPage>
        } />
        <Route path="/keyword-discovery" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><KeywordDiscovery /></Suspense></ProtectedPage>
        } />
        <Route path="/report-export" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><ReportExport /></Suspense></ProtectedPage>
        } />
        <Route path="/alert-email" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><AlertEmail /></Suspense></ProtectedPage>
        } />
        <Route path="/settings" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Settings /></Suspense></ProtectedPage>
        } />
        <Route path="/admin" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Admin /></Suspense></ProtectedPage>
        } />
        <Route path="/system-check" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><SystemCheck /></Suspense></ProtectedPage>
        } />
        <Route path="/rules" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Rules /></Suspense></ProtectedPage>
        } />
        <Route path="/evaluate" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Evaluate /></Suspense></ProtectedPage>
        } />
        <Route path="/brand-db-import" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><BrandDBImport /></Suspense></ProtectedPage>
        } />
        <Route path="/integrations" element={
          <ProtectedPage><Suspense fallback={<PageFallback />}><Integrations /></Suspense></ProtectedPage>
        } />

        {/* 公开业务页（帮助/工单/法务） */}
        <Route path="/help" element={
          <PublicPage><Suspense fallback={<PageFallback />}><Help /></Suspense></PublicPage>
        } />
        <Route path="/tickets" element={
          <PublicPage><Suspense fallback={<PageFallback />}><Tickets /></Suspense></PublicPage>
        } />
        <Route path="/terms" element={
          <PublicPage><Suspense fallback={<PageFallback />}><Terms /></Suspense></PublicPage>
        } />
        <Route path="/privacy" element={
          <PublicPage><Suspense fallback={<PageFallback />}><Privacy /></Suspense></PublicPage>
        } />
        <Route path="/dpa" element={
          <PublicPage><Suspense fallback={<PageFallback />}><DPA /></Suspense></PublicPage>
        } />

        {/* 未匹配路径 → 首页 */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}

export default AppRoutes

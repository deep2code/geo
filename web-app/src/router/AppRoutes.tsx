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

export const AppRoutes: React.FC = () => {
  return (
    <>
      <AuthErrorRedirector />
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* 未登录首页：根路径直接进公开落地页（Landing） */}
          <Route index element={<Navigate to="/landing" replace />} />
          {/* 业务/管理页面：均需登录态（未登录跳登录页，登录后回原路径） */}
          <Route path="dashboard" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><Dashboard /></Suspense></ProtectedRoute>
          } />
          <Route path="compare" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><Compare /></Suspense></ProtectedRoute>
          } />
          <Route path="leaderboard" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><Leaderboard /></Suspense></ProtectedRoute>
          } />
          <Route path="content-optimizer" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><ContentOptimizer /></Suspense></ProtectedRoute>
          } />
          <Route path="brand-management" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><BrandManagement /></Suspense></ProtectedRoute>
          } />
          <Route path="brand-audit" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><BrandAudit /></Suspense></ProtectedRoute>
          } />
          <Route path="keyword-discovery" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><KeywordDiscovery /></Suspense></ProtectedRoute>
          } />
          <Route path="report-export" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><ReportExport /></Suspense></ProtectedRoute>
          } />
          <Route path="alert-email" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><AlertEmail /></Suspense></ProtectedRoute>
          } />
          <Route path="settings" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><Settings /></Suspense></ProtectedRoute>
          } />
          <Route path="admin" element={
            <ProtectedRoute>
              <Suspense fallback={<PageFallback />}><Admin /></Suspense>
            </ProtectedRoute>
          } />
          <Route path="system-check" element={
            <ProtectedRoute>
              <Suspense fallback={<PageFallback />}><SystemCheck /></Suspense>
            </ProtectedRoute>
          } />
          <Route path="rules" element={
            <ProtectedRoute>
              <Suspense fallback={<PageFallback />}><Rules /></Suspense>
            </ProtectedRoute>
          } />
          <Route path="evaluate" element={
            <ProtectedRoute>
              <Suspense fallback={<PageFallback />}><Evaluate /></Suspense>
            </ProtectedRoute>
          } />
          <Route path="brand-db-import" element={
            <ProtectedRoute>
              <Suspense fallback={<PageFallback />}><BrandDBImport /></Suspense>
            </ProtectedRoute>
          } />
          <Route path="integrations" element={
            <ProtectedRoute><Suspense fallback={<PageFallback />}><Integrations /></Suspense></ProtectedRoute>
          } />
          {/* 公开页（无需登录）：帮助 / 工单 / 法务 */}
          <Route path="help" element={
            <Suspense fallback={<PageFallback />}><Help /></Suspense>
          } />
          <Route path="tickets" element={
            <Suspense fallback={<PageFallback />}><Tickets /></Suspense>
          } />
          <Route path="terms" element={
            <Suspense fallback={<PageFallback />}><Terms /></Suspense>
          } />
          <Route path="privacy" element={
            <Suspense fallback={<PageFallback />}><Privacy /></Suspense>
          } />
          <Route path="dpa" element={
            <Suspense fallback={<PageFallback />}><DPA /></Suspense>
          } />
          {/* 未匹配路径 → 根路径（根路径再跳公开落地页） */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
        {/* 管理员登录：独立全屏，不带 Layout。 */}
        <Route path="/admin/login" element={
          <Suspense fallback={<PageFallback />}><AdminLogin /></Suspense>
        } />
        {/* 落地页独立全屏页面，不使用 Layout */}
        <Route path="/landing" element={
          <Suspense fallback={<PageFallback />}><Landing /></Suspense>
        } />
      </Routes>
    </>
  )
}

export default AppRoutes

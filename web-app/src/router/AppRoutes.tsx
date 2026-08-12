import React, { lazy, Suspense, useEffect } from 'react'
import { Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Layout } from '@/components/Layout'
import { onAuthError } from '@/services/api'

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
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={
            <Suspense fallback={<PageFallback />}><Dashboard /></Suspense>
          } />
          <Route path="compare" element={
            <Suspense fallback={<PageFallback />}><Compare /></Suspense>
          } />
          <Route path="leaderboard" element={
            <Suspense fallback={<PageFallback />}><Leaderboard /></Suspense>
          } />
          <Route path="content-optimizer" element={
            <Suspense fallback={<PageFallback />}><ContentOptimizer /></Suspense>
          } />
          <Route path="brand-management" element={
            <Suspense fallback={<PageFallback />}><BrandManagement /></Suspense>
          } />
          <Route path="brand-audit" element={
            <Suspense fallback={<PageFallback />}><BrandAudit /></Suspense>
          } />
          <Route path="keyword-discovery" element={
            <Suspense fallback={<PageFallback />}><KeywordDiscovery /></Suspense>
          } />
          <Route path="report-export" element={
            <Suspense fallback={<PageFallback />}><ReportExport /></Suspense>
          } />
          <Route path="alert-email" element={
            <Suspense fallback={<PageFallback />}><AlertEmail /></Suspense>
          } />
          <Route path="settings" element={
            <Suspense fallback={<PageFallback />}><Settings /></Suspense>
          } />
          <Route path="admin" element={
            <Suspense fallback={<PageFallback />}><Admin /></Suspense>
          } />
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
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
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

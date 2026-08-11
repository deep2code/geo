import React, { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from '@/components/Layout'

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
const Help = lazy(() => import('@/pages/Help'))
const Tickets = lazy(() => import('@/pages/Tickets'))
const Landing = lazy(() => import('@/pages/Landing'))

const PageFallback: React.FC = () => (
  <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-tertiary)' }}>
    <div style={{ fontSize: '32px', marginBottom: '16px' }}>⏳</div>
    <div>加载中...</div>
  </div>
)

export const AppRoutes: React.FC = () => {
  return (
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
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Route>
      {/* 落地页独立全屏页面，不使用 Layout */}
      <Route path="/landing" element={
        <Suspense fallback={<PageFallback />}><Landing /></Suspense>
      } />
    </Routes>
  )
}

export default AppRoutes

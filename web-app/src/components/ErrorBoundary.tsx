import React from 'react'

interface ErrorBoundaryProps {
  children: React.ReactNode
}

interface ErrorBoundaryState {
  error: Error | null
}

/**
 * 全局错误边界：捕获渲染期异常，展示降级界面而非整站白屏。
 * 根因案例：Dashboard 曾把 API 返回的对象直接渲染为 React 子节点，
 * 触发 React #31 后整个 root 被卸载 → 打开一会儿白屏。
 */
class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    console.error('[ErrorBoundary] caught render error:', error, info.componentStack)
  }

  render(): React.ReactNode {
    if (this.state.error) {
      return (
        <div
          style={{
            minHeight: '100vh',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 24,
            background: 'var(--surface-primary, #f8fafc)',
            color: 'var(--text-primary, #0f172a)',
            fontFamily: 'system-ui, -apple-system, sans-serif'
          }}
        >
          <div
            style={{
              maxWidth: 560,
              width: '100%',
              padding: 32,
              borderRadius: 16,
              border: '1px solid var(--border-primary, #e2e8f0)',
              background: 'var(--surface-secondary, #fff)',
              boxShadow: '0 8px 30px rgba(0,0,0,0.06)'
            }}
          >
            <div style={{ fontSize: 40, marginBottom: 12 }}>⚠️</div>
            <h1 style={{ fontSize: 20, fontWeight: 700, margin: '0 0 8px' }}>页面发生错误</h1>
            <p style={{ fontSize: 13, color: 'var(--text-secondary, #475569)', margin: '0 0 16px' }}>
              渲染过程出现异常，页面已停止加载。你可以刷新重试，或联系管理员反馈。
            </p>
            <pre
              style={{
                fontSize: 12,
                padding: 12,
                borderRadius: 8,
                background: 'var(--bg-tertiary, #f1f5f9)',
                overflow: 'auto',
                maxHeight: 180,
                margin: '0 0 16px',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all'
              }}
            >
              {this.state.error.message}
            </pre>
            <button
              type="button"
              onClick={() => window.location.reload()}
              style={{
                padding: '10px 20px',
                borderRadius: 8,
                border: 'none',
                background: 'var(--brand-primary, #6366f1)',
                color: '#fff',
                fontSize: 14,
                fontWeight: 600,
                cursor: 'pointer'
              }}
            >
              🔄 刷新页面
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

export default ErrorBoundary

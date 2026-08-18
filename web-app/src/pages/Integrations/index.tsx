import React, { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import './Integrations.scss'

/**
 * 集成 / MCP（替代原 `geo mcp-server` CLI）。
 * MCP Server 已随 Web 服务同进程启动（默认 :9090 /mcp），
 * 本页展示端点与客户端接入方式，无需命令行。
 */
const Integrations: React.FC = () => {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const mcpPort = 9090
  const mcpUrl = useMemo(() => {
    if (typeof window === 'undefined') return `http://localhost:${mcpPort}/mcp`
    const proto = window.location.protocol
    const host = window.location.hostname || 'localhost'
    return `${proto}//${host}:${mcpPort}/mcp`
  }, [])

  const claudeConfig = useMemo(
    () =>
      JSON.stringify(
        {
          mcpServers: {
            geo: {
              url: mcpUrl,
              headers: { 'X-API-Key': '${GEO_MCP_API_KEY}' }
            }
          }
        },
        null,
        2
      ),
    [mcpUrl]
  )

  const copy = (text: string) => {
    navigator.clipboard?.writeText(text).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      },
      () => {}
    )
  }

  return (
    <div className="integrations-page">
      <div className="integrations-head">
        <h1 className="integrations-title">🔌 {t('integrations.title', '集成 / MCP')}</h1>
        <p className="integrations-sub">{t('integrations.subtitle', 'MCP Server 已随 Web 服务一起启动，让 Claude / Cursor / TraeCode 直接调用 GEO 能力（替代命令行 geo mcp-server）。')}</p>
      </div>

      <Card>
        <h3 className="integrations-section-title">📡 {t('integrations.endpoint', 'MCP 端点')}</h3>
        <div className="integrations-url-row">
          <code className="integrations-url">{mcpUrl}</code>
          <button className="integrations-copy" onClick={() => copy(mcpUrl)}>{copied ? '✓' : '📋'}</button>
        </div>
        <ul className="integrations-list">
          <li>协议：JSON-RPC 2.0 over Streamable HTTP（2025-06-18）</li>
          <li>默认端口：<code>9090</code>（可由 <code>GEO_MCP_PORT</code> 环境变量修改）</li>
          <li>未设置 <code>GEO_MCP_API_KEY</code> 时仅允许本机回环访问；远程客户端需设置该密钥并携带 <code>X-API-Key</code> / <code>Bearer</code>。</li>
          <li>暴露工具：<code>geo_brand_audit</code> / <code>geo_optimize_content</code> / <code>geo_search_companies</code> / <code>geo_chinacheck</code> / <code>geo_readiness_audit</code></li>
        </ul>
      </Card>

      <Card>
        <h3 className="integrations-section-title">🤖 {t('integrations.claude', 'Claude / Cursor 客户端配置')}</h3>
        <p className="integrations-hint">{t('integrations.claudeHint', '将以下内容存入客户端 MCP 配置；${GEO_MCP_API_KEY} 替换为你设置的环境变量值（未设置则删除 headers 字段，仅限本机）。')}</p>
        <pre className="integrations-json">{claudeConfig}</pre>
      </Card>
    </div>
  )
}

export default Integrations

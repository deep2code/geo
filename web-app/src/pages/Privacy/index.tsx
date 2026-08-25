import React from 'react'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import '../Dashboard/Dashboard.scss'

const Privacy: React.FC = () => {
  const navigate = useNavigate()

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">隐私政策</h1>
          <p className="page-subtitle">最后更新：2026 年 8 月 1 日</p>
        </div>
        <Button variant="ghost" size="sm" icon="←" onClick={() => navigate(-1)}>返回</Button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Card title="一、引言" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>崛起GEO（以下简称「我们」）深知个人信息对您的重要性，并致力于保护您的隐私安全。本隐私政策将帮助您了解我们如何收集、使用、存储和保护您的个人信息。</p>
            <p>在使用我们的服务前，请您务必仔细阅读并充分理解本政策的全部内容。</p>
          </div>
        </Card>

        <Card title="二、我们收集的信息" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12, fontWeight: 600 }}>2.1 您主动提供的信息</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>注册信息：姓名、邮箱、手机号、公司名称</li>
              <li style={{ marginBottom: 6 }}>品牌画像：品牌名称、别名、域名、行业、产品信息、竞争对手信息</li>
              <li style={{ marginBottom: 6 }}>业务查询词（Prompts）：您配置的客户查询问题</li>
              <li>内容优化：您提交的待优化文章、URL、标题等</li>
            </ul>
            <p style={{ marginBottom: 12, fontWeight: 600 }}>2.2 自动收集的信息</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>设备信息：浏览器类型、操作系统、屏幕分辨率</li>
              <li style={{ marginBottom: 6 }}>日志信息：IP 地址、访问时间、页面浏览记录</li>
              <li>Cookie 及类似技术：用于保持会话状态和偏好设置</li>
            </ul>
            <p style={{ fontWeight: 600 }}>2.3 审计产生的数据</p>
            <p>品牌审计过程中，我们会调用 AI 搜索引擎获取公开可见的查询结果，用于计算 BVS 评分。</p>
          </div>
        </Card>

        <Card title="三、我们如何使用信息" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>提供、维护和改进本服务的核心功能</li>
              <li style={{ marginBottom: 6 }}>执行品牌可见度审计、内容优化等计算任务</li>
              <li style={{ marginBottom: 6 }}>向您发送告警邮件、周报和重要服务通知</li>
              <li style={{ marginBottom: 6 }}>检测和防止欺诈、滥用等安全风险</li>
              <li style={{ marginBottom: 6 }}>进行匿名化的数据分析，用于产品优化</li>
              <li>遵守法律法规要求或响应合法的政府请求</li>
            </ul>
          </div>
        </Card>

        <Card title="四、信息共享与披露" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>我们不会出售您的个人信息。仅在以下情况下，我们可能共享您的数据：</p>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>经您明确同意后</li>
              <li style={{ marginBottom: 6 }}>与受保密协议约束的服务提供商共享（如云服务商、邮件服务商）</li>
              <li style={{ marginBottom: 6 }}>法律法规要求或司法、行政机关依法要求</li>
              <li>为保护我们、您或公众的合法权益所必需</li>
            </ul>
          </div>
        </Card>

        <Card title="五、数据存储与安全" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 您的数据存储于中华人民共和国境内的服务器上，企业版用户可选择私有化部署。</p>
            <p style={{ marginBottom: 12 }}>2. 我们采用行业标准的安全措施保护您的数据，包括传输加密（TLS）、静态加密、访问控制、审计日志等。</p>
            <p>3. 尽管我们采取了合理的安全措施，但互联网环境并非百分之百安全，我们将尽力保护您的信息安全。</p>
          </div>
        </Card>

        <Card title="六、您的数据权利" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>根据适用的法律法规，您享有以下数据权利：</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}><strong>访问权</strong>：您可以查询我们持有您的哪些个人信息</li>
              <li style={{ marginBottom: 6 }}><strong>更正权</strong>：您可以要求更正不准确的信息</li>
              <li style={{ marginBottom: 6 }}><strong>导出权</strong>：您可以获取您的个人数据副本</li>
              <li style={{ marginBottom: 6 }}><strong>删除权</strong>：您可以要求删除您的个人信息（法定情形除外）</li>
              <li><strong>撤回同意</strong>：您可以撤回此前给予的同意</li>
            </ul>
            <p>您可在「系统设置 → 数据权利」中行使上述权利，或通过工单联系我们。</p>
          </div>
        </Card>

        <Card title="七、Cookie 政策" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>我们使用必要的 Cookie 以保持您的登录状态和记住偏好设置（如主题、语言、界面密度）。</p>
            <p>您可以通过浏览器设置管理或删除 Cookie，但这可能影响部分功能的正常使用。</p>
          </div>
        </Card>

        <Card title="八、未成年人保护" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>本服务主要面向企业和专业用户。若您是未满 14 周岁的未成年人，请在监护人的陪同下阅读本政策并使用我们的服务。</p>
          </div>
        </Card>

        <Card title="九、政策更新" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>我们可能适时更新本隐私政策。更新后的政策将在本页面公布，重大变更将通过站内通知或邮件告知您。</p>
          </div>
        </Card>

        <Card title="十、联系我们" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>如对本隐私政策或数据处理有任何疑问、投诉或请求，请通过「提交工单」或邮箱 privacy@mygeo.example 与我们联系。我们将在 15 个工作日内回复。</p>
          </div>
        </Card>
      </div>
    </div>
  )
}

export default Privacy

import React from 'react'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import '../Dashboard/Dashboard.scss'

const Terms: React.FC = () => {
  const navigate = useNavigate()

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">服务条款</h1>
          <p className="page-subtitle">最后更新：2026 年 8 月 1 日</p>
        </div>
        <Button variant="ghost" size="sm" icon="←" onClick={() => navigate(-1)}>返回</Button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Card title="一、服务说明" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>欢迎使用 MyGEO（以下简称「本服务」）。本服务条款（以下简称「本条款」）是您与 MyGEO 运营方之间就使用本服务所订立的协议。</p>
            <p style={{ marginBottom: 12 }}>MyGEO 是一套面向 AI 搜索引擎的内容与品牌可见度优化平台，提供品牌可见度审计（BVS）、内容优化、竞品对标、关键词发现、报告导出等功能。</p>
            <p>在使用本服务前，请您仔细阅读本条款的全部内容。您一旦注册或使用本服务，即视为您已阅读并同意接受本条款的全部约束。</p>
          </div>
        </Card>

        <Card title="二、用户账号" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 您在注册账号时应提供真实、准确、完整的个人或企业信息，并在信息变更时及时更新。</p>
            <p style={{ marginBottom: 12 }}>2. 您应妥善保管账号和密码，对账号下发生的所有活动承担责任。如发现未经授权的使用，请立即通知我们。</p>
            <p>3. 您不得将账号转让、出借或以其他方式许可第三方使用。</p>
          </div>
        </Card>

        <Card title="三、用户行为规范" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>您在使用本服务时，不得从事以下行为：</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>违反法律法规、公序良俗或侵犯第三方合法权益</li>
              <li style={{ marginBottom: 6 }}>利用本服务进行任何形式的自动化批量查询、数据爬取或反向工程</li>
              <li style={{ marginBottom: 6 }}>上传、传播病毒、恶意代码或有害信息</li>
              <li style={{ marginBottom: 6 }}>干扰、破坏本服务的正常运行或安全性</li>
              <li>未经授权访问他人账号或数据</li>
            </ul>
          </div>
        </Card>

        <Card title="四、知识产权" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 本服务的所有内容（包括但不限于软件、界面设计、商标、Logo、文档等）的知识产权归 MyGEO 运营方所有。</p>
            <p style={{ marginBottom: 12 }}>2. 您通过本服务上传的品牌信息、内容数据等，其知识产权归您或相关权利人所有。您授予我们在服务范围内使用、存储、处理该等数据的许可。</p>
            <p>3. 审计报告、优化建议等本服务生成的内容，您可在合法范围内自由使用。</p>
          </div>
        </Card>

        <Card title="五、付费与退款" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 本服务提供免费版、专业版和企业版三种套餐，具体以定价页面为准。</p>
            <p style={{ marginBottom: 12 }}>2. 付费服务按约定周期结算，开通后不支持中途退款（法律法规另有规定的除外）。</p>
            <p>3. 套餐升级可按比例补差价，降级将在当前周期结束后生效。</p>
          </div>
        </Card>

        <Card title="六、免责声明" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 本服务基于公开可访问的 AI 搜索引擎数据进行分析，结果仅供参考，不构成任何投资、经营或法律建议。</p>
            <p style={{ marginBottom: 12 }}>2. 因 AI 引擎本身的机制变化、API 故障或网络原因导致的服务中断或数据不准确，我们不承担责任。</p>
            <p>3. 在法律允许的最大范围内，我们不对间接、附带、特殊或惩罚性损害承担责任。</p>
          </div>
        </Card>

        <Card title="七、条款变更" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>我们可能根据法律法规变更或服务升级需要修改本条款。变更后的条款将在本页面公布，自公布之日起生效。</p>
            <p>如您继续使用本服务，即视为接受修改后的条款。</p>
          </div>
        </Card>

        <Card title="八、联系我们" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>如对本条款有任何疑问，请通过「提交工单」或邮箱 legal@mygeo.example 与我们联系。</p>
          </div>
        </Card>
      </div>
    </div>
  )
}

export default Terms

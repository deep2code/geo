import React from 'react'
import { useNavigate } from 'react-router-dom'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import '../Dashboard/Dashboard.scss'

const DPA: React.FC = () => {
  const navigate = useNavigate()

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">数据处理协议 (DPA)</h1>
          <p className="page-subtitle">Data Processing Agreement · 最后更新：2026 年 8 月 1 日</p>
        </div>
        <Button variant="ghost" size="sm" icon="←" onClick={() => navigate(-1)}>返回</Button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Card title="一、协议双方与目的" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>本数据处理协议（以下简称「本 DPA」）由以下双方签订：</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}><strong>数据控制者（Controller）</strong>：使用 崛起GEO 服务的用户（即您），决定个人数据的处理目的和方式</li>
              <li><strong>数据处理者（Processor）</strong>：崛起GEO 运营方，按照控制者的指示处理个人数据</li>
            </ul>
            <p>本 DPA 的目的是确保双方在处理个人数据时符合《中华人民共和国个人信息保护法》（PIPL）、《通用数据保护条例》（GDPR，如适用）及其他相关数据保护法律法规的要求。</p>
          </div>
        </Card>

        <Card title="二、数据处理范围" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12, fontWeight: 600 }}>2.1 处理的个人数据类别</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>账户数据：姓名、邮箱、手机号、公司名称</li>
              <li style={{ marginBottom: 6 }}>业务数据：品牌画像、产品信息、竞争对手信息、查询词（Prompts）</li>
              <li style={{ marginBottom: 6 }}>内容数据：用户提交的待优化文章、URL 等</li>
              <li>使用日志：IP 地址、访问时间、操作记录</li>
            </ul>
            <p style={{ marginBottom: 12, fontWeight: 600 }}>2.2 处理目的</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>提供品牌可见度审计（BVS 评分计算）</li>
              <li style={{ marginBottom: 6 }}>执行内容优化分析与建议生成</li>
              <li style={{ marginBottom: 6 }}>关键词发现与报告导出</li>
              <li>告警邮件与周报发送</li>
            </ul>
            <p style={{ fontWeight: 600 }}>2.3 处理方式</p>
            <p>自动化处理为主，辅以必要的人工技术支持（仅限企业版专属客户经理场景）。</p>
          </div>
        </Card>

        <Card title="三、处理者义务" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>仅按照控制者的书面指示处理个人数据，除非法律法规另有要求</li>
              <li style={{ marginBottom: 6 }}>确保接触个人数据的人员受保密义务约束</li>
              <li style={{ marginBottom: 6 }}>采取适当的技术和组织安全措施，保护个人数据安全</li>
              <li style={{ marginBottom: 6 }}>协助控制者响应数据主体权利请求</li>
              <li style={{ marginBottom: 6 }}>在发生或合理预期发生个人数据泄露时，不迟于 72 小时内通知控制者</li>
              <li style={{ marginBottom: 6 }}>根据合理要求向控制者提供审计所需的信息</li>
              <li>在服务终止后，按照控制者要求删除或返还全部个人数据（法律法规要求保留的除外）</li>
            </ul>
          </div>
        </Card>

        <Card title="四、控制者义务" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>确保其收集和提供个人数据的行为符合适用法律</li>
              <li style={{ marginBottom: 6 }}>向数据主体提供清晰、透明的隐私告知</li>
              <li style={{ marginBottom: 6 }}>建立响应数据主体权利请求的内部流程</li>
              <li style={{ marginBottom: 6 }}>如对处理指示进行变更，应提前书面通知处理者</li>
              <li>承担因超出本 DPA 范围的指示导致的责任</li>
            </ul>
          </div>
        </Card>

        <Card title="五、子处理者" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>控制者同意处理者使用以下类别的子处理者：</p>
            <ul style={{ paddingLeft: 20, marginBottom: 12 }}>
              <li style={{ marginBottom: 6 }}>云基础设施服务商（提供计算、存储和网络服务）</li>
              <li style={{ marginBottom: 6 }}>AI 引擎服务商（用于品牌审计和内容优化查询）</li>
              <li>邮件发送服务商（用于告警和报告推送）</li>
            </ul>
            <p style={{ marginBottom: 12 }}>处理者应确保与子处理者签订的数据处理协议不低于本 DPA 的保护标准。</p>
            <p>如处理者新增或更换子处理者，将提前 30 天通过系统公告或邮件通知控制者。控制者如对新子处理者有合理异议，可协商解决方案或终止服务。</p>
          </div>
        </Card>

        <Card title="六、跨境传输" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>1. 默认情况下，所有个人数据存储于中华人民共和国境内。</p>
            <p style={{ marginBottom: 12 }}>2. 企业版用户如需跨境数据传输（如使用境外 AI 引擎），应确保遵守 PIPL 等法规关于跨境传输的要求，包括但不限于安全评估、标准合同或认证。</p>
            <p>3. 处理者不主动将个人数据传输至境外，法律另有规定或经控制者明确同意的除外。</p>
          </div>
        </Card>

        <Card title="七、数据保留" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>账户数据：保留至账户注销后 90 天</li>
              <li style={{ marginBottom: 6 }}>业务数据（品牌、查询词、审计报告）：账户存续期间保留，注销后 30 天内删除</li>
              <li style={{ marginBottom: 6 }}>内容数据：保留最近 30 天的优化记录，更早记录自动清理</li>
              <li>访问日志：保留 6 个月用于安全审计</li>
            </ul>
          </div>
        </Card>

        <Card title="八、数据泄露通知" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>如发生或合理怀疑发生个人数据泄露事件，处理者将：</p>
            <ol style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>在知晓事件后不迟于 72 小时内通知控制者</li>
              <li style={{ marginBottom: 6 }}>提供泄露事件的性质、可能影响和已采取/拟采取的措施</li>
              <li style={{ marginBottom: 6 }}>协助控制者评估是否需要向监管机构和数据主体通报</li>
              <li>采取合理措施减轻事件造成的不利影响</li>
            </ol>
          </div>
        </Card>

        <Card title="九、数据主体权利协助" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>处理者将在合理范围内协助控制者履行数据主体权利响应义务，包括：</p>
            <ul style={{ paddingLeft: 20 }}>
              <li style={{ marginBottom: 6 }}>提供/导出特定数据主体的个人数据副本</li>
              <li style={{ marginBottom: 6 }}>更正不准确的个人数据</li>
              <li style={{ marginBottom: 6 }}>删除特定数据主体的个人数据（法律法规要求保留的除外）</li>
              <li>就权利响应提供必要的技术支持</li>
            </ul>
            <p style={{ marginTop: 12 }}>控制者可通过「系统设置 → 数据权利」自助执行部分操作，其余可通过工单提交请求。</p>
          </div>
        </Card>

        <Card title="十、审计权" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>控制者（或其指定的独立第三方审计机构）可在提前 15 天书面通知的前提下，对处理者与本 DPA 相关的安全措施进行审计。</p>
            <p>审计费用由控制者承担，但处理者违反本 DPA 导致的审计除外。审计应在处理者正常工作时间进行，避免对业务运营造成不合理干扰。</p>
          </div>
        </Card>

        <Card title="十一、责任限制" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>除故意或重大过失外，任何一方在本 DPA 项下的累计赔偿责任以引发责任的事件发生前 12 个月内控制者就相关服务支付的费用总额为上限。本条款不限制任何一方因数据保护违法而可能承担的法定责任。</p>
          </div>
        </Card>

        <Card title="十二、协议终止" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>服务终止后，处理者应根据控制者的指示，在 30 天内删除或返还所有个人数据（法律法规要求保留的除外），并在删除后向控制者提供书面确认。</p>
            <p>本 DPA 在服务终止后对仍有约束力的条款（如保密、责任限制、数据删除义务）继续有效。</p>
          </div>
        </Card>

        <Card title="十三、适用法律与争议解决" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p style={{ marginBottom: 12 }}>本 DPA 的订立、履行和解释适用中华人民共和国法律（不包括港澳台地区法律）。</p>
            <p>因本 DPA 产生的争议，双方应友好协商解决；协商不成的，任何一方可向处理者所在地有管辖权的人民法院提起诉讼。</p>
          </div>
        </Card>

        <Card title="十四、联系方式" compact>
          <div style={{ lineHeight: 1.8, color: 'var(--text-secondary)' }}>
            <p>如对本 DPA 或数据处理实践有任何疑问，请通过「提交工单」或邮箱 dpo@mygeo.example 联系我们的数据保护官（DPO）。</p>
          </div>
        </Card>
      </div>
    </div>
  )
}

export default DPA

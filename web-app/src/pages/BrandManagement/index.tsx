import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Input, Textarea } from '@/components/Input'
import { Button } from '@/components/Button'
import { Table, Modal, type TableColumn } from '@/components'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import type { BrandProfile, AutocompleteCandidate, KnowledgeSearchItem } from '@/types/api'
import '../Dashboard/Dashboard.scss'

const BrandManagement: React.FC = () => {
  const { t } = useTranslation()
  const brands = useAppStore(s => s.brands)
  const addBrand = useAppStore(s => s.addBrand)
  const updateBrand = useAppStore(s => s.updateBrand)
  const deleteBrand = useAppStore(s => s.deleteBrand)
  const setCurrentBrand = useAppStore(s => s.setCurrentBrand)
  const showToast = useAppStore(s => s.showToast)

  const [modalOpen, setModalOpen] = useState(false)
  const [editingBrand, setEditingBrand] = useState<BrandProfile | null>(null)
  const [form, setForm] = useState<Partial<BrandProfile>>({})
  const [autocompleteName, setAutocompleteName] = useState('')
  const [autoLoading, setAutoLoading] = useState(false)
  const [kbQuery, setKbQuery] = useState('')
  const [kbResults, setKbResults] = useState<KnowledgeSearchItem[]>([])
  const [kbLoading, setKbLoading] = useState(false)

  const openAdd = () => {
    setEditingBrand(null)
    setForm({ prompts: [], competitors: [], products: [], aliases: [] })
    setModalOpen(true)
  }

  const openEdit = (b: BrandProfile) => {
    setEditingBrand(b)
    setForm({ ...b })
    setModalOpen(true)
  }

  const handleSave = () => {
    if (!form.name?.trim()) return showToast('品牌名称必填', 'error')
    if (!form.prompts || form.prompts.length === 0) {
      return showToast('至少添加一个查询词 (Prompt)', 'error')
    }
    // 重名校验：store 按 name 匹配更新/删除，重名会导致误改误删 + React key 冲突
    const dup = brands.some(b => b.name === form.name && b.name !== editingBrand?.name)
    if (dup) return showToast('已存在同名品牌，请换一个名称', 'error')
    if (editingBrand) {
      updateBrand(editingBrand.name, form as BrandProfile)
      showToast('已更新品牌：' + form.name, 'success')
    } else {
      addBrand(form as BrandProfile)
      showToast('已添加品牌：' + form.name, 'success')
    }
    setModalOpen(false)
  }

  const handleDelete = (name: string) => {
    if (!confirm(`确认删除品牌「${name}」?`)) return
    deleteBrand(name)
    showToast('已删除', 'success')
  }

  const handleAutocomplete = async () => {
    if (!autocompleteName.trim()) return
    setAutoLoading(true)
    try {
      const c: AutocompleteCandidate = await api.brandAutocomplete(autocompleteName.trim())
      setForm(prev => ({
        ...prev,
        name: c.brand_name || prev.name || autocompleteName,
        aliases: c.brand_aliases ?? prev.aliases,
        domain: c.brand_domain ?? prev.domain,
        products: c.products ?? prev.products,
        prompts: c.prompts ?? prev.prompts,
        competitors: c.competitors ?? prev.competitors,
        industry: c.industry ?? prev.industry,
        category: c.category ?? prev.category,
        company: c.company ?? prev.company
      }))
      showToast('AI 补全完成', 'success')
    } catch (e: any) {
      showToast(e.message || '补全失败', 'error')
    } finally {
      setAutoLoading(false)
    }
  }

  const handleKbSearch = async () => {
    if (!kbQuery.trim()) return
    setKbLoading(true)
    try {
      const r = await api.brandKnowledgeSearch(kbQuery.trim(), 8)
      setKbResults(r.result)
    } catch (e: any) {
      showToast(e.message || '搜索失败', 'error')
    } finally {
      setKbLoading(false)
    }
  }

  const applyKbResult = (item: KnowledgeSearchItem) => {
    setForm(prev => ({
      ...prev,
      name: item.brand_name || prev.name,
      aliases: item.brand_aliases ?? prev.aliases,
      domain: item.brand_domain ?? prev.domain,
      industry: item.industry ?? prev.industry,
      category: item.category ?? prev.category,
      products: item.products ?? prev.products,
      company: {
        name: item.company_name ?? '',
        domain: item.company_domain,
        headquarters: item.hq,
        founded_year: item.founded_year,
        description: item.description,
        credit_code: item.credit_code,
        legal_representative: item.legal_person,
        established_date: item.registered_date,
        registered_capital: item.capital,
        province: item.province,
        registered_address: item.address,
        company_type: item.company_type,
        business_scope: item.business_scope
      }
    }))
    showToast(`已从 ${item.source_label} 应用数据`, 'success')
  }

  const addList = (field: 'aliases' | 'products' | 'prompts') => {
    setForm(prev => ({ ...prev, [field]: [...(prev[field] || []), ''] }))
  }
  const updateList = (field: 'aliases' | 'products' | 'prompts', i: number, v: string) => {
    setForm(prev => {
      const arr = [...(prev[field] || [])]
      arr[i] = v
      return { ...prev, [field]: arr }
    })
  }
  const removeList = (field: 'aliases' | 'products' | 'prompts', i: number) => {
    setForm(prev => ({ ...prev, [field]: (prev[field] || []).filter((_, idx) => idx !== i) }))
  }
  const addCompetitor = () => {
    setForm(prev => ({ ...prev, competitors: [...(prev.competitors || []), { name: '' }] }))
  }
  const updateCompetitor = (i: number, patch: any) => {
    setForm(prev => {
      const arr = [...(prev.competitors || [])]
      arr[i] = { ...arr[i], ...patch }
      return { ...prev, competitors: arr }
    })
  }
  const removeCompetitor = (i: number) => {
    setForm(prev => ({ ...prev, competitors: (prev.competitors || []).filter((_, idx) => idx !== i) }))
  }

  const cols: TableColumn<BrandProfile>[] = [
    { key: 'name', title: t('brandManagement.brandName'), dataIndex: 'name', sortable: true },
    { key: 'domain', title: t('brandManagement.brandDomain'), dataIndex: 'domain' },
    { key: 'industry', title: t('brandManagement.brandIndustry'), dataIndex: 'industry' },
    { key: 'category', title: t('brandManagement.brandCategory'), dataIndex: 'category' },
    {
      key: 'prompts',
      title: 'Prompts',
      render: (r) => <span style={{
        padding: '2px 8px', borderRadius: 999,
        background: 'var(--status-info-bg)', color: 'var(--status-info)',
        fontSize: 12
      }}>{r.prompts?.length ?? 0}</span>
    },
    {
      key: 'competitors',
      title: t('brandManagement.competitors'),
      render: (r) => <span style={{
        padding: '2px 8px', borderRadius: 999,
        background: 'var(--status-error-bg)', color: 'var(--status-error)',
        fontSize: 12
      }}>{r.competitors?.length ?? 0}</span>
    },
    {
      key: 'actions',
      title: '操作',
      align: 'right',
      render: (r) => (
        <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}>
          <Button size="xs" variant="ghost" onClick={() => { setCurrentBrand(r); showToast('已选中：' + r.name, 'info') }}>选中</Button>
          <Button size="xs" variant="secondary" onClick={() => openEdit(r)}>编辑</Button>
          <Button size="xs" variant="danger" onClick={() => handleDelete(r.name)}>删除</Button>
        </div>
      )
    }
  ]

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{t('brandManagement.title')}</h1>
          <p className="page-subtitle">{t('brandManagement.subtitle')}</p>
        </div>
        <Button onClick={openAdd}>+ {t('brandManagement.addBrand')}</Button>
      </div>

      <Card title={t('brandManagement.brandList')} subtitle={`${brands.length} brands`} compact>
        <Table columns={cols} dataSource={brands} rowKey="name" striped pagination pageSize={5} />
      </Card>

      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editingBrand ? t('brandManagement.editBrand') : t('brandManagement.addBrand')}
        size="xl"
        footer={
          <>
            <Button variant="secondary" onClick={() => setModalOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={handleSave}>{t('common.save')}</Button>
          </>
        }
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Card title="🧠 AI 智能补全" variant="outline" compact style={{ borderStyle: 'dashed' }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
              <Input
                label={t('brandManagement.brandName')}
                value={autocompleteName}
                onChange={(e) => setAutocompleteName(e.target.value)}
                placeholder="输入品牌名进行AI补全"
              />
              <Button onClick={handleAutocomplete} loading={autoLoading} style={{ marginBottom: 2 }}>
                ✨ {t('brandManagement.autocomplete')}
              </Button>
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginTop: 6 }}>
              {t('brandManagement.autocompleteHint')}
            </div>
          </Card>

          <Card title="📚 知识库搜索" variant="outline" compact style={{ borderStyle: 'dashed' }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
              <Input
                label={t('brandManagement.knowledgeSearch')}
                isSearch
                value={kbQuery}
                onChange={(e) => setKbQuery(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleKbSearch()}
                placeholder="搜索品牌或公司名"
              />
              <Button variant="secondary" onClick={handleKbSearch} loading={kbLoading} style={{ marginBottom: 2 }}>
                🔍 搜索
              </Button>
            </div>
            {kbResults.length > 0 && (
              <div style={{ marginTop: 8, maxHeight: 180, overflow: 'auto', display: 'flex', flexDirection: 'column', gap: 6 }}>
                {kbResults.map((item, i) => (
                  <div key={i} style={{
                    padding: 8,
                    borderRadius: 6,
                    background: 'var(--surface-secondary)',
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    gap: 8
                  }}>
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <div style={{ fontSize: 13, fontWeight: 600 }}>{item.brand_name}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
                        <span style={{
                          padding: '1px 6px', borderRadius: 999,
                          background: item.source === 'sinofacts' ? 'var(--status-info-bg)' : 'var(--status-warning-bg)',
                          color: item.source === 'sinofacts' ? 'var(--status-info)' : 'var(--status-warning)',
                          marginRight: 6
                        }}>
                          {item.source}
                        </span>
                        {item.industry} · {item.hq} · 匹配度 {item.score.toFixed(0)}
                      </div>
                    </div>
                    <Button size="xs" variant="primary" onClick={() => applyKbResult(item)}>应用</Button>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Input label={t('brandManagement.brandName')} required value={form.name || ''}
              onChange={(e) => setForm(p => ({ ...p, name: e.target.value }))} />
            <Input label={t('brandManagement.brandDomain')} value={form.domain || ''}
              onChange={(e) => setForm(p => ({ ...p, domain: e.target.value }))}
              placeholder="example.com" />
            <Input label={t('brandManagement.brandIndustry')} value={form.industry || ''}
              onChange={(e) => setForm(p => ({ ...p, industry: e.target.value }))} />
            <Input label={t('brandManagement.brandCategory')} value={form.category || ''}
              onChange={(e) => setForm(p => ({ ...p, category: e.target.value }))} />
            <Input label={t('brandManagement.brandMarket')} value={form.market || ''}
              onChange={(e) => setForm(p => ({ ...p, market: e.target.value }))}
              placeholder="cn / us / jp / global" />
            <Input label={t('brandManagement.brandLanguage')} value={form.language || ''}
              onChange={(e) => setForm(p => ({ ...p, language: e.target.value }))}
              placeholder="zh / en / ja" />
          </div>

          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' }}>
                {t('brandManagement.brandAliases')}
              </label>
              <Button size="xs" variant="ghost" onClick={() => addList('aliases')}>+ 添加</Button>
            </div>
            {(form.aliases || []).map((a, i) => (
              <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                <Input value={a} onChange={(e) => updateList('aliases', i, e.target.value)} />
                <Button size="xs" variant="danger" onClick={() => removeList('aliases', i)}>×</Button>
              </div>
            ))}
          </div>

          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
              <label style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-secondary)' }}>
                {t('brandManagement.brandProducts')}
              </label>
              <Button size="xs" variant="ghost" onClick={() => addList('products')}>+ 添加</Button>
            </div>
            {(form.products || []).map((a, i) => (
              <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                <Input value={a} onChange={(e) => updateList('products', i, e.target.value)} />
                <Button size="xs" variant="danger" onClick={() => removeList('products', i)}>×</Button>
              </div>
            ))}
          </div>

          <Card title={t('brandManagement.prompts')} subtitle={t('brandManagement.promptsHint')} compact>
            {(form.prompts || []).length === 0 && (
              <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 13 }}>
                请至少添加一个业务查询词（Prompt）
              </div>
            )}
            {(form.prompts || []).map((a, i) => (
              <div key={i} style={{ display: 'flex', gap: 4, marginBottom: 4 }}>
                <Input value={a} onChange={(e) => updateList('prompts', i, e.target.value)}
                  placeholder="如：最好的 CRM 软件推荐" />
                <Button size="xs" variant="danger" onClick={() => removeList('prompts', i)}>×</Button>
              </div>
            ))}
            <Button variant="ghost" size="sm" onClick={() => addList('prompts')}>+ {t('brandManagement.addPrompt')}</Button>
          </Card>

          <Card title={t('brandManagement.competitors')} compact>
            {(form.competitors || []).map((c, i) => (
              <div key={i} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr auto', gap: 4, marginBottom: 4 }}>
                <Input placeholder={t('brandManagement.competitorName')} value={c.name || ''}
                  onChange={(e) => updateCompetitor(i, { name: e.target.value })} />
                <Input placeholder={t('brandManagement.competitorDomain')} value={c.domain || ''}
                  onChange={(e) => updateCompetitor(i, { domain: e.target.value })} />
                <Input placeholder="别名 (逗号分隔)" value={(c.aliases || []).join(',')}
                  onChange={(e) => updateCompetitor(i, { aliases: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })} />
                <Button size="xs" variant="danger" onClick={() => removeCompetitor(i)}>×</Button>
              </div>
            ))}
            <Button variant="ghost" size="sm" onClick={addCompetitor}>+ {t('brandManagement.addCompetitor')}</Button>
          </Card>

          <Card title={t('brandManagement.brandCompany')} compact>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Input label={t('brandManagement.companyName')} value={form.company?.name || ''}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), name: e.target.value } }))} />
              <Input label={t('brandManagement.companyDomain')} value={form.company?.domain || ''}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), domain: e.target.value } }))} />
              <Input label={t('brandManagement.companyIndustry')} value={form.company?.industry || ''}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), industry: e.target.value } }))} />
              <Input label={t('brandManagement.companyHQ')} value={form.company?.headquarters || ''}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), headquarters: e.target.value } }))} />
              <Input label={t('brandManagement.companyFounded')} type="number" value={form.company?.founded_year || '' as any}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), founded_year: Number(e.target.value) } }))} />
              <Input label={t('brandManagement.creditCode')} value={form.company?.credit_code || ''}
                onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), credit_code: e.target.value } }))} />
            </div>
            <Textarea label={t('brandManagement.companyDesc')} rows={2} style={{ marginTop: 12 }}
              value={form.company?.description || ''}
              onChange={(e) => setForm(p => ({ ...p, company: { ...(p.company || {} as any), description: e.target.value } }))} />
          </Card>
        </div>
      </Modal>
    </div>
  )
}

export default BrandManagement

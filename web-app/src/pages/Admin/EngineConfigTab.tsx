import React, { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/Card'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { useAppStore } from '@/store/useAppStore'
import api from '@/services/api'
import './EngineConfigTab.scss'

// 引擎配置项
interface EngineConfig {
  key: string
  name: string
  icon: string
  apiKey: string
  baseUrl: string
  model: string
  webSearch: boolean
  enabled: boolean
  configured: boolean
}

// 引擎定义
const ENGINE_DEFINITIONS: Omit<EngineConfig, 'apiKey' | 'baseUrl' | 'model' | 'webSearch' | 'enabled' | 'configured'>[] = [
  { key: 'openai', name: 'ChatGPT', icon: '🤖' },
  { key: 'perplexity', name: 'Perplexity', icon: '🔍' },
  { key: 'gemini', name: 'Gemini', icon: '✨' },
  { key: 'claude', name: 'Claude', icon: '🧠' },
  { key: 'grok', name: 'Grok', icon: '⚡' },
  { key: 'qwen', name: '通义千问', icon: '☁️' },
  { key: 'glm', name: '智谱GLM', icon: '💡' },
  { key: 'deepseek', name: 'DeepSeek', icon: '🌊' },
  { key: 'kimi', name: 'Kimi', icon: '🌙' },
  { key: 'ernie', name: '文心一言', icon: '📝' },
  { key: 'doubao', name: '豆包', icon: '🫘' },
  { key: 'xiaomi', name: '小米大模型', icon: '📱' },
  { key: 'xunfei', name: '讯飞星火', icon: '🔥' },
  { key: 'yuanbao', name: '元宝/混元', icon: '🎮' },
]

const EngineConfigTab: React.FC = () => {
  const { t } = useTranslation()
  const showToast = useAppStore(s => s.showToast)

  const [engines, setEngines] = useState<EngineConfig[]>([])
  const [loading, setLoading] = useState(false)
  const [testing, setTesting] = useState<string | null>(null)
  const [testResult, setTestResult] = useState<Record<string, { success: boolean; message: string }>>({})

  // 加载引擎配置
  const loadEngines = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.admin.settings({ category: 'engines' })
      const settings = res?.settings ?? []

      const engineConfigs: EngineConfig[] = ENGINE_DEFINITIONS.map(def => {
        const apiKeySetting = settings.find((s: any) => s.key === `GEO_${def.key.toUpperCase()}_KEY`)
        const baseUrlSetting = settings.find((s: any) => s.key === `GEO_${def.key.toUpperCase()}_BASE`)
        const modelSetting = settings.find((s: any) => s.key === `GEO_${def.key.toUpperCase()}_MODEL`)
        const webSearchSetting = settings.find((s: any) => s.key === `GEO_${def.key.toUpperCase()}_WEB_SEARCH`)
        const enabledSetting = settings.find((s: any) => s.key === `GEO_${def.key.toUpperCase()}_ENABLED`)

        return {
          ...def,
          apiKey: apiKeySetting?.value || '',
          baseUrl: baseUrlSetting?.value || '',
          model: modelSetting?.value || '',
          webSearch: webSearchSetting?.value !== 'false',
          enabled: enabledSetting?.value !== 'false',
          configured: Boolean(apiKeySetting?.value)
        }
      })

      setEngines(engineConfigs)
    } catch {
      showToast(t('admin.engineLoadFailed'), 'error')
    } finally {
      setLoading(false)
    }
  }, [showToast, t])

  useEffect(() => {
    loadEngines()
  }, [loadEngines])

  // 测试引擎连通性
  const testEngine = async (engineKey: string) => {
    setTesting(engineKey)
    setTestResult(prev => ({ ...prev, [engineKey]: { success: false, message: '' } }))

    try {
      const res = await api.admin.testEngine(engineKey)
      setTestResult(prev => ({
        ...prev,
        [engineKey]: { success: res?.success || false, message: res?.message || '' }
      }))
      showToast(
        res?.success ? t('admin.engineTestSuccess') : t('admin.engineTestFailed'),
        res?.success ? 'success' : 'error'
      )
    } catch (e: any) {
      setTestResult(prev => ({
        ...prev,
        [engineKey]: { success: false, message: e?.message || 'Test failed' }
      }))
      showToast(t('admin.engineTestFailed'), 'error')
    } finally {
      setTesting(null)
    }
  }

  // 保存引擎配置
  const saveEngine = async (engine: EngineConfig) => {
    try {
      // 保存 API Key
      if (engine.apiKey) {
        await api.admin.updateSetting(`GEO_${engine.key.toUpperCase()}_KEY`, engine.apiKey)
      }
      // 保存 Base URL
      if (engine.baseUrl) {
        await api.admin.updateSetting(`GEO_${engine.key.toUpperCase()}_BASE`, engine.baseUrl)
      }
      // 保存 Model
      if (engine.model) {
        await api.admin.updateSetting(`GEO_${engine.key.toUpperCase()}_MODEL`, engine.model)
      }
      // 保存 Web Search 设置
      await api.admin.updateSetting(`GEO_${engine.key.toUpperCase()}_WEB_SEARCH`, engine.webSearch ? 'true' : 'false')
      // 保存 Enabled 设置
      await api.admin.updateSetting(`GEO_${engine.key.toUpperCase()}_ENABLED`, engine.enabled ? 'true' : 'false')

      showToast(t('admin.engineSaved'), 'success')
      loadEngines()
    } catch (e: any) {
      showToast(e?.message || t('admin.engineSaveFailed'), 'error')
    }
  }

  // 更新引擎配置
  const updateEngine = (key: string, field: keyof EngineConfig, value: string | boolean) => {
    setEngines(prev => prev.map(e =>
      e.key === key ? { ...e, [field]: value } : e
    ))
  }

  return (
    <div className="engine-config-tab">
      <Card title={t('admin.engineConfigTitle')} compact>
        <p className="engine-config-desc">{t('admin.engineConfigDesc')}</p>

        {loading ? (
          <div className="engine-config-loading">{t('common.loading')}</div>
        ) : (
          <div className="engine-grid">
            {engines.map(engine => (
              <div key={engine.key} className={`engine-card ${engine.configured ? 'configured' : ''} ${!engine.enabled ? 'disabled' : ''}`}>
                <div className="engine-card-header">
                  <span className="engine-icon">{engine.icon}</span>
                  <span className="engine-name">{engine.name}</span>
                  <label className="engine-toggle">
                    <input
                      type="checkbox"
                      checked={engine.enabled}
                      onChange={(e) => updateEngine(engine.key, 'enabled', e.target.checked)}
                    />
                    <span className="engine-toggle-slider"></span>
                  </label>
                </div>

                <div className="engine-card-body">
                  <div className="engine-field">
                    <label>{t('admin.engineApiKey')}</label>
                    <Input
                      type="password"
                      value={engine.apiKey}
                      onChange={(e) => updateEngine(engine.key, 'apiKey', e.target.value)}
                      placeholder={t('admin.engineApiKeyPlaceholder')}
                    />
                  </div>

                  <div className="engine-field">
                    <label>{t('admin.engineBaseUrl')}</label>
                    <Input
                      value={engine.baseUrl}
                      onChange={(e) => updateEngine(engine.key, 'baseUrl', e.target.value)}
                      placeholder={t('admin.engineBaseUrlPlaceholder')}
                    />
                  </div>

                  <div className="engine-field">
                    <label>{t('admin.engineModel')}</label>
                    <Input
                      value={engine.model}
                      onChange={(e) => updateEngine(engine.key, 'model', e.target.value)}
                      placeholder={t('admin.engineModelPlaceholder')}
                    />
                  </div>

                  <div className="engine-field engine-checkbox">
                    <label>
                      <input
                        type="checkbox"
                        checked={engine.webSearch}
                        onChange={(e) => updateEngine(engine.key, 'webSearch', e.target.checked)}
                      />
                      {t('admin.engineWebSearch')}
                    </label>
                  </div>
                </div>

                <div className="engine-card-footer">
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => testEngine(engine.key)}
                    disabled={testing === engine.key || !engine.configured}
                  >
                    {testing === engine.key ? t('common.testing') : t('admin.engineTest')}
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => saveEngine(engine)}
                  >
                    {t('common.save')}
                  </Button>
                </div>

                {testResult[engine.key] && (
                  <div className={`engine-test-result ${testResult[engine.key].success ? 'success' : 'error'}`}>
                    {testResult[engine.key].message}
                  </div>
                )}

                <div className="engine-status">
                  {engine.configured ? (
                    <span className="status-configured">{t('admin.engineConfigured')}</span>
                  ) : (
                    <span className="status-not-configured">{t('admin.engineNotConfigured')}</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}

export default EngineConfigTab

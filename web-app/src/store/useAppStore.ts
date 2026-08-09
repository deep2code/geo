import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import type { BrandProfile, VisibilityReport, OptimizationResponse, WhitelabelMeta } from '@/types/api'

export type ThemeMode = 'light' | 'dark' | 'brand'
export type UIDensity = 'compact' | 'comfortable' | 'spacious'

export const DEFAULT_WHITELABEL: WhitelabelMeta = {
  brand_name: 'MyGEO',
  primary_color: '#6366f1'
}

interface WhitelabelSlice {
  whitelabel: WhitelabelMeta
  setWhitelabel: (w: WhitelabelMeta) => void
  applyWhitelabelDOM: (w: WhitelabelMeta) => void
}

interface SettingsSlice {
  theme: ThemeMode
  density: UIDensity
  apiBaseUrl: string
  apiTimeout: number
  setTheme: (t: ThemeMode) => void
  setDensity: (d: UIDensity) => void
  setApiBaseUrl: (u: string) => void
  setApiTimeout: (t: number) => void
  resetSettings: () => void
}

interface BrandSlice {
  brands: BrandProfile[]
  currentBrand: BrandProfile | null
  lastReport: VisibilityReport | null
  addBrand: (b: BrandProfile) => void
  updateBrand: (name: string, b: Partial<BrandProfile>) => void
  deleteBrand: (name: string) => void
  setCurrentBrand: (b: BrandProfile | null) => void
  setLastReport: (r: VisibilityReport | null) => void
}

interface ContentSlice {
  lastOptimization: OptimizationResponse | null
  setLastOptimization: (r: OptimizationResponse | null) => void
}

interface UISlice {
  sidebarOpen: boolean
  toast: { message: string; type: 'success' | 'error' | 'info' | 'warning' } | null
  setSidebarOpen: (v: boolean) => void
  showToast: (message: string, type?: 'success' | 'error' | 'info' | 'warning') => void
  clearToast: () => void
}

export type AppState = WhitelabelSlice & SettingsSlice & BrandSlice & ContentSlice & UISlice

const DEFAULT_BRAND: BrandProfile = {
  name: '示例科技',
  aliases: ['ExampleTech'],
  domain: 'example.com',
  products: ['产品A', '产品B'],
  industry: '企业软件',
  category: 'CRM',
  prompts: [
    '最好的 CRM 软件推荐',
    '中小企业客户管理系统对比',
    '国内 SaaS CRM 排行榜'
  ],
  competitors: [
    { name: '竞品A', aliases: ['CompA'], domain: 'compa.com' },
    { name: '竞品B', aliases: ['CompB'], domain: 'compb.com' }
  ]
}

export const useAppStore = create<AppState>()(
  persist(
    (set, get) => ({
      whitelabel: DEFAULT_WHITELABEL,
      setWhitelabel: (w) => {
        set({ whitelabel: w })
        get().applyWhitelabelDOM(w)
      },
      applyWhitelabelDOM: (w) => {
        if (typeof document === 'undefined') return
        const root = document.documentElement
        if (w.primary_color) {
          root.style.setProperty('--wl-primary-color', w.primary_color)
          root.style.setProperty('--brand-primary', w.primary_color)
        }
        if (w.brand_name) {
          document.title = w.brand_name
        }
        if (w.favicon_url) {
          let link = document.querySelector('link[rel="icon"]') as HTMLLinkElement | null
          if (!link) {
            link = document.createElement('link')
            link.rel = 'icon'
            document.head.appendChild(link)
          }
          link.href = w.favicon_url
        }
      },

      theme: 'light',
      density: 'comfortable',
      apiBaseUrl: '',
      apiTimeout: 120,
      setTheme: (t) => {
        set({ theme: t })
        if (typeof document !== 'undefined') {
          const root = document.documentElement
          if (t === 'light') {
            root.removeAttribute('data-theme')
          } else {
            root.setAttribute('data-theme', t)
          }
        }
      },
      setDensity: (d) => set({ density: d }),
      setApiBaseUrl: (u) => set({ apiBaseUrl: u }),
      setApiTimeout: (t) => set({ apiTimeout: t }),
      resetSettings: () => set({
        theme: 'light',
        density: 'comfortable',
        apiBaseUrl: '',
        apiTimeout: 120
      }),

      brands: [DEFAULT_BRAND],
      currentBrand: DEFAULT_BRAND,
      lastReport: null,
      addBrand: (b) => set({ brands: [...get().brands, b] }),
      updateBrand: (name, patch) => set({
        brands: get().brands.map(b => b.name === name ? { ...b, ...patch } : b),
        currentBrand: get().currentBrand?.name === name
          ? { ...get().currentBrand!, ...patch }
          : get().currentBrand
      }),
      deleteBrand: (name) => {
        const brands = get().brands.filter(b => b.name !== name)
        set({
          brands,
          currentBrand: get().currentBrand?.name === name
            ? brands[0] ?? null
            : get().currentBrand
        })
      },
      setCurrentBrand: (b) => set({ currentBrand: b }),
      setLastReport: (r) => set({ lastReport: r }),

      lastOptimization: null,
      setLastOptimization: (r) => set({ lastOptimization: r }),

      sidebarOpen: true,
      toast: null,
      setSidebarOpen: (v) => set({ sidebarOpen: v }),
      showToast: (message, type = 'info') => {
        set({ toast: { message, type } })
        setTimeout(() => get().clearToast(), 3000)
      },
      clearToast: () => set({ toast: null })
    }),
    {
      name: 'geo-app-storage',
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({
        whitelabel: s.whitelabel,
        theme: s.theme,
        density: s.density,
        apiBaseUrl: s.apiBaseUrl,
        apiTimeout: s.apiTimeout,
        brands: s.brands,
        currentBrand: s.currentBrand
      }),
      onRehydrateStorage: () => (state) => {
        if (state) {
          if (state.theme !== 'light' && typeof document !== 'undefined') {
            document.documentElement.setAttribute('data-theme', state.theme)
          }
          if (state.whitelabel && typeof document !== 'undefined') {
            state.applyWhitelabelDOM(state.whitelabel)
          }
        }
      }
    }
  )
)

export default useAppStore

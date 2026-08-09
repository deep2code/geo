import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import zhCN from './locales/zh-CN.json'
import en from './locales/en.json'
import ja from './locales/ja.json'

export type LanguageCode = 'zh-CN' | 'en' | 'ja'

export const LANGUAGES: { code: LanguageCode; label: string; flag: string }[] = [
  { code: 'zh-CN', label: '简体中文', flag: '🇨🇳' },
  { code: 'en', label: 'English', flag: '🇺🇸' },
  { code: 'ja', label: '日本語', flag: '🇯🇵' }
]

const savedLang = (typeof localStorage !== 'undefined'
  ? localStorage.getItem('geo-lang') as LanguageCode | null
  : null)

const browserLang = (() => {
  if (typeof navigator === 'undefined') return 'en'
  const nav = navigator.language
  if (nav.startsWith('zh')) return 'zh-CN'
  if (nav.startsWith('ja')) return 'ja'
  return 'en'
})()

const fallbackLng = 'en'

i18n
  .use(initReactI18next)
  .init({
    resources: {
      'zh-CN': { translation: zhCN },
      'en': { translation: en },
      'ja': { translation: ja }
    },
    lng: savedLang ?? browserLang,
    fallbackLng,
    supportedLngs: ['zh-CN', 'en', 'ja'],
    interpolation: {
      escapeValue: false
    },
    react: {
      useSuspense: false
    }
  })

export const changeLanguage = (code: LanguageCode) => {
  i18n.changeLanguage(code)
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem('geo-lang', code)
  }
}

export const getCurrentLanguage = (): LanguageCode => {
  const lng = i18n.language
  if (lng.startsWith('zh')) return 'zh-CN'
  if (lng.startsWith('ja')) return 'ja'
  return 'en'
}

export default i18n

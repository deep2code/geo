import React, { useEffect } from 'react'
import { BrowserRouter } from 'react-router-dom'
import AppRoutes from '@/router/AppRoutes'
import { useAppStore, DEFAULT_WHITELABEL } from '@/store/useAppStore'
import api from '@/services/api'
import '@/styles/tokens.scss'
import '@/styles/reset.scss'
import '@/i18n'

const App: React.FC = () => {
  const setWhitelabel = useAppStore(s => s.setWhitelabel)

  useEffect(() => {
    const loadWhitelabel = async () => {
      try {
        const meta = await api.metaWhitelabel()
        if (meta?.brand_name) {
          setWhitelabel(meta)
        }
      } catch {
        setWhitelabel(DEFAULT_WHITELABEL)
      }
    }
    loadWhitelabel()
  }, [setWhitelabel])

  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  )
}

export default App

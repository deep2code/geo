import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      // autoUpdate：新版本自动在后台下载并激活，与 Settings 页面声明一致
      registerType: 'autoUpdate',
      includeAssets: ['favicon.svg', 'robots.txt', 'llms.txt', 'llms-full.txt', 'sitemap.xml'],
      manifest: {
        name: '崛起GEO · AI 平台',
        short_name: '崛起GEO',
        description: 'AI 引擎可见度优化平台 — 生成式引擎优化（GEO）系统',
        theme_color: '#6366f1',
        background_color: '#ffffff',
        display: 'standalone',
        orientation: 'portrait-primary',
        start_url: '/',
        icons: [
          {
            src: '/favicon.svg',
            sizes: 'any',
            type: 'image/svg+xml',
            purpose: 'any'
          }
        ]
      },
      workbox: {
        // 预缓存：构建产物中的静态资源（JS/CSS/HTML/SVG/manifest 等）
        globPatterns: ['**/*.{js,css,html,svg,txt,xml,webmanifest}'],
        // 不缓存 API 请求（GEO 数据实时性要求高）
        navigateFallback: '/index.html'
      }
    })
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src')
    }
  },
  css: {
    preprocessorOptions: {
      scss: {
        // Dart Sass 现代 JS API：消除 legacy-js-api 弃用警告（Dart Sass 2.0 移除旧 API）
        api: 'modern'
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:7070',
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: path.resolve(__dirname, '../internal/server/web/dist'),
    emptyOutDir: true,
    sourcemap: false,
    // 明确构建目标（与 tsconfig.json target 一致），避免注入不必要的 polyfill
    target: 'es2020',
    // 关闭 gzip 体积报告，加速构建（不影响产物质量）
    reportCompressedSize: false,
    // 提升单个 chunk 体积告警阈值（前端 SPA 业务代码密集，1000KB 以下不告警）
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        // 固定文件命名（[name]-[hash].js），带目录前缀便于排查
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]',
        // 函数式 manualChunks：按模块路径精确分配，避免 Rollup 对象式配置的自动合并问题
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('react-router')) return 'router-vendor'
          if (id.includes('react-i18next') || id.includes('i18next')) return 'i18n'
          if (id.includes('zustand')) return 'state'
          // react/react-dom/scheduler 归入 react-vendor（放在最后匹配，避免 react-router 命中）
          if (id.includes('react') || id.includes('scheduler')) return 'react-vendor'
        }
      }
    }
  }
})

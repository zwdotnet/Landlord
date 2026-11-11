import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

export default defineConfig({
  base: process.env.VITE_BASE_PATH || '/',
  plugins: [vue(), {
    name: 'landlord-service-worker-version',
    writeBundle(options, bundle) {
      const fingerprint = createHash('sha256').update(Object.keys(bundle).sort().join('\n'))
      for (const file of ['sw.js', 'offline.html', 'favicon-192.png', 'favicon-512.png']) {
        fingerprint.update(readFileSync(resolve('public', file)))
      }
      const version = fingerprint.digest('hex').slice(0, 16)
      const source = readFileSync(resolve('public/sw.js'), 'utf8').replaceAll('__CACHE_VERSION__', version)
      writeFileSync(resolve(options.dir || 'dist', 'sw.js'), source)
    },
  }],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8910',
        changeOrigin: true
      },
      '/uploads': {
        target: 'http://localhost:8910',
        changeOrigin: true
      }
    }
  }
})

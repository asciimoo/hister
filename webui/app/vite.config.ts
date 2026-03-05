import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig(({ mode }) => ({
  plugins: [tailwindcss(), sveltekit()],
  build: {
    minify: mode !== 'development',
    cssMinify: mode !== 'development',
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:4433',
      '/static': 'http://127.0.0.1:4433',
      '/search': {
        target: 'http://127.0.0.1:4433',
        ws: true,
        configure: (proxy) => {
          proxy.on('proxyReqWs', (proxyReq) => {
            proxyReq.setHeader('Origin', 'http://127.0.0.1:4433');
          });
        },
      },
    },
  },
}));

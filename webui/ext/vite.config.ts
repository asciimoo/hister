import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { resolve } from 'path';
import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from 'fs';

function extensionPlugin() {
  return {
    name: 'browser-extension',
    writeBundle() {
      const root = import.meta.dirname;
      const distDir = resolve(root, 'dist');
      const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf-8'));
      const base = JSON.parse(readFileSync(resolve(root, 'src/manifest.json'), 'utf-8'));

      // Chrome manifest
      const chrome = structuredClone(base);
      chrome.version = pkg.version;
      chrome.background.service_worker = 'background.js';
      delete chrome.chrome_settings_overrides;
      writeFileSync(resolve(distDir, 'manifest.json'), JSON.stringify(chrome));

      // Firefox manifest
      const ff = structuredClone(base);
      ff.version = pkg.version;
      ff.background.scripts = ['background.js'];
      ff.content_security_policy = { extension_pages: "script-src 'self'" };
      const geckoSettings = {
        id: '{f0bda7ce-0cda-42dc-9ea8-126b20fed280}',
        strict_min_version: '110.0',
        data_collection_permissions: {
          required: ['browsingActivity', 'websiteContent'],
        },
      };
      ff.browser_specific_settings = {
        gecko: geckoSettings,
        gecko_android: geckoSettings,
      };
      writeFileSync(resolve(distDir, 'manifest_ff.json'), JSON.stringify(ff));

      // Copy HTML shells and assets
      const copies: [string, string][] = [
        ['src/popup/popup.html', 'popup.html'],
        ['src/options/options.html', 'options.html'],
        ['assets/icon128.png', 'assets/icons/icon128.png'],
        ['assets/logo.png', 'assets/logo.png'],
      ];
      mkdirSync(resolve(distDir, 'assets/icons'), { recursive: true });
      for (const [src, dest] of copies) {
        copyFileSync(resolve(root, src), resolve(distDir, dest));
      }
    },
  };
}

export default defineConfig(({ mode }) => ({
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
    minify: mode === 'production',
    rollupOptions: {
      input: {
        background: resolve(import.meta.dirname, 'src/background/background.ts'),
        content: resolve(import.meta.dirname, 'src/content/content.ts'),
        popup: resolve(import.meta.dirname, 'src/popup/popup.ts'),
        options: resolve(import.meta.dirname, 'src/options/options.ts'),
      },
      output: {
        entryFileNames: '[name].js',
        chunkFileNames: 'shared.js',
        assetFileNames: (info) => {
          if (info.names?.[0]?.endsWith('.css') || info.originalFileNames?.[0]?.endsWith('.css')) {
            return 'style.css';
          }
          return '[name].[ext]';
        },
      },
    },
  },
  plugins: [tailwindcss(), svelte(), extensionPlugin()],
}));

// SPDX-License-Identifier: AGPL-3.0-or-later
import type { Page } from '@playwright/test';

export const original = 'https://example.com/article';
export const preview = {
  title: 'Saved article',
  content: '<p>Saved article content</p>',
  added: 1700000000,
  version_count: 1,
};

export function previewPath(url = original, version?: number) {
  const params = new URLSearchParams({ id: url, document_id: 'document-1', title: 'Article' });
  if (version) params.set('version', String(version));
  return `/preview?${params}`;
}

export async function mockAPI(
  page: Page,
  options: {
    canWrite?: boolean;
    historyResult?: boolean;
    pinned?: boolean;
    config?: Record<string, unknown>;
  } = {},
) {
  const result = {
    id: 'document-1',
    url: original,
    title: preview.title,
    domain: 'example.com',
    text: 'An indexed article',
    added: preview.added,
    label: 'reading',
    pinned: options.pinned,
  };
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          documents: options.historyResult ? [] : [result],
          history: options.historyResult ? [result] : [],
          total: 1,
        }),
      ),
    );
  });
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    const data: Record<string, unknown> = {
      '/api/config': {
        wsUrl: 'ws://127.0.0.1:4174/search',
        title: 'Hister',
        searchUrl: 'https://example.com/search?q=',
        hotkeys: { '/': 'focus_search_input', Enter: 'open_result' },
        authMode: 'none',
        authenticated: true,
        canWrite: options.canWrite ?? true,
        canConfigure: true,
        historyEnabled: true,
        colorScheme: 'automatic',
        search: {
          version: 0,
          fields: [],
          facets: [],
          sort: { field: '', label: '', description: '', options: [] },
          valueSets: {},
        },
        ...options.config,
      },
      '/api/stats': {
        doc_count: 2,
        recent_searches: [{ query: 'svelte' }, { query: 'golang' }],
      },
      '/api/rules': { allow: [], skip: [], priority: [], versioning: [], aliases: {} },
      '/api/settings': { revision: 'revision-1', fields: [], values: [], pending: null },
      '/api/preview': preview,
      '/api/versions': [],
      '/api/extractors': [
        { name: 'basic', description: 'Stored content' },
        { name: 'readability', description: 'Readable content' },
      ],
    };
    await route.fulfill({ json: data[path] ?? {} });
  });
}

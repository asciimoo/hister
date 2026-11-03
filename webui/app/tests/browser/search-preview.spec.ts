// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type WebSocketRoute } from '@playwright/test';

import { mockAPI, original, preview, previewPath } from './fixtures';

test.beforeEach(async ({ page }) => {
  await mockAPI(page);
});

test('PDF previews show readable pages without overflowing the panel', async ({
  page,
  isMobile,
}) => {
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'Research notes',
        template: 'pdf',
        content:
          '<div class="pdf-preview" role="region" aria-label="PDF preview">' +
          '<section class="pdf-page"><h2 class="pdf-page-number">Page 1</h2>' +
          '<p>Understanding search<br>  Notes from the reading group</p>' +
          '<p>Saved PDFs keep their line breaks and paragraph spacing, making longer documents easier to read.</p>' +
          '<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; literal text</p></section>' +
          '<section class="pdf-page"><h2 class="pdf-page-number">Page 2</h2>' +
          '<p>References<br>https://example.com/' +
          'long-path/'.repeat(40) +
          '</p></section></div>',
      },
    }),
  );
  for (const path of isMobile ? [previewPath()] : ['/?q=example', previewPath()]) {
    await page.goto(path);
    const pdf = page.getByRole('region', { name: 'PDF preview' });
    await expect(pdf.getByRole('heading', { name: 'Page 1' })).toBeVisible();
    await expect(pdf.getByRole('heading', { name: 'Page 2' })).toBeVisible();
    await expect(pdf.locator('p').first()).toHaveCSS('white-space', 'pre-wrap');
    await expect(pdf.locator('p').first()).toHaveJSProperty(
      'innerText',
      'Understanding search\n  Notes from the reading group',
    );
    await expect(pdf.locator('script')).toHaveCount(0);
    await expect(pdf).toContainText('<script>alert(1)</script> & literal text');
    const pages = pdf.locator('.pdf-page');
    const first = (await pages.nth(0).boundingBox())!;
    const second = (await pages.nth(1).boundingBox())!;
    expect(second.y - first.y - first.height).toBeGreaterThanOrEqual(16);
    for (const width of isMobile ? [375, 320] : [1280]) {
      await page.setViewportSize({ width, height: 812 });
      const viewport = page.locator('.preview-panel [data-slot="scroll-area-viewport"]');
      expect(await viewport.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(
        1,
      );
    }
    await page.evaluate(() => document.documentElement.classList.remove('dark'));
    const light = await pages.first().evaluate((el) => getComputedStyle(el).backgroundColor);
    await page.screenshot({
      path: test.info().outputPath(`pdf-${path.startsWith('/?') ? 'panel' : 'full'}-light.png`),
    });
    await page.evaluate(() => document.documentElement.classList.add('dark'));
    await expect(pages.first()).not.toHaveCSS('background-color', light);
    await page.screenshot({
      path: test.info().outputPath(`pdf-${path.startsWith('/?') ? 'panel' : 'full'}-dark.png`),
    });
  }
});

test('code previews highlight known syntax and preserve source text safely', async ({ page }) => {
  const source =
    '\n// Saved source\r\nconst html = "<img src=x onerror=alert(1)> &";\n\tconsole.log(html);\r// Old style line ending\n';
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'main.js',
        template: 'code',
        content: JSON.stringify({ text: source, syntax: 'javascript' }),
      },
    }),
  );
  await page.goto(previewPath('file:///missing/main.js'));
  const code = page.getByRole('region', { name: 'Code preview' });
  await expect(code.getByRole('combobox', { name: 'Code language' })).toHaveValue('auto');
  await expect(code.locator('pre code')).toHaveJSProperty('textContent', source);
  await expect(code.locator('script, img')).toHaveCount(0);
  await expect(async () => {
    const colors = await code
      .locator('pre code span')
      .evaluateAll((tokens) => [...new Set(tokens.map((token) => getComputedStyle(token).color))]);
    expect(colors.length).toBeGreaterThan(1);
  }).toPass();
  const keyword = code.locator('pre code span').filter({ hasText: /^const$/ });
  await page.evaluate(() => document.documentElement.classList.remove('dark'));
  const lightColor = await keyword.evaluate((element) => getComputedStyle(element).color);
  await page.evaluate(() => document.documentElement.classList.add('dark'));
  await expect(keyword).not.toHaveCSS('color', lightColor);
  await expect(code.locator('pre code')).toHaveJSProperty('textContent', source);
  await page.screenshot({ path: test.info().outputPath('code-preview.png') });
});

test('code preview controls stay compact and accessible while scrolling', async ({
  page,
  isMobile,
}) => {
  const source = 'const answer = 42;\n'.repeat(150);
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'Long source',
        template: 'code',
        content: JSON.stringify({ text: source, syntax: 'javascript' }),
      },
    }),
  );
  const fullscreen = previewPath('file:///notes/source.js');
  for (const path of isMobile ? [fullscreen] : ['/?q=example', fullscreen]) {
    await page.goto(path);
    const region = page.getByRole('region', { name: 'Code preview' });
    const language = region.getByRole('combobox', { name: 'Code language' });
    const highlight = region.getByRole('checkbox', { name: 'Highlight syntax' });
    const wrap = region.getByRole('checkbox', { name: 'Wrap lines' });
    const numbers = region.getByRole('checkbox', { name: 'Line numbers' });
    const copy = region.getByRole('button', { name: 'Copy code' });
    const viewport = page.locator('.preview-panel [data-slot="scroll-area-viewport"]');
    await expect(language).toBeVisible();
    for (const width of isMobile ? [375, 320] : [1280]) {
      await page.setViewportSize({ width, height: 800 });
      await viewport.evaluate((element) => {
        element.scrollTop = 400;
      });
      await expect(viewport).toHaveJSProperty('scrollTop', 400);
      const bounds = (await viewport.boundingBox())!;
      for (const control of [language, highlight, wrap, numbers, copy]) {
        await expect(control).toBeInViewport({ ratio: 1 });
        const box = (await control.boundingBox())!;
        expect(box.y).toBeGreaterThanOrEqual(bounds.y);
        expect(box.x).toBeGreaterThanOrEqual(bounds.x);
        expect(box.x + box.width).toBeLessThanOrEqual(bounds.x + bounds.width);
      }
      if (!isMobile && path === fullscreen) {
        const selector = (await language.boundingBox())!;
        for (const control of [highlight, wrap, numbers, copy]) {
          const box = (await control.boundingBox())!;
          expect(Math.abs(box.y + box.height / 2 - selector.y - selector.height / 2)).toBeLessThan(
            2,
          );
        }
      }
      await wrap.uncheck();
      await numbers.check();
      await language.selectOption('plaintext');
      await expect(region.locator('pre')).toHaveCSS('white-space', 'pre');
      await expect(region.locator('pre code')).toHaveJSProperty('textContent', source);
      expect(await viewport.evaluate((element) => element.scrollTop)).toBeGreaterThan(300);
      await page.screenshot({
        path: test
          .info()
          .outputPath(`code-controls-${path === fullscreen ? 'fullscreen' : 'panel'}-${width}.png`),
      });
    }
  }
});

test('code previews copy original source independently of display options', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  let source = '\n\tconst html = "<tag> & π";  \n\n' + '// long line '.repeat(30) + '\n';
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'Source',
        template: 'code',
        content: JSON.stringify({ text: source, syntax: 'javascript' }),
      },
    }),
  );
  await page.goto(previewPath('file:///notes/source.js'));
  const region = page.getByRole('region', { name: 'Code preview' });
  const copy = region.getByRole('button', { name: 'Copy code' });
  await expect(region.locator('.code-token').first()).toBeVisible();
  await region.getByRole('checkbox', { name: 'Line numbers' }).check();
  for (const wrap of [true, false]) {
    await region.getByRole('checkbox', { name: 'Wrap lines' }).setChecked(wrap);
    await page.evaluate(() => navigator.clipboard.writeText('previous clipboard content'));
    await copy.click();
    await expect(copy).toHaveText('Copied');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(source);
    await region.getByRole('combobox', { name: 'Code language' }).selectOption('plaintext');
  }
  source = '\n\t' + 'x'.repeat(150 * 1024) + '\n';
  await page.goto(previewPath('file:///notes/large.txt'));
  await expect(region.getByRole('status')).toContainText('Large file.');
  await expect(copy).toHaveText('Copy');
  await copy.click();
  await expect(copy).toHaveText('Copied');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(source);
});

test('code previews report clipboard failures and allow retry', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const source = 'const answer = 42;\n';
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({ json: { template: 'code', content: JSON.stringify({ text: source }) } }),
  );
  await page.goto(previewPath('file:///notes/source.js'));
  await page.evaluate(() => {
    const writeText = navigator.clipboard.writeText.bind(navigator.clipboard);
    navigator.clipboard.writeText = async () => {
      navigator.clipboard.writeText = writeText;
      throw new DOMException('Clipboard permission denied', 'NotAllowedError');
    };
  });
  const region = page.getByRole('region', { name: 'Code preview' });
  const copy = region.getByRole('button', { name: 'Copy code' });
  await copy.click();
  await expect(region.getByRole('alert')).toContainText('Could not copy');
  await expect(copy).toHaveText('Copy');
  await expect(copy).toBeEnabled();
  await copy.click();
  await expect(copy).toHaveText('Copied');
  await expect(region.getByRole('alert')).toHaveCount(0);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(source);
  await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { value: undefined }));
  await copy.click();
  await expect(region.getByRole('alert')).toContainText('copy it manually');
  await expect(copy).toHaveText('Copy');
});

test('code previews remember language overrides per document and display preferences', async ({
  page,
}) => {
  const source = 'const answer = 42;\nconsole.log(answer);\n';
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'Unknown source',
        template: 'code',
        content: JSON.stringify({ text: source }),
      },
    }),
  );
  const first = previewPath('file:///notes/source.unknown');
  const second = first.replace('document-1', 'document-2');
  await page.goto(first);
  const region = page.getByRole('region', { name: 'Code preview' });
  const language = region.getByRole('combobox', { name: 'Code language' });
  const code = region.locator('pre code');
  await expect(code).toHaveJSProperty('textContent', source);
  await expect(code.locator('.code-token')).toHaveCount(0);
  await language.selectOption('javascript');
  await expect(code.locator('.code-token').first()).toBeVisible();
  await page.reload();
  await expect(language).toHaveValue('javascript');
  await expect(code.locator('.code-token').first()).toBeVisible();
  await region.getByRole('checkbox', { name: 'Wrap lines' }).uncheck();
  await region.getByRole('checkbox', { name: 'Line numbers' }).check();
  await expect(region.locator('pre')).toHaveCSS('white-space', 'pre');
  await expect(region.locator('[data-line-number="2"]')).toBeVisible();
  expect(
    await region
      .locator('[data-line-number="2"]')
      .evaluate((line) => getComputedStyle(line, '::before').content),
  ).toBe('"2"');
  const heights = await region.locator('.code-line').evaluateAll((lines) =>
    lines.map((line) => ({
      height: line.getBoundingClientRect().height,
      expected: parseFloat(getComputedStyle(line).lineHeight),
    })),
  );
  for (const { height, expected } of heights) expect(height).toBeCloseTo(expected, 0);
  await expect(code).toHaveJSProperty('textContent', source);
  await region.getByRole('checkbox', { name: 'Highlight syntax' }).uncheck();
  await expect(code.locator('.code-token')).toHaveCount(0);
  await page.reload();
  await expect(region.getByRole('checkbox', { name: 'Highlight syntax' })).not.toBeChecked();
  await expect(region.getByRole('checkbox', { name: 'Wrap lines' })).not.toBeChecked();
  await expect(region.getByRole('checkbox', { name: 'Line numbers' })).toBeChecked();
  await region.getByRole('checkbox', { name: 'Highlight syntax' }).check();
  await language.selectOption('plaintext');
  await expect(code.locator('.code-token')).toHaveCount(0);
  await page.reload();
  await expect(language).toHaveValue('plaintext');
  await page.goto(second);
  await expect(language).toHaveValue('auto');
  await expect(code.locator('.code-token')).toHaveCount(0);
  await page.goto(first);
  await language.selectOption('auto');
  await page.reload();
  await expect(language).toHaveValue('auto');
});

test('code previews keep large files complete without highlighting', async ({ page }) => {
  for (const source of ['x'.repeat(150 * 1024), 'line\n'.repeat(5001), 'x'.repeat(10001)]) {
    await page.route('**/api/preview?*', (route) =>
      route.fulfill({
        json: {
          title: 'Large file',
          template: 'code',
          content: JSON.stringify({ text: source, syntax: 'javascript' }),
        },
      }),
    );
    await page.goto(previewPath('file:///notes/large.js'));
    const region = page.getByRole('region', { name: 'Code preview' });
    await expect(region.getByRole('status')).toHaveText('Large file. Showing plain text.');
    await expect(region.locator('pre code')).toHaveJSProperty('textContent', source);
    await expect(region.locator('.code-token')).toHaveCount(0);
    await expect(region.getByRole('checkbox', { name: 'Line numbers' })).toBeDisabled();
  }
});

test('code previews reject malformed payloads and keep unknown syntax readable', async ({
  page,
}) => {
  for (const content of [
    '{"text":"<img src=x>"',
    '{"text":42}',
    '{"text":"<img src=x>","syntax":{}}',
  ]) {
    await page.route('**/api/preview?*', (route) =>
      route.fulfill({ json: { template: 'code', content } }),
    );
    await page.goto(previewPath());
    await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
    await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
    await expect(page.locator('.preview-content img')).toHaveCount(0);
  }
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        template: 'code',
        content: JSON.stringify({ text: '<img src=x>\n', syntax: '../../unknown' }),
      },
    }),
  );
  await page.goto(previewPath());
  const code = page.getByRole('region', { name: 'Code preview' });
  await expect(code.locator('pre code')).toHaveJSProperty('textContent', '<img src=x>\n');
  await expect(code.locator('img, .code-token')).toHaveCount(0);
});

test('code previews stay readable when the highlighter cannot load', async ({ page }) => {
  const source = 'const answer = 42;\n';
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        template: 'code',
        content: JSON.stringify({ text: source, syntax: 'javascript' }),
      },
    }),
  );
  await page.route(/shiki.*(?:core|engine|langs)/, (route) => route.abort());
  await page.goto(previewPath());
  const region = page.getByRole('region', { name: 'Code preview' });
  await expect(region.getByRole('status')).toContainText('Highlighting unavailable.');
  await expect(region.locator('pre code')).toHaveJSProperty('textContent', source);
  await region.getByRole('combobox', { name: 'Code language' }).selectOption('plaintext');
  await expect(region.getByRole('status')).toHaveCount(0);
});

test('code previews ignore a delayed language load after selecting plain text', async ({
  page,
}) => {
  const source = 'const answer = 42;\n';
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let started!: () => void;
  const loading = new Promise<void>((resolve) => {
    started = resolve;
  });
  await page.route(/shiki.*langs.*javascript/, async (route) => {
    started();
    await gate;
    await route.continue();
  });
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        template: 'code',
        content: JSON.stringify({ text: source, syntax: 'javascript' }),
      },
    }),
  );
  await page.goto(previewPath());
  await loading;
  const region = page.getByRole('region', { name: 'Code preview' });
  await region.getByRole('combobox', { name: 'Code language' }).selectOption('plaintext');
  release();
  await page.waitForLoadState('networkidle');
  await expect(region.locator('pre code')).toHaveJSProperty('textContent', source);
  await expect(region.locator('.code-token')).toHaveCount(0);
});

test('code previews keep language choices when entering fullscreen from the side panel', async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, 'The side panel is only available on desktop.');
  await page.route('**/api/preview?*', (route) =>
    route.fulfill({
      json: {
        title: 'Example',
        template: 'code',
        content: JSON.stringify({ text: 'let value = 1;\n', syntax: 'javascript' }),
      },
    }),
  );
  await page.goto('/?q=example');
  const region = page.getByRole('region', { name: 'Code preview' });
  await region.getByRole('combobox', { name: 'Code language' }).selectOption('plaintext');
  await page.getByRole('button', { name: 'Enter fullscreen', exact: true }).click();
  await expect(page).toHaveURL(/\/preview\?/);
  await expect(region.getByRole('combobox', { name: 'Code language' })).toHaveValue('plaintext');
  await expect(region.locator('.code-token')).toHaveCount(0);
  await page.getByRole('button', { name: 'Exit fullscreen', exact: true }).click();
  await expect(page).toHaveURL(/\/\?q=example$/);
  await expect(region.getByRole('combobox', { name: 'Code language' })).toHaveValue('plaintext');
});

test('local file result titles open fullscreen previews with a download action', async ({
  page,
  isMobile,
}) => {
  const file = {
    id: 'local-document-2',
    url: 'file:///home/me/notes.txt',
    title: 'Local notes',
  };
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          documents: [{ id: 'document-1', url: original, title: preview.title }, file],
          total: 2,
        }),
      ),
    );
  });
  await page.route('**/api/preview?*', (route) => {
    const isFile = new URL(route.request().url()).searchParams.get('url') === file.url;
    return route.fulfill({
      json: { ...preview, title: isFile ? file.title : preview.title },
    });
  });
  await page.route('**/api/file?*', (route) =>
    route.fulfill({
      headers: { 'Content-Disposition': 'attachment; filename="notes.txt"' },
      body: 'Saved notes',
    }),
  );
  const downloads: string[] = [];
  page.on('download', (download) => downloads.push(download.suggestedFilename()));

  await page.goto('/?q=notes');
  const rows = page.locator('[data-result]');
  await expect(rows).toHaveCount(2);
  await expect(
    rows.first().getByRole('link', { name: preview.title, exact: true }),
  ).toHaveAttribute('href', original);
  const downloadLink = page.locator('.preview-header').getByRole('link', { name: 'Download file' });
  if (!isMobile) {
    await rows.last().getByRole('link', { name: 'Preview', exact: true }).click();
    await expect(downloadLink).toHaveAttribute('href', '/api/file?id=local-document-2');
  }
  await rows.last().getByRole('link', { name: file.title, exact: true }).click();
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === '/preview' &&
      url.searchParams.get('id') === file.url &&
      url.searchParams.get('document_id') === file.id,
  );
  await expect(page.getByRole('heading', { name: file.title, exact: true })).toBeVisible();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
  await expect(rows).toHaveCount(0);
  expect(downloads).toEqual([]);

  await expect(downloadLink).toHaveAttribute('href', '/api/file?id=local-document-2');
  await expect(downloadLink).toBeInViewport({ ratio: 1 });
  await page.screenshot({ path: test.info().outputPath('file-preview-header.png') });
  const downloadEvent = page.waitForEvent('download');
  await downloadLink.click();
  const download = await downloadEvent;
  expect(download.suggestedFilename()).toBe('notes.txt');
  const stream = await download.createReadStream();
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(chunk);
  expect(Buffer.concat(chunks).toString()).toBe('Saved notes');
  await expect(page.getByRole('heading', { name: file.title, exact: true })).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(/\/\?q=notes$/);
  await expect(rows).toHaveCount(2);
});

test('preview headers hide downloads for web pages, remote snapshots, and missing document IDs', async ({
  page,
}) => {
  const filePreview = new URLSearchParams({ id: 'file:///home/me/notes.txt' });
  for (const path of [
    previewPath(),
    previewPath('remote-file://laptop/notes.txt'),
    `/preview?${filePreview}`,
  ]) {
    await page.goto(path);
    await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
    await expect(
      page.locator('.preview-header').getByRole('link', { name: 'Download file' }),
    ).toHaveCount(0);
  }
});

test.describe('shared preview navigation', () => {
  const documents = [
    { id: 'document-1', url: original, title: preview.title, added: preview.added },
    { id: 'document-2', url: original, title: 'Another saved copy', added: preview.added },
  ];
  const pages = [
    { path: '/?q=article', action: 'Preview' },
    { path: '/history?filter=article', action: 'Preview entry' },
  ];

  test.beforeEach(async ({ page }) => {
    await mockAPI(page, { config: { hotkeys: { v: 'view_result_popup' } } });
    await page.routeWebSocket('**/search', (socket) => {
      socket.onMessage((message) => {
        const results = JSON.parse(String(message)).text === 'missing' ? [] : documents;
        socket.send(JSON.stringify({ documents: results, total: results.length }));
      });
    });
    await page.route('**/api/history?*', (route) => route.fulfill({ json: { documents } }));
    await page.route('**/api/history/timeline?*', (route) =>
      route.fulfill({ json: { days: [], older: { key: 'older', to: 0, count: 0 }, months: [] } }),
    );
    await page.route('**/api/versions?*', (route) =>
      route.fulfill({
        json: [{ id: 7, created_at: '2023-11-14T22:13:20Z', text_diff: '+Old content' }],
      }),
    );
    await page.route('**/api/preview?*', (route) => {
      const params = new URL(route.request().url()).searchParams;
      const document = documents.find((item) => item.id === params.get('document_id'))!;
      return route.fulfill({
        json: {
          ...preview,
          title: document.title,
          ...(params.get('version') === '7'
            ? {
                content: '<p>Archived content</p>',
                version_id: 7,
                version_created_at: '2023-11-14T22:13:20Z',
              }
            : {}),
        },
      });
    });
  });

  for (const source of pages) {
    test(`${source.path} preserves document and version through fullscreen and browser history`, async ({
      page,
      isMobile,
    }) => {
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await page.goto(source.path);
      const rows = page.locator('[data-result]');
      await expect(rows).toHaveCount(2);
      if (isMobile)
        await rows
          .first()
          .getByRole('link', { name: source.action, exact: true })
          .or(rows.first().getByRole('button', { name: source.action, exact: true }))
          .click();
      await expect(page.getByRole('heading', { name: preview.title, exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Show document details', exact: true }).click();
      await page.getByRole('button', { name: '1 previous version', exact: true }).click();
      await page.getByRole('button', { name: 'show this version', exact: true }).click();
      await expect(page.getByText('Archived content', { exact: true })).toBeVisible();
      if (!isMobile) {
        await expect(page).toHaveURL(new RegExp(`${source.path.replace(/[?]/g, '\\?')}$`));
        await page.getByRole('button', { name: 'Enter fullscreen', exact: true }).click();
      }
      const archivedURL = page.url();
      expect(new URL(archivedURL).pathname).toBe('/preview');
      expect(new URL(archivedURL).searchParams.get('document_id')).toBe('document-1');
      expect(new URL(archivedURL).searchParams.get('version')).toBe('7');
      await expect(page.getByText('Archived content', { exact: true })).toBeVisible();

      await page.goBack();
      await expect(page).toHaveURL(new URL(source.path, archivedURL).href);
      await expect(rows).toHaveCount(2);
      await page.goForward();
      await expect(page).toHaveURL(archivedURL);
      await expect(page.getByText('Archived content', { exact: true })).toBeVisible();
      if (!isMobile) {
        await page.keyboard.press('v');
        await expect(page).toHaveURL(new URL(source.path, archivedURL).href);
        await expect(page.getByText('Archived content', { exact: true })).toBeVisible();
        await page.getByRole('button', { name: 'Enter fullscreen', exact: true }).click();
        await page.keyboard.press('ArrowDown');
        await expect(
          page.getByRole('heading', { name: documents[1].title, exact: true }),
        ).toBeVisible();
        await expect(page).toHaveURL(/document_id=document-2/);
        expect(new URL(page.url()).searchParams.has('version')).toBe(false);
      }
      await page.getByRole('button', { name: 'Close preview', exact: true }).click();
      await expect(page).toHaveURL(new URL(source.path, archivedURL).href);
      await expect(rows).toHaveCount(2);
      await expect(page.locator('.preview-panel')).toHaveCount(0);
      await page.reload();
      await expect(rows).toHaveCount(2);
      await expect(page.locator('.preview-panel')).toHaveCount(0);
      await page.keyboard.press('v');
      await expect(page.getByRole('heading', { name: preview.title, exact: true })).toBeVisible();
      if (isMobile) {
        await page.keyboard.press('v');
        await expect(page).toHaveURL(new URL(source.path, archivedURL).href);
        await expect(page.locator('.preview-panel')).toHaveCount(0);
      } else {
        await page.keyboard.press('v');
        await expect(
          page.getByRole('button', { name: 'Exit fullscreen', exact: true }),
        ).toBeVisible();
      }
      if (source.path.startsWith('/?')) {
        if (isMobile) await page.keyboard.press('v');
        await page.getByRole('searchbox').fill('missing');
        await expect(page).toHaveURL(/\/\?q=missing$/);
        await expect(rows).toHaveCount(0);
        await expect(page.locator('.preview-panel')).toHaveCount(0);
      }
      expect(errors).toEqual([]);
    });
  }

  test('panel width is shared while open preferences stay separate', async ({ page, isMobile }) => {
    test.skip(isMobile, 'Split panels are only available on desktop.');
    await page.goto(pages[0].path);
    const separator = page.getByRole('separator', { name: 'Resize preview panel' });
    const box = (await separator.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + 100, box.y + box.height / 2);
    await page.mouse.up();
    const width = await page.evaluate(() => localStorage.getItem('hister-panel-width'));
    expect(Number(width)).toBeLessThan(50);
    await page.getByRole('button', { name: 'Close preview', exact: true }).click();
    await page.goto(pages[1].path);
    await expect(page.locator('.preview-panel')).toBeVisible();
    const panelStyle = await page.locator('.preview-panel').locator('..').getAttribute('style');
    expect(Number(panelStyle?.match(/[\d.]+(?=%)/)?.[0])).toBeCloseTo(Number(width), 3);
    await page.setViewportSize({ width: 1000, height: 800 });
    await expect(page.locator('.preview-panel')).toHaveCount(0);
    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.locator('.preview-panel')).toBeVisible();
    await page.goto(pages[0].path);
    await expect(page.locator('[data-result]')).toHaveCount(2);
    await expect(page.locator('.preview-panel')).toHaveCount(0);
    await page
      .locator('[data-result]')
      .first()
      .getByRole('link', { name: 'Preview', exact: true })
      .click();
    await expect(page.locator('.preview-panel')).toBeVisible();
    await page.reload();
    await expect(page.locator('.preview-panel')).toBeVisible();
  });
});

test('the compact toolbar keeps filters and sorting visible and secondary actions accessible', async ({
  page,
  isMobile,
}) => {
  await mockAPI(page, {
    canWrite: false,
    config: {
      searchUrl: 'https://example.com/search?q={query}',
      search: {
        version: 1,
        fields: [{ name: 'domain', label: 'Domain', kind: 'keyword', description: '' }],
        facets: [
          {
            name: 'domains',
            label: 'Domain',
            queryField: 'domain',
            kind: 'terms',
            defaultSize: 5,
          },
        ],
        sort: {
          field: 'sort',
          label: 'Sort',
          description: '',
          options: [
            { value: 'relevance', label: 'Relevance', default: true, visible: true },
            { value: 'date', label: 'Newest', visible: true },
          ],
        },
        valueSets: {},
      },
    },
  });
  await page.route('**/api/facets?*', (route) =>
    route.fulfill({
      json: { terms: { domains: { terms: [{ term: 'example.com', count: 1 }] } } },
    }),
  );
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage((message) => {
      const request = JSON.parse(String(message));
      const documents =
        request.text === 'missing'
          ? []
          : [{ id: 'document-1', url: original, title: preview.title, text: 'An indexed article' }];
      socket.send(
        JSON.stringify({ documents, total: documents.length, search_duration: '12.34ms' }),
      );
    });
  });
  await page.goto('/?q=article%20domain%3Aexample.com');
  const toolbar = page.locator('.results-toolbar');
  const actions = toolbar.getByRole('button', { name: 'Actions', exact: true });
  const webSearch = toolbar.getByRole('link', { name: 'Search the web', exact: true });
  const filters = page.getByRole('group', { name: 'Active filters' });
  const input = page.getByRole('searchbox');
  await expect(page.locator('[data-result]')).toHaveCount(1);
  await page.evaluate(() => document.fonts.ready);
  for (const width of isMobile ? [375, 320] : [1280, 1024]) {
    await page.setViewportSize({ width, height: 812 });
    await expect(webSearch).toBeVisible();
    await expect(async () => {
      const bounds = (await toolbar.boundingBox())!;
      expect(bounds.height).toBeLessThanOrEqual(isMobile ? 100 : 65);
      for (const control of await toolbar.locator('button, a').all()) {
        const box = (await control.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(bounds.x);
        expect(box.x + box.width).toBeLessThanOrEqual(bounds.x + bounds.width);
      }
    }).toPass({ timeout: 5000 });
    await expect(filters).toBeVisible();
    await expect(toolbar.getByRole('button', { name: /^Filters/ })).toBeVisible();
    await expect(toolbar.getByRole('button', { name: 'Sort: Relevance' })).toHaveText('Sort');
    await expect(toolbar).not.toContainText('12.34ms');
    await expect(page.getByRole('button', { name: 'Server connected', exact: true })).toHaveCount(
      0,
    );
    await page.screenshot({ path: test.info().outputPath(`compact-toolbar-${width}.png`) });
  }
  await toolbar.getByRole('button', { name: /^Filters/ }).click();
  await expect(
    page.getByRole('menu').getByRole('button', { name: 'example.com (1)' }),
  ).toHaveAttribute('aria-pressed', 'true');
  await page.keyboard.press('Escape');
  await toolbar.getByRole('button', { name: 'Sort: Relevance' }).click();
  const newest = page.getByRole('menu').getByRole('button', { name: 'Newest', exact: true });
  await newest.focus();
  await newest.press('Enter');
  await expect(input).toHaveValue('article domain:example.com sort:date');
  await expect(toolbar.getByRole('button', { name: 'Sort: Newest' })).toContainText('Newest');
  await filters.getByRole('button', { name: 'Remove Domain: example.com filter' }).click();
  await expect(input).toHaveValue('article sort:date');
  await expect(filters).toHaveCount(0);
  await expect(webSearch).toHaveAttribute(
    'href',
    'https://example.com/search?q=article%20sort%3Adate',
  );
  await actions.click();
  const menu = page.getByRole('menu');
  await expect(menu.getByRole('link', { name: 'Search the web' })).toHaveCount(0);
  await expect(menu.getByText('Search time: 12.34ms', { exact: true })).toBeVisible();
  await expect(menu.getByRole('checkbox', { name: 'Always expand snippets' })).toBeVisible();
  await expect(menu.getByRole('button', { name: 'JSON', exact: true })).toBeVisible();
  await expect(menu.getByRole('button', { name: 'Delete all matching results' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  if (!isMobile) {
    await page.setViewportSize({ width: 1280, height: 812 });
    await page.getByRole('button', { name: 'Close preview', exact: true }).click();
    await expect(toolbar.getByRole('button', { name: 'Preview', exact: true })).toHaveCount(0);
    await actions.click();
    await menu.getByRole('button', { name: 'Show preview', exact: true }).click();
    await expect(menu).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Close preview', exact: true })).toBeVisible();
  }
  await input.fill('missing');
  await expect(page.getByText('No results found for', { exact: false })).toBeVisible();
  await expect(webSearch).toBeVisible();
  await expect(webSearch).toHaveAttribute('href', 'https://example.com/search?q=missing');
  await page.route('https://example.com/search?*', (route) =>
    route.fulfill({ body: 'External search', contentType: 'text/plain' }),
  );
  await webSearch.focus();
  await webSearch.press('Enter');
  await expect(page).toHaveURL('https://example.com/search?q=missing');
});

test('connection failures remain visible and retry restores search without routine status clutter', async ({
  page,
}) => {
  let socket: WebSocketRoute | undefined;
  let disconnected = false;
  let respond: (() => void) | undefined;
  await page.routeWebSocket('**/search', (connection) => {
    if (disconnected) {
      connection.close();
      return;
    }
    socket = connection;
    respond = undefined;
    connection.onMessage(() => {
      respond = () =>
        connection.send(
          JSON.stringify({
            documents: [{ id: 'document-1', url: original, title: preview.title }],
            total: 1,
          }),
        );
    });
  });
  await page.goto('/?q=article');
  await expect(page.locator('.results-toolbar')).toContainText('Searching…');
  await expect(page.locator('.results-list')).toHaveAttribute('aria-busy', 'true');
  await expect.poll(() => !!respond).toBe(true);
  respond!();
  await expect(page.locator('[data-result]')).toHaveCount(1);
  disconnected = true;
  socket!.close();
  const notice = page.getByRole('alert');
  await expect(notice).toContainText('Cannot connect to Hister');
  await expect(notice).toContainText('Showing the last loaded results');
  await expect(page.locator('[data-result]')).toHaveCount(1);
  disconnected = false;
  await notice.getByRole('button', { name: 'Retry now' }).click();
  await expect(notice).toHaveCount(0);
  await expect.poll(() => !!respond).toBe(true);
  respond!();
  await expect(page.locator('.results-list')).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByRole('button', { name: 'Server connected', exact: true })).toHaveCount(0);
});

test('narrow results truncate the URL while keeping Preview beside it', async ({
  page,
  isMobile,
}) => {
  const urls = [
    `${original}/${'saved-reading/'.repeat(12)}?lang=en#overview`,
    `${original}/${'reference-notes/'.repeat(12)}`,
  ];
  const label = 'Reference material to read for the next research project';
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          history: [
            {
              id: 'document-1',
              url: urls[0],
              title: 'Pinned article',
              domain: 'example.com',
              text: 'A saved article with a long label.',
              label,
              pinned: true,
            },
          ],
          documents: [
            {
              id: 'document-2',
              url: urls[1],
              title: 'Another article',
              domain: 'example.com',
              text: 'A saved article without a label.',
            },
          ],
          total: 2,
        }),
      ),
    );
  });
  await page.goto('/?q=article');
  const rows = page.locator('[data-result]');
  await expect(rows).toHaveCount(2);
  await page.evaluate(() => document.fonts.ready);

  for (const width of isMobile ? [375, 320] : [1280, 1024]) {
    await page.setViewportSize({ width, height: 812 });
    for (let index = 0; index < urls.length; index++) {
      const row = rows.nth(index);
      const url = row.locator('.result-url-text');
      const previewLink = row.getByRole('link', { name: 'Preview', exact: true });
      await expect(url).toHaveAttribute('title', urls[index]);
      await expect(previewLink).toBeVisible();
      await expect(async () => {
        const urlBox = (await url.boundingBox())!;
        const previewBox = (await previewLink.boundingBox())!;
        const rowBox = (await row.boundingBox())!;
        expect(
          Math.abs(urlBox.y + urlBox.height / 2 - (previewBox.y + previewBox.height / 2)),
        ).toBeLessThanOrEqual(2);
        expect(previewBox.x).toBeGreaterThanOrEqual(urlBox.x + urlBox.width);
        expect(previewBox.x + previewBox.width).toBeLessThanOrEqual(rowBox.x + rowBox.width);
        expect(urlBox.width).toBeGreaterThan(20);
        expect(await url.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(
          true,
        );
        expect(await url.evaluate((element) => getComputedStyle(element).textOverflow)).toBe(
          'ellipsis',
        );
      }).toPass({ timeout: 5000 });
    }
    await expect(rows.first().getByText(label, { exact: true })).toBeVisible();
    await expect(
      rows.first().getByRole('img', { name: 'Pinned result', exact: true }),
    ).toBeVisible();
    await page.screenshot({ path: test.info().outputPath(`url-preview-${width}.png`) });
  }

  await rows.nth(1).getByRole('link', { name: 'Preview', exact: true }).click();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
});

test('result metadata is available in preview details instead of the result row', async ({
  page,
  isMobile,
}) => {
  await mockAPI(page, { config: { semanticEnabled: true } });
  await page.addInitScript(() => localStorage.setItem('hister-semantic-on', 'true'));
  const updated = 1700086400;
  const articleURL = `${original}?lang=en#overview`;
  const fileURL = 'file:///home/me/notes.txt';
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          documents: [
            {
              id: 'document-1',
              url: articleURL,
              title: preview.title,
              domain: 'example.com',
              text: 'An indexed article',
              label: 'reading',
              added: preview.added,
              updated,
              add_count: 7,
              score: 0.73,
            },
            {
              id: 'document-2',
              url: fileURL,
              title: 'Local notes',
              domain: '',
              text: 'Saved notes',
              added: preview.added,
              score: 0,
            },
          ],
          total: 2,
        }),
      ),
    );
  });
  await page.route('**/api/preview?**', async (route) => {
    const isFile = new URL(route.request().url()).searchParams.get('url') === fileURL;
    await route.fulfill({
      json: {
        ...preview,
        title: isFile ? 'Local notes' : preview.title,
        updated,
        details: {
          type: isFile ? 'local' : 'web',
          language: 'en',
          label: isFile ? '' : 'reading',
          visits: isFile ? 1 : 7,
          user_id: 1,
          metadata: {},
        },
      },
    });
  });
  await page.goto('/?q=article');
  const rows = page.locator('[data-result]');
  await expect(rows).toHaveCount(2);
  await expect(rows.first().locator('.result-url-text')).toHaveText(
    'example.com/article?lang=en#overview',
  );
  await expect(rows.first().locator('.result-url-text')).toHaveAttribute('title', articleURL);
  await expect(rows.first().getByText('reading', { exact: true })).toBeVisible();
  await expect(rows.first().getByTitle('index count')).toHaveCount(0);
  await expect(rows.first().locator('time')).toHaveCount(0);
  await expect(rows.first().getByText('0.73', { exact: true })).toHaveCount(0);
  await expect(rows.first().getByRole('button', { name: 'Copy URL' })).toHaveCount(0);
  await expect(rows.nth(1).locator('.result-url-text')).toHaveText('/home/me/notes.txt');
  await page.screenshot({ path: test.info().outputPath('result-metadata.png') });

  await rows.first().getByRole('link', { name: 'Preview', exact: true }).click();
  await expect(page.getByRole('heading', { name: preview.title, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '1 previous version', exact: true })).toHaveCount(
    0,
  );
  await expect(page.locator('time')).toHaveCount(0);
  await page.getByRole('button', { name: 'Show document details', exact: true }).click();
  const details = page.locator('#preview-document-details');
  await expect(details).toBeVisible();
  await expect(
    details.getByRole('button', { name: '1 previous version', exact: true }),
  ).toBeVisible();
  await expect(details.getByText('0.73', { exact: true })).toBeVisible();
  await expect(
    details.locator('div').filter({ has: page.locator('dt', { hasText: /^visits$/ }) }),
  ).toContainText('7');
  for (const timestamp of [preview.added, updated]) {
    await expect(
      details.locator(`time[datetime="${new Date(timestamp * 1000).toISOString()}"]`),
    ).toBeVisible();
  }

  if (isMobile) await page.getByRole('button', { name: 'Close preview', exact: true }).click();
  await rows.nth(1).getByRole('link', { name: 'Preview', exact: true }).click();
  await expect(details.getByText('0.00', { exact: true })).toBeVisible();
  await expect(details.getByText('0.73', { exact: true })).toHaveCount(0);
  await expect(
    details.locator('div').filter({ has: page.locator('dt', { hasText: /^visits$/ }) }),
  ).toContainText('1');
});

for (const interaction of ['hover', 'keyboard'] as const) {
  test(`preview header icons show tooltips on ${interaction}`, async ({ page, isMobile }) => {
    test.skip(isMobile && interaction === 'hover', 'Touch screens do not have hover.');
    await page.goto(previewPath('file:///notes/article.txt'));
    const header = page.locator('.preview-header');
    for (const [role, name] of [
      ['link', 'Download file'],
      ['button', 'Change extractor'],
      ['link', 'Open direct preview'],
      ['button', 'Show document details'],
      ['button', 'Close preview'],
    ] as const) {
      const control = header.getByRole(role, { name, exact: true });
      if (interaction === 'hover') {
        await control.hover();
      } else {
        await control.focus();
        await page.keyboard.press('Shift+Tab');
        await page.keyboard.press('Tab');
        await expect(control).toBeFocused();
      }
      await expect(page.getByRole('tooltip', { name, exact: true })).toBeVisible();
      await expect(page.getByRole('tooltip', { name, exact: true })).toBeInViewport({ ratio: 1 });
      await expect(control).toHaveAccessibleDescription(name);
      await page.keyboard.press('Escape');
      await expect(page.getByRole('tooltip')).toHaveCount(0);
      await page.mouse.move(0, 0);
    }
    await header.getByRole('button', { name: 'Show document details', exact: true }).click();
    const hideDetails = header.getByRole('button', { name: 'Hide document details', exact: true });
    await hideDetails.press('Tab');
    await page.keyboard.press('Shift+Tab');
    await expect(
      page.getByRole('tooltip', { name: 'Hide document details', exact: true }),
    ).toBeVisible();
    await hideDetails.press('Enter');
    await expect(page.locator('#preview-document-details')).toHaveCount(0);
    await header.getByRole('button', { name: 'Change extractor', exact: true }).click();
    await expect(
      page.getByRole('menuitemradio', { name: 'readability', exact: true }),
    ).toBeVisible();
    await expect(page.getByRole('tooltip')).toHaveCount(0);
  });
}

test('preview header fullscreen tooltips follow the current view', async ({ page, isMobile }) => {
  test.skip(isMobile, 'Fullscreen toggle is available on desktop.');
  await page.goto('/?q=article');
  for (const name of ['Enter fullscreen', 'Exit fullscreen']) {
    const toggle = page.getByRole('button', { name, exact: true });
    await toggle.hover();
    await expect(page.getByRole('tooltip', { name, exact: true })).toBeVisible();
    await toggle.click();
  }
  await expect(page.getByRole('button', { name: 'Enter fullscreen', exact: true })).toBeVisible();
});

test('preview header tooltips remain available while loading and after failure', async ({
  page,
  isMobile,
}) => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  await page.route('**/api/preview?**', async (route) => {
    await pending;
    await route.fulfill({ status: 500 });
  });
  await page.goto(isMobile ? previewPath() : '/?q=article');
  await expect(page.getByRole('status')).toHaveText('Loading…');
  for (const state of ['loading', 'error']) {
    if (state === 'error') {
      finish();
      await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
    }
    const names = isMobile ? ['Close preview'] : ['Enter fullscreen', 'Close preview'];
    for (const name of names) {
      const control = page.getByRole('button', { name, exact: true });
      await control.focus();
      await page.keyboard.press('Shift+Tab');
      await page.keyboard.press('Tab');
      await expect(page.getByRole('tooltip', { name, exact: true })).toBeVisible();
      await page.keyboard.press('Escape');
    }
  }
});

test('preview header keeps only the title while details retain metadata and content controls', async ({
  page,
  isMobile,
}) => {
  const meta = {
    author: 'Article author',
    published: '2026-04-09',
    type: 'article',
    description: 'A detailed description of the saved article.',
    videos: [{ type: 'video', url: 'https://example.com/saved-video.mp4' }],
  };
  await page.route('**/api/preview?**', (route) => route.fulfill({ json: { ...preview, meta } }));
  await page.route('**/api/versions?**', (route) =>
    route.fulfill({
      json: [{ id: 7, created_at: '2023-11-14T22:13:20Z', text_diff: '+Older content' }],
    }),
  );
  await page.route('https://example.com/saved-video.mp4', (route) =>
    route.fulfill({ body: '', contentType: 'video/mp4' }),
  );
  await page.goto('/?q=article');
  if (isMobile) {
    await page.locator('[data-result]').getByRole('link', { name: 'Preview', exact: true }).click();
  }
  const header = page.locator('.preview-header');
  const details = page.locator('#preview-document-details');
  const showDetails = header.getByRole('button', { name: 'Show document details', exact: true });
  const hideDetails = header.getByRole('button', { name: 'Hide document details', exact: true });
  await expect(header.getByRole('heading', { name: preview.title, exact: true })).toBeVisible();
  await expect(header).toHaveText(preview.title);
  await expect(showDetails).toHaveAttribute('aria-expanded', 'false');
  await expect(header.getByRole('link', { name: preview.title, exact: true })).toHaveAttribute(
    'href',
    original,
  );
  await expect(header.getByRole('link', { name: 'Open direct preview' })).toBeVisible();
  await expect(header.getByRole('button', { name: 'Change extractor' })).toBeVisible();
  await expect(header.getByRole('button', { name: 'Close preview' })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath('preview-header.png') });

  await showDetails.focus();
  await showDetails.press('Enter');
  await expect(hideDetails).toHaveAttribute('aria-expanded', 'true');
  await expect(details.getByText(meta.author, { exact: true })).toBeVisible();
  await expect(details).toContainText('2026');
  await expect(details.getByText(meta.type, { exact: true })).toBeVisible();
  await expect(details.getByText(meta.description, { exact: true })).toBeVisible();
  await details.getByRole('button', { name: 'Show embedded video', exact: true }).click();
  await expect(page.locator('.preview-content video')).toBeVisible();
  await details.getByRole('button', { name: 'Hide embedded video', exact: true }).click();
  await expect(page.locator('.preview-content video')).toHaveCount(0);
  await details.getByRole('button', { name: '1 previous version', exact: true }).click();
  await expect(page.getByRole('button', { name: 'show this version' })).toBeVisible();
  await hideDetails.click();
  await expect(header).toHaveText(preview.title);
  await expect(page.getByRole('button', { name: 'show this version' })).toBeVisible();
  await showDetails.click();
  await details.getByRole('button', { name: '1 previous version', exact: true }).click();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath('preview-details.png') });

  await page.reload();
  await expect(details).toBeVisible();
  await hideDetails.click();
  await page.reload();
  await expect(header).toHaveText(preview.title);
  await expect(showDetails).toHaveAttribute('aria-expanded', 'false');
});

test('preview details remain available with only header metadata or version history', async ({
  page,
}) => {
  for (const fields of [
    { meta: { author: 'Article author' } },
    { meta: { published: '2026-04-09' } },
    { meta: { type: 'article' } },
    { meta: { description: 'Article description' } },
    { meta: { videos: [{ type: 'video', url: 'https://example.com/saved-video.mp4' }] } },
    { version_count: 2 },
  ]) {
    await page.route('**/api/preview?**', (route) =>
      route.fulfill({ json: { title: preview.title, content: preview.content, ...fields } }),
    );
    await page.goto(previewPath());
    const header = page.locator('.preview-header');
    const showDetails = header.getByRole('button', { name: 'Show document details', exact: true });
    await expect(showDetails).toBeVisible();
    await expect(header).toHaveText(preview.title);
    await showDetails.click();
    await expect(page.locator('#preview-document-details')).toBeVisible();
    await header.getByRole('button', { name: 'Hide document details', exact: true }).click();
  }
});

test('recent searches explain browser scope and hiding never deletes server history', async ({
  page,
}) => {
  const mutations: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/api/') && request.method() !== 'GET') {
      mutations.push(request.url());
    }
  });
  await page.goto('/');
  const recents = page.getByRole('region', { name: 'Recent searches' });
  await expect(recents).toContainText('Your history remains on the server.');
  await recents.getByRole('button', { name: 'Hide recent search on this browser: svelte' }).click();
  await expect(recents.getByRole('button', { name: 'svelte', exact: true })).toHaveCount(0);
  await expect(recents.getByRole('button', { name: 'golang', exact: true })).toBeVisible();
  await page.reload();
  await expect(recents.getByRole('button', { name: 'golang', exact: true })).toBeVisible();
  await expect(recents.getByRole('button', { name: 'svelte', exact: true })).toHaveCount(0);
  await recents.getByRole('button', { name: 'Hide all on this browser' }).click();
  await expect(recents).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('link', { name: 'Browse all indexed pages' })).toBeVisible();
  await expect(recents).toHaveCount(0);
  expect(mutations).toEqual([]);
});

for (const failure of ['server', 'network'] as const) {
  test(`a ${failure} preview failure can retry the same archived document`, async ({ page }) => {
    const requests: URLSearchParams[] = [];
    await page.route('**/api/preview?**', async (route) => {
      requests.push(new URL(route.request().url()).searchParams);
      if (requests.length === 1) {
        if (failure === 'network') await route.abort();
        else await route.fulfill({ status: 500, body: 'Internal server details' });
      } else {
        await route.fulfill({
          json: { ...preview, version_id: 7, version_created_at: '2023-11-14T22:13:20Z' },
        });
      }
    });
    await page.goto(previewPath(original, 7));
    await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
    await expect(page.getByRole('link', { name: 'Open original', exact: true })).toHaveAttribute(
      'href',
      original,
    );
    await expect(page.locator('body')).not.toContainText('Internal server details');
    await page.getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
    await expect(page.getByText('Viewing archived version from', { exact: false })).toBeVisible();
    await expect(page.getByRole('alert')).toHaveCount(0);
    expect(requests).toHaveLength(2);
    for (const request of requests) {
      expect(request.get('url')).toBe(original);
      expect(request.get('document_id')).toBe('document-1');
      expect(request.get('version')).toBe('7');
    }
  });

  test(`a ${failure} version history failure has a retry instead of an empty list`, async ({
    page,
  }) => {
    let attempts = 0;
    await page.route('**/api/versions?**', async (route) => {
      attempts++;
      if (attempts === 1) {
        if (failure === 'network') await route.abort();
        else await route.fulfill({ status: 500 });
      } else {
        await route.fulfill({
          json: [{ id: 7, created_at: '2023-11-14T22:13:20Z', text_diff: '+Older content' }],
        });
      }
    });
    await page.goto(previewPath());
    await page.getByRole('button', { name: 'Show document details', exact: true }).click();
    await page.getByRole('button', { name: '1 previous version', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Could not load previous versions.');
    await page.getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(page.getByRole('button', { name: 'show this version' })).toBeVisible();
    await expect(page.getByRole('alert')).toHaveCount(0);
    expect(attempts).toBe(2);
  });
}

test('version history distinguishes loading from an empty successful response', async ({
  page,
}) => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  await page.route('**/api/versions?**', async (route) => {
    await pending;
    await route.fulfill({ json: [] });
  });
  await page.goto(previewPath());
  await page.getByRole('button', { name: 'Show document details', exact: true }).click();
  await page.getByRole('button', { name: '1 previous version', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Loading previous versions…');
  finish();
  await expect(page.getByText('No previous versions available.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '1 previous version', exact: true }).click();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
});

test('preview errors do not offer unusable or unsafe original links', async ({ page }) => {
  await page.route('**/api/preview?**', (route) => route.fulfill({ status: 500 }));
  for (const url of [
    'remote-file://host/file.txt',
    'file:///private/file.txt',
    'javascript:alert(1)',
  ]) {
    await page.goto(previewPath(url));
    await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
    await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Open original', exact: true })).toHaveCount(0);
  }
});

test('retry preserves an explicitly selected extractor', async ({ page }) => {
  const requests: URLSearchParams[] = [];
  await page.route('**/api/preview?**', async (route) => {
    requests.push(new URL(route.request().url()).searchParams);
    if (requests.length === 2) await route.fulfill({ status: 500 });
    else await route.fulfill({ json: preview });
  });
  await page.goto(previewPath());
  await page.getByRole('button', { name: 'Change extractor' }).click();
  await page.getByRole('menuitemradio', { name: 'readability', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
  expect(requests).toHaveLength(3);
  expect(requests[1].get('extractor')).toBe('readability');
  expect(requests[2].toString()).toBe(requests[1].toString());
});

test('a delayed preview failure does not replace a newly selected document', async ({ page }) => {
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  await page.route('**/api/preview?**', async (route) => {
    if (new URL(route.request().url()).searchParams.get('url') === original) {
      await pending;
      await route.fulfill({ status: 500 });
    } else {
      await route.fulfill({ json: preview });
    }
  });
  await page.goto(previewPath());
  await expect(page.getByRole('status')).toHaveText('Loading…');
  // The standalone preview uses popstate to select a different document in the same panel.
  await page.evaluate((path) => {
    history.pushState({}, '', path);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, previewPath('https://example.com/another-article'));
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
  finish();
  await page.waitForLoadState('networkidle');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
});

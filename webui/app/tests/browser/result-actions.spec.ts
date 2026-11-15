// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type Page } from '@playwright/test';
import { mockAPI, original, preview, previewPath } from './fixtures';
import { emptySearchCapabilities } from '../../src/lib/search-schema';

for (const configured of [undefined, true, false]) {
  test(`suggestions follow the ${configured ?? 'omitted'} default and remember a browser override`, async ({
    page,
  }) => {
    const config = configured === undefined ? {} : { showSuggestions: configured };
    await mockAPI(page, { canWrite: false, config });
    await page.goto('/?q=sve');
    const search = page.getByRole('searchbox');
    const suggestions = page.getByRole('listbox', { name: 'Search suggestions' });
    const actions = page.getByRole('button', { name: 'Actions', exact: true });
    const preference = page.getByRole('checkbox', { name: 'Show suggestions', exact: true });
    const defaultEnabled = configured ?? false;
    await expect(page.locator('[data-result]')).toHaveCount(1);
    await search.focus();
    await expect(suggestions).toBeVisible({ visible: defaultEnabled });
    await expect(search).toHaveAttribute('aria-expanded', String(defaultEnabled));
    expect(await page.evaluate(() => localStorage.getItem('hister-show-suggestions'))).toBeNull();
    await actions.click();
    await expect(preference).toBeChecked({ checked: defaultEnabled });
    await preference.setChecked(!defaultEnabled);
    await page.keyboard.press('Escape');
    await search.focus();
    await expect(suggestions).toBeVisible({ visible: !defaultEnabled });
    await page.reload();
    await expect(page.locator('[data-result]')).toHaveCount(1);
    await search.focus();
    await expect(suggestions).toBeVisible({ visible: !defaultEnabled });
    await actions.click();
    await expect(preference).toBeChecked({ checked: !defaultEnabled });
    await preference.setChecked(defaultEnabled);
    await page.keyboard.press('Escape');
    await page.reload();
    await expect(page.locator('[data-result]')).toHaveCount(1);
    await search.focus();
    await expect(suggestions).toBeVisible({ visible: defaultEnabled });

    if (defaultEnabled) {
      await search.press('ArrowDown');
      await search.press('Enter');
      await expect(search).toHaveValue('svelte');
    } else {
      await expect(search).not.toHaveAttribute('aria-activedescendant');
      await search.press('Tab');
      await expect(search).toHaveValue('sve');
    }
  });
}

test('suggestions can be enabled from Actions when a search has no results', async ({ page }) => {
  await mockAPI(page, { canWrite: false, config: { showSuggestions: false } });
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() => socket.send(JSON.stringify({ documents: [], total: 0 })));
  });
  await page.goto('/?q=sve');
  await expect(page.getByText('0 results', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Actions', exact: true }).click();
  await page.getByRole('checkbox', { name: 'Show suggestions', exact: true }).check();
  await page.keyboard.press('Escape');
  await page.getByRole('searchbox').focus();
  await expect(page.getByRole('listbox', { name: 'Search suggestions' })).toBeVisible();
});

test('hidden suggestions skip facet lookups and preserve Tab result navigation', async ({
  page,
}) => {
  const searchConfig = emptySearchCapabilities();
  searchConfig.fields = [
    { name: 'domain', label: 'Domain', description: '', kind: 'keyword', facet: 'domains' },
  ];
  searchConfig.facets = [
    { name: 'domains', label: 'Domains', queryField: 'domain', kind: 'terms', defaultSize: 10 },
  ];
  await mockAPI(page, {
    config: { showSuggestions: false, search: searchConfig, hotkeys: { tab: 'autocomplete' } },
  });
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          documents: [
            { id: 'document-1', url: original, title: 'First article' },
            { id: 'document-2', url: `${original}/2`, title: 'Second article' },
          ],
          total: 2,
          query_suggestion: 'domain:example.com',
        }),
      ),
    );
  });
  let facetRequests = 0;
  await page.route('**/api/facets?**', async (route) => {
    facetRequests++;
    await route.fulfill({ json: { domains: [{ term: 'example.com', count: 1 }] } });
  });
  await page.goto('/?q=domain:exa');
  await expect(page.locator('[data-result]')).toHaveCount(2);
  const search = page.getByRole('searchbox');
  await search.focus();
  await search.press('Tab');
  await expect(search).toHaveValue('domain:exa');
  await expect(page.locator('[data-result]').nth(1)).toHaveClass(/result-card-active/);
  await page.waitForTimeout(250);
  expect(facetRequests).toBe(0);

  await page.getByRole('button', { name: 'Actions', exact: true }).click();
  await page.getByRole('checkbox', { name: 'Show suggestions', exact: true }).check();
  await page.keyboard.press('Escape');
  await search.focus();
  await expect.poll(() => facetRequests).toBeGreaterThan(0);
  await expect(page.getByRole('listbox', { name: 'Search suggestions' })).toBeVisible();
});

async function openActions(page: Page) {
  const trigger = page.getByRole('button', { name: `Actions for ${preview.title}`, exact: true });
  await expect(trigger).toBeVisible({ timeout: 3000 });
  await trigger.click();
  return trigger;
}

for (const colorScheme of ['light', 'dark'] as const) {
  test(`selecting results keeps their text in place in the ${colorScheme} theme`, async ({
    page,
    isMobile,
  }) => {
    await mockAPI(page, { config: { colorScheme } });
    const results = [
      { title: 'Pinned article', pinned: true, label: 'reading' },
      { title: 'Previously opened article', pinned: false, label: '' },
      { title: 'Another article', pinned: false, label: 'reference' },
    ].map((result, index) => ({
      ...result,
      id: `document-${index}`,
      url: `${original}/${index}`,
      domain: 'example.com',
      text: 'A saved article with useful context.',
    }));
    await page.routeWebSocket('**/search', (socket) => {
      socket.onMessage(() =>
        socket.send(
          JSON.stringify({
            history: results.slice(0, 2),
            documents: results.slice(2),
            total: results.length,
          }),
        ),
      );
    });
    await page.goto('/?q=article');
    const rows = page.locator('[data-result]');
    await expect(rows).toHaveCount(3);
    await page.evaluate(() => document.fonts.ready);
    if (colorScheme === 'dark') await expect(page.locator('html')).toHaveClass(/dark/);
    else await expect(page.locator('html')).not.toHaveClass(/dark/);
    await rows.first().locator('[data-result-link]').focus();

    async function textPositions() {
      return rows.evaluateAll((elements) =>
        elements.map((element) =>
          ['[data-result-link]', '.result-meta', '.result-snippet'].map((selector) => {
            const part = element.querySelector(selector)!;
            const bounds = part.getBoundingClientRect();
            const row = element.getBoundingClientRect();
            return {
              x: bounds.x - row.x,
              y: bounds.y - row.y,
              width: bounds.width,
              height: bounds.height,
            };
          }),
        ),
      );
    }
    const before = await textPositions();
    for (const selected of [1, 2, 0]) {
      await page.keyboard.press('ArrowDown');
      await expect(rows.nth(selected)).toHaveClass(/result-card-active/);
      await expect.poll(textPositions).toEqual(before);
    }
    await expect(
      rows.first().getByRole('img', { name: 'Pinned result', exact: true }),
    ).toBeVisible();
    await expect(
      rows.nth(1).getByRole('img', { name: 'Prioritized for this query', exact: true }),
    ).toBeVisible();
    await page.screenshot({ path: test.info().outputPath(`result-hierarchy-${colorScheme}.png`) });

    const trigger = rows.first().getByRole('button', { name: 'Actions for Pinned article' });
    await trigger.click();
    const actions = isMobile ? page.getByRole('dialog') : page.getByRole('menu');
    await expect(actions.getByRole('button', { name: 'Unpin', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    await rows
      .nth(1)
      .getByRole('button', { name: 'Actions for Previously opened article' })
      .click();
    await expect(
      actions.getByRole('button', { name: 'Forget for this query', exact: true }),
    ).toBeVisible();
  });
}

test('result titles wrap below favicons and sit close to their bodies with space between results', async ({
  page,
  isMobile,
}) => {
  await mockAPI(page);
  const titles = [
    preview.title,
    'A longer article title that wraps across multiple lines while keeping its source and excerpt close together',
    'Another long article title with no favicon that wraps while keeping the fallback icon beside the first line',
  ];
  await page.routeWebSocket('**/search', (socket) => {
    socket.onMessage(() =>
      socket.send(
        JSON.stringify({
          documents: titles.map((title, index) => ({
            id: `document-${index}`,
            url: `${original}/${index}`,
            title,
            domain: 'example.com',
            favicon:
              index < 2
                ? 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20"%3E%3Crect width="20" height="20" rx="3" fill="%230891b2"/%3E%3Cpath d="M5 4v12m10-12v12M5 10h10" stroke="white" stroke-width="2"/%3E%3C/svg%3E'
                : '',
            text: index === 2 ? '' : 'A short excerpt from the saved article.',
          })),
          total: titles.length,
        }),
      ),
    );
  });
  await page.goto('/?q=article');
  const rows = page.locator('[data-result]');
  await expect(rows).toHaveCount(titles.length);
  await page.evaluate(() => document.fonts.ready);

  async function checkSpacing() {
    for (let index = 0; index < titles.length; index++) {
      const row = rows.nth(index);
      const title = (await row.locator('[data-result-link]').boundingBox())!;
      const body = (await row.locator('.result-meta').boundingBox())!;
      const actions = (await row.getByRole('button', { name: /^Actions for / }).boundingBox())!;
      const titleGap = body.y - (title.y + title.height);
      expect(titleGap).toBeGreaterThanOrEqual(0);
      expect(titleGap).toBeLessThanOrEqual(4);
      expect(actions.height).toBeGreaterThanOrEqual(isMobile ? 44 : 32);
      expect(actions.width).toBeGreaterThanOrEqual(isMobile ? 44 : 32);
      expect(title.x + title.width).toBeLessThanOrEqual(actions.x);
      expect(body.x + body.width).toBeLessThanOrEqual(actions.x);
      if (index > 0) {
        const lines = await row.locator('[data-result-link]').evaluate((link) => {
          const walker = document.createTreeWalker(link, NodeFilter.SHOW_TEXT);
          const lines: { x: number; y: number }[] = [];
          while (walker.nextNode()) {
            if (!walker.currentNode.textContent?.trim()) continue;
            const range = document.createRange();
            range.selectNodeContents(walker.currentNode);
            for (const rect of range.getClientRects()) {
              if (rect.width > 0 && !lines.some((line) => Math.abs(line.y - rect.y) < 1)) {
                lines.push({ x: rect.x, y: rect.y });
              }
            }
          }
          return lines;
        });
        expect(lines.length).toBeGreaterThan(1);
        expect(lines[0].x - body.x).toBeGreaterThanOrEqual(20);
        expect(Math.abs(lines[1].x - body.x)).toBeLessThanOrEqual(1);
      }
      if (index > 0) {
        const previous = (await rows.nth(index - 1).boundingBox())!;
        const current = (await row.boundingBox())!;
        expect(current.y - (previous.y + previous.height)).toBeGreaterThanOrEqual(20);
      }
    }
  }

  await expect(checkSpacing).toPass({ timeout: 5000 });
  await page.screenshot({ path: test.info().outputPath('result-spacing.png') });
  if (!isMobile) {
    await page.getByRole('button', { name: 'Close preview', exact: true }).click();
    await expect(checkSpacing).toPass({ timeout: 5000 });
  }
  const trigger = rows.nth(1).getByRole('button', { name: /^Actions for / });
  await trigger.click();
  await expect(isMobile ? page.getByRole('dialog') : page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
});

test('result actions use a bottom sheet on phones and a dropdown on desktop', async ({
  page,
  isMobile,
}) => {
  await mockAPI(page);
  await page.goto('/?q=article');
  const trigger = await openActions(page);
  if (isMobile) {
    const sheet = page.getByRole('dialog', { name: 'Result actions', exact: true });
    await expect(sheet).toBeVisible();
    await expect(sheet.getByRole('textbox')).toHaveCount(0);
    for (const name of ['Pin result', 'Edit label', 'Disable indexing', 'Delete result']) {
      const action = sheet.getByRole('button', { name, exact: true });
      await expect(action).toBeVisible();
      expect((await action.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    }
    await expect(async () => {
      const box = (await sheet.boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
      expect(Math.abs(box.y + box.height - page.viewportSize()!.height)).toBeLessThan(2);
    }).toPass();
    await page.screenshot({ path: test.info().outputPath('result-actions.png') });
    for (let tab = 0; tab < 6; tab++) {
      await page.keyboard.press('Tab');
      await expect(sheet.locator(':focus')).toHaveCount(1);
    }
    await page.keyboard.press('Escape');
    await expect(sheet).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await expect(page).toHaveURL(/q=article/);
    await trigger.click();
    await sheet.getByRole('button', { name: 'Close result actions' }).click();
    await expect(trigger).toBeFocused();
  } else {
    await expect(page.getByRole('menu')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Pin', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
});

test('the mobile label editor preserves input after failure and updates the result after saving', async ({
  page,
  isMobile,
}) => {
  test.skip(!isMobile, 'Mobile editor');
  await mockAPI(page);
  const bodies: unknown[] = [];
  await page.route('**/api/label', async (route) => {
    bodies.push(route.request().postDataJSON());
    if (bodies.length === 1) await route.abort();
    else if (bodies.length === 2) await route.fulfill({ status: 500 });
    else await route.fulfill({ json: {} });
  });
  await page.goto('/?q=article');
  await openActions(page);
  await page.getByRole('button', { name: 'Edit label', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Edit label', exact: true });
  await expect(editor.getByRole('textbox', { name: 'Label', exact: true })).toHaveValue('reading');
  await editor.getByRole('textbox', { name: 'Label', exact: true }).fill('research');
  await page.screenshot({ path: test.info().outputPath('label-editor.png') });
  for (let attempt = 0; attempt < 2; attempt++) {
    await editor.getByRole('button', { name: 'Save label' }).click();
    await expect(editor.getByRole('alert')).toContainText('Failed to save label.');
    await expect(editor.getByRole('textbox', { name: 'Label', exact: true })).toHaveValue(
      'research',
    );
  }
  await editor.getByRole('button', { name: 'Save label' }).click();
  await expect(editor.getByRole('status')).toHaveText('Label saved.');
  await editor.getByRole('button', { name: 'Back to actions' }).click();
  await page.getByRole('button', { name: 'Close result actions' }).click();
  await expect(page.locator('[data-result]').getByText('research', { exact: true })).toBeVisible();
  expect(bodies).toEqual(Array(3).fill({ url: original, label: 'research' }));
});

test('the mobile pin editor submits its query with Enter and reports server failures', async ({
  page,
  isMobile,
}) => {
  test.skip(!isMobile, 'Mobile editor');
  await mockAPI(page);
  const bodies: unknown[] = [];
  await page.route('**/api/history', async (route) => {
    bodies.push(route.request().postDataJSON());
    await route.fulfill({ status: bodies.length === 1 ? 500 : 200, json: {} });
  });
  await page.goto('/?q=article');
  await openActions(page);
  await page.getByRole('button', { name: 'Pin result', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Pin result', exact: true });
  const query = editor.getByRole('textbox', { name: 'Search query' });
  await expect(query).toHaveValue('article');
  await query.fill('research');
  await query.press('Enter');
  await expect(editor.getByRole('alert')).toHaveText('Failed to update priority.');
  await query.press('Enter');
  await expect(editor.getByRole('status')).toHaveText('Priority result added.');
  await expect(page).toHaveURL(/q=article/);
  expect(bodies).toEqual(
    Array(2).fill({ url: original, title: preview.title, query: 'research', pin: true }),
  );
});

for (const confirm of [false, true]) {
  test(`mobile skip rules ${confirm ? 'delete matches only after confirmation' : 'keep documents when deletion is canceled'}`, async ({
    page,
    isMobile,
  }) => {
    test.skip(!isMobile, 'Mobile editor');
    await mockAPI(page);
    const deletions: { dry_run?: boolean }[] = [];
    let savedRules = '';
    await page.route('**/api/rules', async (route) => {
      if (route.request().method() === 'POST') savedRules = route.request().postData() ?? '';
      await route.fulfill({ json: { allow: [], skip: [], priority: [], versioning: [] } });
    });
    await page.route('**/api/delete', async (route) => {
      const body = route.request().postDataJSON();
      deletions.push(body);
      await route.fulfill({ json: body.dry_run ? { matched: 2 } : { deleted: 2 } });
    });
    await page.goto('/?q=article');
    await openActions(page);
    await page.getByRole('button', { name: 'Disable indexing', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Disable indexing', exact: true });
    await editor.getByRole('checkbox', { name: 'Delete matching documents' }).check();
    await editor.getByRole('button', { name: 'This URL', exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: 'Delete matching documents?' });
    await expect(confirmation).toBeVisible();
    expect(deletions).toHaveLength(1);
    expect(deletions[0].dry_run).toBe(true);
    expect(original).toMatch(new RegExp(new URLSearchParams(savedRules).get('skip')!));
    await confirmation
      .getByRole('button', { name: confirm ? 'Delete 2 documents' : 'Cancel', exact: true })
      .click();
    await expect(confirmation).toHaveCount(0);
    if (confirm) {
      await expect(page.locator('[data-result]')).toHaveCount(0);
      expect(deletions).toHaveLength(2);
      expect(deletions[1].dry_run).toBeUndefined();
    } else {
      await expect(page.locator('[data-result]')).toHaveCount(1);
      expect(deletions).toHaveLength(1);
    }
  });
}

test('pinned and history results retain their mobile actions', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'Mobile actions');
  await mockAPI(page, { historyResult: true, pinned: true });
  const bodies: unknown[] = [];
  await page.route('**/api/history', async (route) => {
    bodies.push(route.request().postDataJSON());
    await route.fulfill({ json: {} });
  });
  await page.goto('/?q=article');
  await openActions(page);
  const sheet = page.getByRole('dialog', { name: 'Result actions', exact: true });
  await expect(sheet.getByRole('button', { name: 'Pin result', exact: true })).toHaveCount(0);
  await expect(sheet.getByRole('button', { name: 'Delete result', exact: true })).toHaveCount(0);
  await sheet.getByRole('button', { name: 'Unpin', exact: true }).click();
  await expect(sheet.getByRole('status')).toHaveText('Priority result removed.');
  await sheet.getByRole('button', { name: 'Forget for this query', exact: true }).click();
  await expect(sheet).toHaveCount(0);
  expect(bodies).toEqual([
    { url: original, title: preview.title, query: 'article', pin: false },
    { url: original, query: 'article', delete: true },
  ]);
});

test('read only result actions offer copying without editing controls', async ({
  page,
  isMobile,
}) => {
  await mockAPI(page, { canWrite: false });
  await page.goto('/?q=article');
  await openActions(page);
  const actions = isMobile
    ? page.getByRole('dialog', { name: 'Result actions', exact: true })
    : page.getByRole('menu');
  await expect(actions.getByRole('button', { name: 'Copy URL', exact: true })).toBeVisible();
  await expect(actions.getByRole('textbox')).toHaveCount(0);
  for (const name of [
    'Pin',
    'Pin result',
    'Edit label',
    'Save',
    'Disable indexing',
    'Delete result',
  ]) {
    await expect(actions.getByRole('button', { name, exact: true })).toHaveCount(0);
  }
});

for (const canWrite of [false, true]) {
  test(`result actions copy the original URL for ${canWrite ? 'editing' : 'read only'} users`, async ({
    page,
    context,
    isMobile,
  }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await mockAPI(page, { canWrite });
    await page.goto('/?q=article');
    const row = page.locator('[data-result]');
    await expect(row.getByRole('button', { name: 'Copy URL' })).toHaveCount(0);
    const trigger = await openActions(page);
    const actions = isMobile
      ? page.getByRole('dialog', { name: 'Result actions', exact: true })
      : page.getByRole('menu');
    const copy = actions.getByRole('button', { name: 'Copy URL', exact: true });
    await copy.focus();
    await page.keyboard.press('Enter');
    await expect(actions.getByRole('status')).toHaveText('URL copied.');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(original);
    await page.keyboard.press('Escape');
    await expect(trigger).toBeFocused();
    await expect(page).toHaveURL(/q=article/);
  });
}

test('copy failure is reported and can be retried', async ({ page, context, isMobile }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await mockAPI(page);
  await page.goto('/?q=article');
  await page.evaluate(() => {
    const writeText = navigator.clipboard.writeText.bind(navigator.clipboard);
    navigator.clipboard.writeText = async (text) => {
      navigator.clipboard.writeText = writeText;
      throw new DOMException('Permission denied', 'NotAllowedError');
    };
  });
  await openActions(page);
  const actions = isMobile
    ? page.getByRole('dialog', { name: 'Result actions', exact: true })
    : page.getByRole('menu');
  const copy = actions.getByRole('button', { name: 'Copy URL', exact: true });
  await copy.click();
  await expect(actions.getByRole('alert')).toHaveText('Could not copy URL. Please try again.');
  await copy.click();
  await expect(actions.getByRole('status')).toHaveText('URL copied.');
  await expect(actions.getByRole('alert')).toHaveCount(0);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(original);
});

for (const [query, deletionQuery] of [
  ['article', 'article'],
  ['article sort:date', 'article'],
  ['title: sort:date sort:date', 'title: sort:date'],
  ['sort:date', '*'],
]) {
  test(`delete all matching results confirms and removes six matches for ${query}`, async ({
    page,
  }) => {
    const search = emptySearchCapabilities();
    search.fields = [{ name: 'title', label: 'Title', description: '', kind: 'text' }];
    search.sort = {
      field: 'sort',
      label: 'Sort',
      description: '',
      options: [{ value: 'date', label: 'Date', visible: true }],
    };
    await mockAPI(page, { config: { search } });
    let documents = Array.from({ length: 6 }, (_, index) => ({
      id: `document-${index}`,
      url: `${original}/${index}`,
      title: `Article ${index + 1}`,
      text: 'An indexed article',
    }));
    await page.routeWebSocket('**/search', (socket) => {
      socket.onMessage(() => socket.send(JSON.stringify({ documents, total: documents.length })));
    });
    const requests: unknown[] = [];
    await page.route('**/api/delete', async (route) => {
      requests.push(route.request().postDataJSON());
      documents = [];
      await route.fulfill({ json: { deleted: 6 } });
    });
    await page.goto(`/?${new URLSearchParams({ q: query })}`);
    await expect(page.locator('[data-result]')).toHaveCount(6);
    await page.getByRole('button', { name: 'Actions', exact: true }).click();
    await page.getByRole('button', { name: 'Delete all matching results', exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: 'Delete all matching results' });
    await expect(confirmation).toBeVisible();
    await expect(confirmation).toContainText('all 6 result(s)');
    expect(requests).toHaveLength(0);
    await confirmation.getByRole('button', { name: 'No', exact: true }).click();
    await expect(confirmation).toHaveCount(0);
    await expect(page.locator('[data-result]')).toHaveCount(6);
    expect(requests).toHaveLength(0);
    await page.getByRole('button', { name: 'Delete all matching results', exact: true }).click();
    await confirmation.getByRole('button', { name: 'Yes, delete all', exact: true }).click();
    await expect(confirmation).toHaveCount(0);
    await expect(page.locator('[data-result]')).toHaveCount(0);
    await expect(page.getByText('0 results', { exact: true })).toBeVisible();
    expect(requests).toEqual([{ query: deletionQuery }]);
  });
}

test('the mobile delete action removes the selected result', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'Mobile actions');
  await mockAPI(page);
  const requests: unknown[] = [];
  await page.route('**/api/delete', async (route) => {
    requests.push(route.request().postDataJSON());
    await route.fulfill({ json: { deleted: 1 } });
  });
  await page.goto('/?q=article');
  await openActions(page);
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Delete result', exact: true })
    .click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('[data-result]')).toHaveCount(0);
  expect(requests).toEqual([{ query: `url:"${original}"` }]);
});

test('changing viewport closes the previous menu and fits the new layout', async ({ page }) => {
  await mockAPI(page);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/?q=article');
  await openActions(page);
  await expect(page.getByRole('dialog', { name: 'Result actions', exact: true })).toBeVisible();
  await page.setViewportSize({ width: 1024, height: 768 });
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await openActions(page);
  await expect(page.getByRole('menu')).toBeVisible();
  await page.setViewportSize({ width: 320, height: 568 });
  await expect(page.getByRole('menu')).toHaveCount(0);
  await openActions(page);
  await page.getByRole('button', { name: 'Disable indexing', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Disable indexing', exact: true });
  await expect(editor.getByRole('button', { name: 'This Domain', exact: true })).toBeInViewport();
  await expect(async () => {
    const box = (await editor.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(320);
    expect(box.y).toBeGreaterThanOrEqual(0);
    expect(box.y + box.height).toBeLessThanOrEqual(568);
  }).toPass();
  await page.screenshot({ path: test.info().outputPath('skip-editor-small.png') });
});

test('preview close buttons are named while loading, after failure, and after recovery', async ({
  page,
}) => {
  await mockAPI(page);
  let finish: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  let attempts = 0;
  await page.route('**/api/preview?**', async (route) => {
    if (++attempts === 1) {
      await pending;
      await route.fulfill({ status: 500 });
    } else {
      await route.fulfill({ json: preview });
    }
  });
  await page.goto(previewPath());
  await expect(page.getByRole('status')).toHaveText('Loading…');
  await expect(page.getByRole('button', { name: 'Close preview', exact: true })).toBeVisible();
  finish();
  await expect(page.getByRole('alert')).toContainText('Could not load this preview.');
  await expect(page.getByRole('button', { name: 'Close preview', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByText('Saved article content', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Close preview', exact: true }).click();
  await expect(page.getByRole('searchbox', { name: 'Search your history' })).toBeVisible();
});

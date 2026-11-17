// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test } from '@playwright/test';

test('canonical metadata uses the Nogon domain and preserves existing page paths', async ({
  page,
}) => {
  for (const path of ['/', '/docs/quickstart', '/posts/how-hister-works']) {
    await page.goto(path);
    const canonical = `https://nogon.eu${path}`;
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', canonical);
    await expect(page.locator('meta[property="og:url"]')).toHaveAttribute('content', canonical);
    await expect(page.locator('meta[property="og:image"]')).toHaveAttribute(
      'content',
      'https://nogon.eu/logo.png',
    );
    const schemas = await page.locator('script[type="application/ld+json"]').allTextContents();
    expect(schemas.length).toBeGreaterThan(0);
    for (const schema of schemas) {
      const data = JSON.parse(schema);
      expect(data['@graph'].some((node: { url?: string }) => node.url === canonical)).toBe(true);
      expect(schema).not.toContain('hister.org');
    }
  }
});

test('discovery links use Nogon while existing RSS article identities remain stable', async ({
  page,
  request,
}) => {
  await page.goto('/');
  const robots = await request.get('/robots.txt');
  expect(robots.ok()).toBe(true);
  expect(await robots.text()).toContain('Sitemap: https://nogon.eu/sitemap.xml');

  for (const path of ['/sitemap.xml', '/posts/rss.xml']) {
    const response = await request.get(path);
    expect(response.ok()).toBe(true);
    const parsed = await page.evaluate(
      (xml) => {
        const document = new DOMParser().parseFromString(xml, 'application/xml');
        return {
          errors: document.querySelectorAll('parsererror').length,
          locations: Array.from(document.querySelectorAll('loc'), (node) => node.textContent),
          links: Array.from(
            document.querySelectorAll('link'),
            (node) => node.getAttribute('href') || node.textContent,
          ),
          items: Array.from(document.querySelectorAll('item'), (item) => ({
            link: item.querySelector('link')?.textContent,
            guid: item.querySelector('guid')?.textContent,
          })),
        };
      },
      await response.text(),
    );
    expect(parsed.errors).toBe(0);
    const urls = [...parsed.locations, ...parsed.links];
    expect(urls.length).toBeGreaterThan(0);
    for (const url of urls) expect(url).toMatch(/^https:\/\/nogon\.eu\//);
    if (path === '/posts/rss.xml') {
      expect(parsed.items.length).toBeGreaterThan(0);
      for (const item of parsed.items) {
        expect(item.guid).toBe(item.link?.replace('https://nogon.eu/', 'https://hister.org/'));
      }
    } else {
      expect(parsed.locations).toContain('https://nogon.eu/docs/quickstart');
      expect(parsed.locations).toContain('https://nogon.eu/posts/how-hister-works');
    }
  }
});

test('new users can follow the setup steps from installation to their first search', async ({
  page,
}) => {
  await page.goto('/docs');
  await expect(page.getByRole('heading', { level: 2 }).first()).toHaveText(
    'Start searching with Nogon',
  );

  for (const [link, destination] of [
    ['Install Nogon', 'Installing Nogon'],
    ['Connect your browser', 'Installing a Browser Extension'],
    ['Find your first page', 'Accessing the Web Interface'],
  ]) {
    const setup = page.getByRole('region', { name: 'Start searching with Nogon' });
    await setup.getByRole('link', { name: new RegExp(link) }).click();
    await expect(page.getByRole('heading', { name: destination, exact: true })).toBeInViewport();
    if (link === 'Find your first page') {
      await expect(
        page.getByText('Search for a few distinctive words from the page.', { exact: false }),
      ).toBeVisible();
    }
    await page.goto('/docs');
  }

  await page.getByRole('link', { name: /^Configuration Reference/ }).click();
  await expect(
    page.getByRole('heading', { name: 'Configuration Reference', level: 1 }),
  ).toBeVisible();
});

test('Start Here guides follow setup without a separate task section', async ({ page }) => {
  await page.goto('/docs');
  await expect(page.getByRole('region', { name: 'Common tasks' })).toHaveCount(0);
  await expect(page.getByRole('heading', { level: 2 }).nth(1)).toHaveText('Start Here');
  const guides = page.getByRole('region', { name: 'Start Here', exact: true });
  await expect(guides).toBeVisible();
  await guides.getByRole('link', { name: /^What Nogon Is/ }).focus();
  await page.keyboard.press('Tab');
  await expect(guides.getByRole('link', { name: /^Quickstart/ })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Quickstart', level: 1 })).toBeVisible();
});

test('the quickstart shows its first command without scrolling', async ({ page }) => {
  await page.goto('/docs/quickstart');
  await page.evaluate(() => document.fonts.ready);
  await expect(page.getByRole('button', { name: 'Copy code block' }).first()).toBeVisible();
  await expect(page.getByText('./nogon listen', { exact: true })).toBeInViewport({ ratio: 1 });
});

test('the docs entry page fits narrow phone screens', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await page.goto('/docs');
  await page.evaluate(() => document.fonts.ready);
  const width = await page.evaluate(() => ({
    content: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth,
  }));
  expect(width.content).toBeLessThanOrEqual(width.viewport);
});

test('documentation search, code copying, and page navigation remain available', async ({
  page,
  context,
  isMobile,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto('/docs/quickstart');
  await page.getByRole('button', { name: 'Copy code block' }).first().click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe('./nogon listen');

  if (isMobile) await page.getByRole('article').getByText('On this page', { exact: true }).click();
  await page
    .getByRole('navigation', { name: 'On this page', exact: true })
    .getByRole('link', { name: 'Installing a Browser Extension', exact: true })
    .click();
  await expect(
    page.getByRole('heading', { name: 'Installing a Browser Extension', exact: true }),
  ).toBeInViewport();

  const search = page.getByRole('combobox', { name: 'Search all documentation' });
  await search.fill('MCP');
  await expect(page.getByRole('option').first()).toContainText('MCP Integration');
  await search.press('Enter');
  await expect(page.getByRole('heading', { name: 'MCP Integration', level: 1 })).toBeVisible();

  if (isMobile) await page.getByText('Browse', { exact: true }).click();
  await page
    .getByRole('navigation', { name: 'Documentation', exact: true })
    .getByRole('link', { name: 'Quickstart', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'Quickstart', level: 1 })).toBeVisible();
});

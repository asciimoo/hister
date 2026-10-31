// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test } from '@playwright/test';

test('new users can follow the setup steps from installation to their first search', async ({
  page,
}) => {
  await page.goto('/docs');
  await expect(page.getByRole('heading', { level: 2 }).first()).toHaveText(
    'Start searching with Hister',
  );

  for (const [link, destination] of [
    ['Install Hister', 'Installing Hister'],
    ['Connect your browser', 'Installing a Browser Extension'],
    ['Find your first page', 'Accessing the Web Interface'],
  ]) {
    const setup = page.getByRole('region', { name: 'Start searching with Hister' });
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
  await expect(page.getByText('Start Here', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2 }).nth(1)).toHaveText('What Hister Is');
  await page.getByRole('link', { name: /^Quickstart/ }).click();
  await expect(page.getByRole('heading', { name: 'Quickstart', level: 1 })).toBeVisible();
});

test('the quickstart shows its first command without scrolling', async ({ page }) => {
  await page.goto('/docs/quickstart');
  await page.evaluate(() => document.fonts.ready);
  await expect(page.getByRole('button', { name: 'Copy code block' }).first()).toBeVisible();
  await expect(page.getByText('./hister listen', { exact: true })).toBeInViewport({ ratio: 1 });
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
    .toBe('./hister listen');

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

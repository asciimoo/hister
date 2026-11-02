// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type Page } from '@playwright/test';
import { mockAPI, previewPath } from './fixtures';

async function mockLogin(page: Page, authMode: 'token' | 'user', publicMode = false) {
  const config = {
    basePath: '',
    authMode,
    authenticated: false,
    canWrite: false,
    canConfigure: false,
    public: publicMode,
    oauthProviders: ['oidc'],
  };
  await mockAPI(page, { config });
  await page.route(`**/api/${authMode === 'token' ? 'token-login' : 'login'}`, async (route) => {
    const body = route.request().postDataJSON();
    if ((body.token ?? body.password) !== 'correct') {
      await route.fulfill({ status: 401 });
      return;
    }
    config.authenticated = true;
    config.canWrite = true;
    config.canConfigure = true;
    await route.fulfill({ json: { username: 'alice' } });
  });
  return config;
}

async function signIn(page: Page, mode: 'token' | 'user', password = 'correct') {
  if (mode === 'user') await page.getByRole('textbox', { name: 'Username' }).fill('alice');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
}

for (const mode of ['token', 'user'] as const) {
  test(`${mode} login protects settings discovery and returns to Settings`, async ({ page }) => {
    const config = await mockLogin(page, mode, true);
    let anonymousReads = 0;
    page.on('request', (request) => {
      if (!config.authenticated && new URL(request.url()).pathname === '/api/settings')
        anonymousReads++;
    });
    await page.goto('/settings');
    await expect(page).toHaveURL(
      (url) => url.pathname === '/auth' && url.searchParams.get('return_to') === '/settings',
    );
    expect(anonymousReads).toBe(0);
    await signIn(page, mode);
    await expect(page).toHaveURL('http://127.0.0.1:4174/settings');
    await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  });

  test(`${mode} login returns to a protected preview after a failed attempt`, async ({ page }) => {
    const config = await mockLogin(page, mode);
    await page.route('**/api/preview?*', async (route) => {
      if (config.authenticated) await route.fallback();
      else await route.fulfill({ status: 403 });
    });
    const target = previewPath() + '#saved-section';
    await page.goto(target);
    await expect(page).toHaveURL(
      (url) => url.pathname === '/auth' && url.searchParams.get('return_to') === target,
    );
    await signIn(page, mode, 'wrong');
    await expect(page).toHaveURL((url) => url.pathname === '/auth');
    await signIn(page, mode);
    await expect(page).toHaveURL(new URL(target, 'http://127.0.0.1:4174').href);
    await expect(page.getByText('Saved article content')).toBeVisible();
  });
}

for (const path of ['/add', '/rules', '/profile', '/settings']) {
  test(`public mode login returns to ${path}`, async ({ page }) => {
    await mockLogin(page, 'user', true);
    const target = path + '?source=bookmark#saved-section';
    await page.goto(target);
    await expect(page).toHaveURL((url) => url.searchParams.get('return_to') === target);
    await signIn(page, 'user');
    await expect(page).toHaveURL(new URL(target, 'http://127.0.0.1:4174').href);
  });
}

test('a rejected configuration request preserves the search query and fragment', async ({
  page,
}) => {
  const config = await mockLogin(page, 'token');
  await page.route('**/api/config', async (route) => {
    if (!config.authenticated && new URL(page.url()).pathname !== '/auth') {
      await route.fulfill({ status: 403 });
    } else {
      await route.fallback();
    }
  });
  const target = '/?q=needle#results';
  await page.goto(target);
  await expect(page).toHaveURL((url) => url.searchParams.get('return_to') === target);
  await signIn(page, 'token');
  await expect(page).toHaveURL(new URL(target, 'http://127.0.0.1:4174').href);
});

test('OAuth receives the local return destination', async ({ page }) => {
  await mockLogin(page, 'user');
  let returnTo: string | null = null;
  await page.route('**/api/oauth?*', async (route) => {
    returnTo = new URL(route.request().url()).searchParams.get('return_to');
    await route.fulfill({ contentType: 'text/plain', body: 'OAuth login started' });
  });
  const target = previewPath() + '#saved-section';
  await page.goto('/auth?' + new URLSearchParams({ return_to: target }));
  await page.getByRole('button', { name: 'Sign in with SSO' }).click();
  await expect(page.getByText('OAuth login started')).toBeVisible();
  expect(returnTo).toBe(target);
});

test('an authenticated visitor follows the requested page without another login', async ({
  page,
}) => {
  await mockAPI(page);
  await page.goto('/auth?' + new URLSearchParams({ return_to: '/help#query-syntax' }));
  await expect(page).toHaveURL('http://127.0.0.1:4174/help#query-syntax');
});

test('manual login from the menu preserves the current page', async ({ page }) => {
  await mockLogin(page, 'user', true);
  await page.goto('/help#query-syntax');
  await page.getByRole('button', { name: 'Open navigation menu' }).click();
  await page.getByRole('menuitem', { name: 'Login', exact: true }).click();
  await expect(page).toHaveURL((url) => url.searchParams.get('return_to') === '/help#query-syntax');
  await signIn(page, 'user');
  await expect(page).toHaveURL('http://127.0.0.1:4174/help#query-syntax');
});

test('token mode does not send the unavailable profile page through a login loop', async ({
  page,
}) => {
  const config = await mockLogin(page, 'token');
  config.authenticated = true;
  config.canWrite = true;
  await page.goto('/auth?' + new URLSearchParams({ return_to: '/profile' }));
  await expect(page).toHaveURL('http://127.0.0.1:4174/');
});

for (const [target, expected] of [
  ['/hister/settings#server', '/hister/settings#server'],
  ['/hister/preview?id=article#saved-section', '/hister/preview?id=article#saved-section'],
  ['/preview', '/hister/'],
  ['/hister-other/preview', '/hister/'],
  ['/hister/%2e%2e/preview', '/hister/'],
  ['/hister/%2f%2foutside.example', '/hister/'],
  ['/hister/api/oauth?provider=oidc', '/hister/'],
]) {
  test(`base path login validates ${target}`, async ({ page }) => {
    const config = await mockLogin(page, 'token');
    config.basePath = '/hister';
    await page.route('http://127.0.0.1:4174/hister/**', (route) =>
      route.fulfill({ contentType: 'text/plain', body: 'Returned to Hister' }),
    );
    await page.goto('/auth?' + new URLSearchParams({ return_to: target }));
    await signIn(page, 'token');
    await expect(page).toHaveURL(new URL(expected, 'http://127.0.0.1:4174').href);
  });
}

for (const target of [
  '',
  'https://outside.example/',
  '//outside.example/',
  '/\\outside.example/',
  '/%xx',
  '/preview\n',
  '/api/oauth?provider=oidc',
  '/auth',
]) {
  test(`login rejects return destination ${target}`, async ({ page }) => {
    await mockLogin(page, 'token');
    await page.goto('/auth?' + new URLSearchParams({ return_to: target }));
    await signIn(page, 'token');
    await expect(page).toHaveURL('http://127.0.0.1:4174/');
  });
}

async function mockProfile(page: Page, revision = 4) {
  await mockAPI(page, { config: { authMode: 'user', authenticated: true } });
  const profile = { user_id: 7, username: 'alice', is_admin: false, revision };
  await page.route('**/api/profile', (route) => route.fulfill({ json: profile }));
  return profile;
}

async function confirmTokenRotation(page: Page) {
  await page.getByRole('main').getByRole('button', { name: 'Reset Token', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Reset Token', exact: true }).click();
}

test('profile token rotation uses the account ID and updates its revision', async ({ page }) => {
  await mockProfile(page);
  const revisions: number[] = [];
  await page.route('**/api/profile/token', (route) => route.fulfill({ status: 404 }));
  await page.route('**/api/users/7/token', async (route) => {
    expect(route.request().method()).toBe('POST');
    const body = route.request().postDataJSON();
    expect(body).toEqual({ revision: 4 + revisions.length });
    revisions.push(body.revision);
    await route.fulfill({
      json: { token: `new-token-${revisions.length}`, revision: body.revision + 1 },
    });
  });
  await page.goto('/profile');
  await expect(page.getByText('alice', { exact: true })).toBeVisible();
  await confirmTokenRotation(page);
  await expect(page.getByText('new-token-1', { exact: true })).toBeVisible();
  await confirmTokenRotation(page);
  await expect(page.getByText('new-token-2', { exact: true })).toBeVisible();
  await expect(page.getByText('new-token-1', { exact: true })).toHaveCount(0);
  expect(revisions).toEqual([4, 5]);
});

test('profile token rotation requires a reload after an account conflict', async ({ page }) => {
  const profile = await mockProfile(page);
  let requests = 0;
  await page.route('**/api/users/7/token', async (route) => {
    requests++;
    if (requests === 1) {
      expect(route.request().postDataJSON()).toEqual({ revision: 4 });
      profile.revision = 5;
      await route.fulfill({
        status: 409,
        json: { code: 'stale_revision', error: 'user has changed' },
      });
    } else {
      expect(route.request().postDataJSON()).toEqual({ revision: 5 });
      await route.fulfill({ json: { token: 'new-token', revision: 6 } });
    }
  });
  await page.goto('/profile');
  await expect(page.getByText('alice', { exact: true })).toBeVisible();
  await confirmTokenRotation(page);
  await expect(page.getByRole('status')).toContainText('account changed');
  await expect(
    page.getByRole('main').getByRole('button', { name: 'Reset Token', exact: true }),
  ).toBeDisabled();
  expect(requests).toBe(1);
  await page.reload();
  await expect(
    page.getByRole('main').getByRole('button', { name: 'Reset Token', exact: true }),
  ).toBeEnabled();
  await confirmTokenRotation(page);
  await expect(page.getByText('new-token', { exact: true })).toBeVisible();
  expect(requests).toBe(2);
});

test('profile token rotation stays disabled when profile loading fails', async ({ page }) => {
  await mockProfile(page);
  await page.route('**/api/profile', (route) => route.fulfill({ status: 500, json: {} }));
  await page.goto('/profile');
  await expect(page.getByRole('status')).toContainText('Failed to load profile');
  await expect(
    page.getByRole('main').getByRole('button', { name: 'Reset Token', exact: true }),
  ).toBeDisabled();
});

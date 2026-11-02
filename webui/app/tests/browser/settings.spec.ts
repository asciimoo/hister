// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type Page } from '@playwright/test';
import { mockAPI } from './fixtures';
import type { SettingsState } from '../../src/lib/settings';

async function settingsAPI(page: Page, config: Record<string, unknown> = {}) {
  const publicConfig = { canConfigure: true, ...config };
  await mockAPI(page, { config: publicConfig });
  const fields = [
    { path: 'app.title', label: 'Title', type: 'string', control: 'text', default: 'Hister' },
    { path: 'app.subtitle', label: 'Subtitle', type: 'string', control: 'text', default: '' },
    {
      path: 'app.snippet_length',
      label: 'Snippet length',
      type: 'integer',
      control: 'number',
      default: 300,
      min: 1,
      max: 10000,
    },
    {
      path: 'app.color_scheme',
      label: 'Color scheme',
      type: 'string',
      control: 'select',
      default: 'automatic',
      choices: ['automatic', 'light', 'dark'],
    },
    {
      path: 'app.show_suggestions',
      label: 'Show suggestions',
      type: 'boolean',
      control: 'switch',
      default: false,
    },
    {
      path: 'server.address',
      label: 'Listen address',
      type: 'string',
      control: 'text',
      default: '127.0.0.1:4433',
    },
  ].map((field) => ({
    path: field.path,
    category: field.path.split('.')[0],
    label: field.label,
    description: `${field.label} description.`,
    default: field.default,
    schema: {
      type: field.type,
      control: field.control,
      collection: 'none',
      sensitivity: 'none',
      min: field.min,
      max: field.max,
      choices: field.choices,
    },
    effect: 'runtime',
    requires_confirmation: field.path === 'server.address',
    read_only_reason:
      field.path === 'server.address'
        ? 'Connection changes require confirmation and are not available yet.'
        : undefined,
  }));
  const state: SettingsState = {
    revision: 'revision-1',
    fields,
    values: fields.map((field) => ({
      path: field.path,
      value: field.default,
      editable: field.path !== 'server.address',
      source: { kind: 'default', name: '', locked: false, web_selected: false, reason: '' },
      reason: field.read_only_reason,
      secret_present: false,
    })),
  };
  const suggestions = state.values.find((value) => value.path === 'app.show_suggestions')!;
  suggestions.value = true;
  Object.assign(suggestions.source, { kind: 'web', web_selected: true });
  await page.route('**/api/settings', (route) => route.fulfill({ json: state }));
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({ json: { revision: state.revision, values: state.values } }),
  );
  return { state, publicConfig };
}

test('authorized navigation edits typed App fields and saves one draft across tabs', async ({
  page,
}) => {
  const { state, publicConfig } = await settingsAPI(page);
  const saves: unknown[] = [];
  await page.route('**/api/settings/apply', async (route) => {
    const draft = route.request().postDataJSON();
    saves.push(draft);
    for (const edit of draft.edits) {
      const field = state.values.find((field) => field.path === edit.path)!;
      field.value = edit.value;
      field.source.kind = 'web';
      field.source.web_selected = true;
    }
    state.revision = 'revision-2';
    Object.assign(publicConfig, { title: 'My archive' });
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/help');
  await page.getByRole('button', { name: 'Open navigation menu' }).click();
  await page.getByRole('menuitem', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('My archive');
  await page.getByRole('spinbutton', { name: 'Snippet length', exact: true }).fill('420');
  await page.getByRole('combobox', { name: 'Color scheme', exact: true }).selectOption('dark');
  await page.getByRole('switch', { name: 'Show suggestions', exact: true }).uncheck();
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('My archive');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saves).toEqual([
    {
      revision: 'revision-1',
      edits: [
        { path: 'app.title', action: 'set', value: 'My archive' },
        { path: 'app.snippet_length', action: 'set', value: 420 },
        { path: 'app.color_scheme', action: 'set', value: 'dark' },
        { path: 'app.show_suggestions', action: 'set', value: false },
      ],
    },
  ]);
  await expect(page.getByRole('banner')).toContainText('My archive');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});

test('nonadministrators cannot navigate to private settings or enter a login loop', async ({
  page,
}) => {
  await settingsAPI(page, { canConfigure: false, authMode: 'user', authenticated: true });
  let privateReads = 0;
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/settings') privateReads++;
  });
  await page.goto('/settings');
  await expect(page.getByRole('alert')).toContainText('You do not have permission');
  await page.getByRole('button', { name: 'Open navigation menu' }).click();
  await expect(page.getByRole('menuitem', { name: 'Settings', exact: true })).toHaveCount(0);
  expect(privateReads).toBe(0);
  await expect(page).toHaveURL(/\/settings$/);
});

test('source labels distinguish commented defaults, explicit locks and overridden web selections', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  const subtitle = state.values.find((value) => value.path === 'app.subtitle')!;
  Object.assign(subtitle, {
    value: 'Managed title',
    editable: false,
    reason: 'Set by environment: HISTER__APP__SUBTITLE',
  });
  subtitle.source = {
    kind: 'environment',
    name: 'HISTER__APP__SUBTITLE',
    locked: true,
    web_selected: true,
    reason: subtitle.reason!,
  };
  await page.goto('/settings');
  const region = page.getByRole('region', { name: 'Subtitle', exact: true });
  await expect(region).toContainText('Environment');
  await expect(region).toContainText('HISTER__APP__SUBTITLE');
  await expect(region).toContainText('overridden');
  await expect(region.getByRole('textbox')).toBeDisabled();
  await expect(region.getByRole('button')).toHaveCount(0);
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  const address = page.getByRole('region', { name: 'Listen address', exact: true });
  await expect(address).toContainText('Built in default');
  await expect(address.getByText('Locked', { exact: true })).toHaveCount(0);
  await expect(address.getByRole('textbox')).toHaveValue('127.0.0.1:4433');
  await expect(address.getByRole('textbox')).toBeDisabled();
  const addressValue = state.values.find((value) => value.path === 'server.address')!;
  addressValue.source = {
    kind: 'file',
    name: '/etc/hister/config.yml',
    locked: true,
    web_selected: false,
    reason: 'Set by file: /etc/hister/config.yml',
  };
  addressValue.reason = addressValue.source.reason;
  await page.reload();
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await expect(address).toContainText('Locked');
  await expect(address).toContainText('Main configuration');
});

test('reset removes a web selection while clear stores an explicit empty string', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  const title = state.values.find((value) => value.path === 'app.title')!;
  title.value = 'Saved title';
  Object.assign(title.source, { kind: 'web', web_selected: true });
  let saved: unknown;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Reset Title', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
  await page.getByRole('button', { name: 'Clear Subtitle', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saved).toEqual({
    revision: 'revision-1',
    edits: [
      { path: 'app.title', action: 'reset' },
      { path: 'app.subtitle', action: 'set', value: '' },
    ],
  });
});

test('validation errors identify their tab and preserve the complete draft', async ({ page }) => {
  await settingsAPI(page);
  let applied = false;
  await page.route('**/api/settings/apply', (route) => {
    applied = true;
    return route.fulfill({ json: {} });
  });
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      status: 422,
      json: {
        code: 'invalid_settings',
        errors: [
          { path: 'app.snippet_length', code: 'invalid_value', message: 'Must be at least 1.' },
        ],
      },
    }),
  );
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Keep this draft');
  await page.getByRole('spinbutton', { name: 'Snippet length', exact: true }).fill('0');
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'App (1 error)', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'App (1 error)', exact: true }).click();
  await expect(
    page.getByRole('spinbutton', { name: 'Snippet length', exact: true }),
  ).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByText('Must be at least 1.', { exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Keep this draft',
  );
  expect(applied).toBe(false);
});

test('stale revisions retain the draft and require an explicit reload', async ({ page }) => {
  const { state } = await settingsAPI(page);
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      status: 409,
      json: { code: 'stale_revision', error: 'Read current settings.' },
    }),
  );
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Keep this draft');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('changed');
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Keep this draft',
  );
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  state.values[0].value = 'Another administrator';
  state.revision = 'revision-2';
  const dismissed = page.waitForEvent('dialog').then((dialog) => dialog.dismiss());
  await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
  await dismissed;
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Keep this draft',
  );
  const accepted = page.waitForEvent('dialog').then((dialog) => dialog.accept());
  await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
  await accepted;
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Another administrator',
  );
});

test('a lost save response rereads active state without replaying the save', async ({ page }) => {
  const { state } = await settingsAPI(page);
  let saves = 0;
  let readsAfterSave = 0;
  await page.route('**/api/settings', (route) => {
    if (saves) readsAfterSave++;
    return route.fulfill({ json: state });
  });
  await page.route('**/api/settings/apply', (route) => {
    saves++;
    state.revision = 'revision-2';
    state.values[0].value = 'Saved despite disconnect';
    return route.abort('failed');
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Saved despite disconnect');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('response was lost');
  await expect.poll(() => readsAfterSave).toBeGreaterThan(0);
  expect(saves).toBe(1);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Saved despite disconnect',
  );
});

test('temporary service interruptions recover after save and refresh public configuration', async ({
  page,
}) => {
  const { state, publicConfig } = await settingsAPI(page);
  let saved = false;
  let interruptions = 0;
  let configInterruptions = 0;
  await page.route('**/api/settings', (route) => {
    if (saved && interruptions++ < 2) return route.fulfill({ status: 503, body: 'Reloading' });
    return route.fulfill({ json: state });
  });
  await page.route('**/api/config', (route) => {
    if (saved && configInterruptions++ < 1)
      return route.fulfill({ status: 503, body: 'Reloading' });
    return route.fallback();
  });
  await page.route('**/api/settings/apply', (route) => {
    saved = true;
    Object.assign(publicConfig, { title: 'Reconnected' });
    return route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Reconnected');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  await expect(page.getByRole('banner')).toContainText('Reconnected');
});

test('keyboard tabs preserve drafts and navigation warns about unsaved changes', async ({
  page,
}) => {
  await settingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Unsaved');
  await page.getByRole('tab', { name: 'App', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toBeFocused();
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Server');
  await page.keyboard.press('Home');
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Unsaved');
  const dismissed = page.waitForEvent('dialog').then((dialog) => dialog.dismiss());
  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await dismissed;
  await expect(page).toHaveURL(/\/settings$/);
  const accepted = page.waitForEvent('dialog').then((dialog) => dialog.accept());
  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await accepted;
  await expect(page).toHaveURL(/\/rules$/);
});

for (const code of ['persistence_failed', 'apply_failed', 'recovery_failed']) {
  test(`${code} is reported without losing the draft or claiming success`, async ({ page }) => {
    await settingsAPI(page);
    await page.route('**/api/settings/apply', (route) =>
      route.fulfill({ status: 500, json: { code, error: `Server reported ${code}.` } }),
    );
    await page.goto('/settings');
    await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Keep after failure');
    await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText(code);
    await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
      'Keep after failure',
    );
    await expect(page.getByRole('status')).not.toContainText('saved');
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  });
}

test('server denial after capability discovery is handled locally', async ({ page }) => {
  await settingsAPI(page, { authMode: 'user', authenticated: true });
  await page.route('**/api/settings', (route) => route.fulfill({ status: 403 }));
  await page.goto('/settings');
  await expect(page.getByRole('alert')).toContainText('You do not have permission');
  await expect(page.getByRole('button', { name: 'Save and apply' })).toHaveCount(0);
  await expect(page).toHaveURL(/\/settings$/);
});

test('session requests carry CSRF protection and drafts stay out of browser storage', async ({
  page,
}) => {
  await settingsAPI(page, { authMode: 'token', authenticated: true });
  await page.route('**/api/config', async (route) => {
    await route.fulfill({
      headers: { 'X-CSRF-Token': 'csrf-current' },
      json: {
        authMode: 'token',
        authenticated: true,
        canConfigure: true,
        colorScheme: 'automatic',
        title: 'Hister',
      },
    });
  });
  let csrf: string | undefined;
  await page.route('**/api/settings/validate', (route) => {
    csrf = route.request().headers()['x-csrf-token'];
    return route.fulfill({
      status: 422,
      json: { errors: [{ path: 'app.title', message: 'Keep the draft.' }] },
    });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Private unsaved title');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('highlighted');
  expect(csrf).toBe('csrf-current');
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'Private unsaved title',
  );
});

test('all categories remain accessible on narrow screens and secrets remain hidden', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  for (const category of [
    'indexer',
    'crawler',
    'semantic_search',
    'extractors',
    'hotkeys',
    'sensitive_content_patterns',
  ]) {
    const path = `${category}.private`;
    state.fields.push({
      path,
      category,
      label: 'Private option',
      description: 'Managed in the main configuration.',
      schema: { type: 'unknown', control: 'none', collection: 'unknown', sensitivity: 'unknown' },
      effect: 'unknown',
      requires_confirmation: false,
      read_only_reason: 'Manage in the main configuration.',
    });
    state.values.push({
      path,
      editable: false,
      secret_present: true,
      source: { kind: 'file', locked: true, web_selected: false },
    });
  }
  await page.goto('/settings');
  await page
    .getByRole('textbox', { name: 'Title', exact: true })
    .fill('Draft across all categories');
  await page.getByRole('tab', { name: 'App', exact: true }).focus();
  await page.keyboard.press('End');
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Sensitive content patterns');
  await expect(page.getByText('Value stored (hidden)', { exact: true })).toBeVisible();
  await expect(page.getByRole('tabpanel').getByRole('textbox')).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.keyboard.press('Home');
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Draft across all categories',
  );
});

test('leaving an unsaved draft through the home link warns only once', async ({ page }) => {
  await settingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Unsaved title');
  let warnings = 0;
  page.on('dialog', async (dialog) => {
    warnings++;
    await dialog.accept();
  });
  await page.getByRole('link', { name: 'Hister logo Hister', exact: true }).click();
  await expect(page).toHaveURL('http://127.0.0.1:4174/');
  expect(warnings).toBe(1);
});

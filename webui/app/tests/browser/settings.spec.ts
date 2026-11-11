// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type Page } from '@playwright/test';
import { mockAPI } from './fixtures';
import type { SettingsState } from '../../src/lib/settings';

async function settingsAPI(page: Page, config: Record<string, unknown> = {}) {
  const publicConfig = { canConfigure: true, ...config };
  await mockAPI(page, { config: publicConfig });
  const fields = [
    {
      path: 'app.title',
      group: 'Appearance',
      label: 'Title',
      type: 'string',
      control: 'text',
      default: 'Hister',
    },
    {
      path: 'app.subtitle',
      group: 'Appearance',
      label: 'Subtitle',
      type: 'string',
      control: 'text',
      default: '',
    },
    {
      path: 'app.snippet_length',
      group: 'Search behavior',
      label: 'Snippet length',
      type: 'integer',
      control: 'number',
      default: 300,
      min: 1,
      max: 10000,
    },
    {
      path: 'app.color_scheme',
      group: 'Appearance',
      label: 'Color scheme',
      type: 'string',
      control: 'select',
      default: 'automatic',
      choices: ['automatic', 'light', 'dark'],
    },
    {
      path: 'app.show_suggestions',
      group: 'Search behavior',
      label: 'Show suggestions',
      type: 'boolean',
      control: 'switch',
      default: false,
    },
    {
      path: 'server.address',
      group: 'Connection',
      label: 'Listen address',
      type: 'string',
      control: 'text',
      default: '127.0.0.1:4433',
    },
  ].map((field) => ({
    path: field.path,
    category: field.path.split('.')[0],
    group: field.group,
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

async function hotkeySettingsAPI(page: Page) {
  const { state, publicConfig } = await settingsAPI(page);
  state.fields.push({
    path: 'hotkeys.web',
    category: 'hotkeys',
    label: 'Web keyboard shortcuts',
    description: 'Shortcuts apply on the search page after saving. Use lowercase keys.',
    schema: {
      type: 'dictionary',
      control: 'dictionary',
      collection: 'replace',
      sensitivity: 'none',
      key_label: 'Key combination',
      value_label: 'Action',
      max_items: 256,
      items: {
        type: 'string',
        control: 'select',
        collection: 'none',
        sensitivity: 'none',
        choices: ['focus_search_input', 'show_hotkeys'],
        choice_labels: {
          focus_search_input: 'Focus search input',
          show_hotkeys: 'Show keyboard shortcuts',
        },
      },
    },
    default: { '/': 'focus_search_input', '?': 'show_hotkeys' },
    effect: 'runtime',
    requires_confirmation: false,
  });
  state.values.push({
    path: 'hotkeys.web',
    value: { 'alt+x': 'focus_search_input' },
    editable: true,
    source: { kind: 'web', locked: false, web_selected: true },
    secret_present: false,
  });
  return { state, publicConfig };
}

test('web hotkeys use named actions and save a shared draft with working shortcuts', async ({
  page,
}) => {
  const { state, publicConfig } = await hotkeySettingsAPI(page);
  let saved: any;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    const hotkeys = saved.edits.find((edit: any) => edit.path === 'hotkeys.web').value;
    state.values.find((value) => value.path === 'hotkeys.web')!.value = hotkeys;
    Object.assign(publicConfig, { hotkeys });
    state.revision = 'revision-2';
    await route.fulfill({ json: {} });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Shortcuts instance');
  await page.getByRole('tab', { name: 'Hotkeys', exact: true }).click();
  const shortcuts = page.getByRole('region', { name: 'Web keyboard shortcuts', exact: true });
  await expect(shortcuts.getByText('Focus search input', { exact: true })).toBeVisible();
  await shortcuts.getByRole('button', { name: 'Add shortcut', exact: true }).click();
  const editor = shortcuts.getByRole('group', { name: 'Web keyboard shortcuts 2', exact: true });
  await expect(editor.getByRole('textbox', { name: 'Key combination', exact: true })).toBeFocused();
  await editor.getByRole('textbox', { name: 'Key combination', exact: true }).fill('alt+h');
  await editor.getByRole('combobox', { name: 'Action', exact: true }).selectOption('show_hotkeys');
  await editor.getByRole('button', { name: 'Add shortcut', exact: true }).click();
  await expect(shortcuts.getByText('Show keyboard shortcuts', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saved.edits).toEqual(
    expect.arrayContaining([
      { path: 'app.title', action: 'set', value: 'Shortcuts instance' },
      {
        path: 'hotkeys.web',
        action: 'set',
        value: { 'alt+x': 'focus_search_input', 'alt+h': 'show_hotkeys' },
      },
    ]),
  );
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.goto('/');
  await expect(page.getByRole('searchbox', { name: 'Search your history' })).toBeFocused();
  await page.getByRole('heading', { name: 'Hister', exact: true }).click();
  await page.keyboard.press('Alt+h');
  await expect(page.getByRole('dialog')).toContainText('Keyboard');
});

test('web hotkeys retain invalid rows and distinguish clear, reset and source locks', async ({
  page,
}) => {
  const { state } = await hotkeySettingsAPI(page);
  let draft: any;
  await page.route('**/api/settings/apply', (route) =>
    route.fulfill({
      status: 422,
      json: { code: 'invalid_draft', message: 'Save failed; draft retained.' },
    }),
  );
  await page.route('**/api/settings/validate', async (route) => {
    draft = route.request().postDataJSON();
    const edit = draft.edits.find((edit: any) => edit.path === 'hotkeys.web');
    if (edit?.value?.['shift+x']) {
      await route.fulfill({
        status: 422,
        json: {
          code: 'invalid_draft',
          error: 'Invalid draft',
          errors: [
            {
              path: 'hotkeys.web',
              code: 'invalid_value',
              message: 'Use supported lowercase keys.',
            },
          ],
        },
      });
    } else await route.fulfill({ json: { revision: state.revision, values: state.values } });
  });
  await page.goto('/settings?tab=hotkeys');
  const shortcuts = page.getByRole('region', { name: 'Web keyboard shortcuts', exact: true });
  await shortcuts.getByRole('button', { name: 'Add shortcut', exact: true }).click();
  const editor = shortcuts.getByRole('group', { name: 'Web keyboard shortcuts 2', exact: true });
  const key = editor.getByRole('textbox', { name: 'Key combination', exact: true });
  await key.fill('shift+x');
  await editor.getByRole('combobox', { name: 'Action', exact: true }).selectOption('show_hotkeys');
  await editor.getByRole('button', { name: 'Add shortcut', exact: true }).click();
  await expect(shortcuts.getByText('Use supported lowercase keys.')).toBeVisible();
  await expect(key).toHaveValue('shift+x');
  await key.fill('alt+x');
  await editor.getByRole('button', { name: 'Add shortcut', exact: true }).click();
  await expect(editor.getByRole('alert')).toContainText('Names must be unique');
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
  await shortcuts
    .getByRole('button', { name: 'Clear Web keyboard shortcuts', exact: true })
    .click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect.poll(() => draft.edits).toEqual([{ path: 'hotkeys.web', action: 'set', value: {} }]);
  await shortcuts
    .getByRole('button', { name: 'Reset Web keyboard shortcuts', exact: true })
    .click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect.poll(() => draft.edits).toEqual([{ path: 'hotkeys.web', action: 'reset' }]);
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  Object.assign(
    state.values.find((value) => value.path === 'hotkeys.web')!,
    {
      editable: false,
      reason: 'Controlled by the main configuration.',
      source: { kind: 'file', name: '/config.yml', locked: true, web_selected: true },
    },
  );
  state.revision = 'revision-locked';
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(shortcuts.getByRole('button', { name: 'Add shortcut', exact: true })).toHaveCount(0);
  await expect(
    shortcuts.getByRole('button', { name: 'Clear Web keyboard shortcuts', exact: true }),
  ).toHaveCount(0);
  await shortcuts
    .getByRole('button', {
      name: 'Locked: Source information for Web keyboard shortcuts',
      exact: true,
    })
    .click();
  await expect(page.getByRole('tooltip')).toContainText('main configuration');
});

async function extractionSettingsAPI(page: Page) {
  const { state } = await settingsAPI(page);
  const ordinary = { collection: 'none', sensitivity: 'none' };
  const secret = { type: 'string', control: 'secret', collection: 'none', sensitivity: 'secret' };
  const definitions = [
    {
      path: 'extractors.ytdlp.enable',
      label: 'Enable Ytdlp',
      schema: { ...ordinary, type: 'boolean', control: 'switch' },
      value: false,
    },
    {
      path: 'extractors.ytdlp.options.timeout',
      label: 'Timeout (seconds)',
      schema: { ...ordinary, type: 'integer', control: 'number', min: 1 },
      value: 15,
    },
    {
      path: 'extractors.ytdlp.options.cookies_file',
      label: 'Cookies file',
      schema: secret,
      value: { action: 'clear' },
    },
    {
      path: 'extractors.ytdlp.options.binary',
      label: 'Executable',
      schema: secret,
      value: undefined,
      readOnly: 'Executable selections are file managed.',
    },
    {
      path: 'semantic_search.enable',
      label: 'Semantic search',
      schema: { ...ordinary, type: 'boolean', control: 'switch' },
      value: false,
      effect: 'reembed',
    },
    {
      path: 'semantic_search.embedding_endpoint',
      label: 'Embedding endpoint',
      schema: { ...ordinary, type: 'string', control: 'url', min_length: 1 },
      value: 'http://localhost:11434/v1/embeddings',
      effect: 'reembed',
    },
    {
      path: 'semantic_search.embedding_model',
      label: 'Embedding model',
      schema: { ...ordinary, type: 'string', control: 'text' },
      value: 'default-model',
      effect: 'reembed',
    },
    {
      path: 'semantic_search.dimensions',
      label: 'Embedding dimensions',
      schema: { ...ordinary, type: 'integer', control: 'number', min: 1 },
      value: 2000,
      effect: 'reembed',
    },
    {
      path: 'semantic_search.semantic_weight',
      label: 'Semantic weight',
      schema: { ...ordinary, type: 'number', control: 'number', min: 0, max: 1 },
      value: 0.4,
    },
    {
      path: 'semantic_search.api_key',
      label: 'Embedding API key',
      schema: secret,
      value: { action: 'keep', ref: 'api-key-ref' },
    },
    {
      path: 'semantic_search.headers',
      label: 'Embedding headers',
      schema: {
        type: 'dictionary',
        control: 'dictionary',
        sensitivity: 'mixed',
        collection: 'replace',
        items: secret,
      },
      value: { 'x-token': { action: 'keep', ref: 'header-ref' } },
    },
  ];
  for (const entry of definitions) {
    state.fields.push({
      path: entry.path,
      label: entry.label,
      category: entry.path.split('.')[0],
      description: `${entry.label} description.`,
      default: entry.schema.sensitivity === 'none' ? entry.value : undefined,
      schema: entry.schema,
      effect: entry.effect ?? 'runtime',
      requires_confirmation: false,
      read_only_reason: entry.readOnly,
    });
    state.values.push({
      path: entry.path,
      value: entry.value,
      editable: !entry.readOnly,
      reason: entry.readOnly,
      source: { kind: 'default', locked: false, web_selected: false },
      secret_present:
        !!entry.readOnly ||
        entry.path === 'semantic_search.api_key' ||
        entry.path === 'semantic_search.headers',
    });
  }
  return state;
}

test('embedding endpoint is visible and saves and resets as an ordinary URL', async ({ page }) => {
  const state = await extractionSettingsAPI(page);
  const value = state.values.find((entry) => entry.path === 'semantic_search.embedding_endpoint')!;
  value.value = 'https://embeddings.example/v1/embeddings';
  value.source = { kind: 'web', locked: false, web_selected: true };
  const drafts: any[] = [];
  await page.route('**/api/settings/apply', async (route) => {
    const draft = route.request().postDataJSON();
    drafts.push(draft);
    const edit = draft.edits.find((entry: any) => entry.path === value.path);
    value.value = edit.action === 'reset' ? 'http://localhost:11434/v1/embeddings' : edit.value;
    value.source.web_selected = edit.action !== 'reset';
    state.revision += '-saved';
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=semantic_search');
  const field = page.getByRole('region', { name: 'Embedding endpoint', exact: true });
  const input = field.locator('input[type="url"]');
  await expect(input).toHaveValue('https://embeddings.example/v1/embeddings');
  await expect(field.getByRole('combobox')).toHaveCount(0);
  await expect(field.locator('input[type="password"]')).toHaveCount(0);
  await input.fill('https://new.example/embeddings');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
  expect(drafts[0].edits).toEqual([
    { path: value.path, action: 'set', value: 'https://new.example/embeddings' },
  ]);
  await expect(input).toHaveValue('https://new.example/embeddings');
  await page.getByRole('button', { name: 'Reset Embedding endpoint', exact: true }).click();
  await expect(input).toHaveValue('http://localhost:11434/v1/embeddings');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
  expect(drafts[1].edits).toEqual([{ path: value.path, action: 'reset' }]);
});

test('extractor and semantic controls share one draft with secrets and reindex guidance', async ({
  page,
}) => {
  await extractionSettingsAPI(page);
  let draft: any;
  await page.route('**/api/settings/apply', async (route) => {
    draft = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=extractors');
  await expect(page.getByRole('switch', { name: 'Enable Ytdlp', exact: true })).not.toBeChecked();
  await expect(
    page.getByRole('spinbutton', { name: 'Timeout (seconds)', exact: true }),
  ).toHaveValue('15');
  await expect(
    page
      .getByRole('region', { name: 'Executable', exact: true })
      .getByText('Read only', { exact: true }),
  ).toBeVisible();
  await page.getByRole('spinbutton', { name: 'Timeout (seconds)', exact: true }).fill('10');
  await page
    .getByRole('region', { name: 'Cookies file', exact: true })
    .locator('input')
    .fill('private-cookies-path');
  await page.getByRole('tab', { name: 'Semantic search', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Embedding model', exact: true })).toContainText(
    'Reindex existing documents after changing this setting.',
  );
  await expect(
    page.getByText('Saving does not start reindexing or test the embedding endpoint.'),
  ).toBeVisible();
  await page.getByRole('textbox', { name: 'Embedding model', exact: true }).fill('small-model');
  await page.getByRole('spinbutton', { name: 'Embedding dimensions', exact: true }).fill('128');
  await page.getByRole('spinbutton', { name: 'Semantic weight', exact: true }).fill('0.65');
  await page
    .getByRole('combobox', { name: 'Embedding API key action', exact: true })
    .selectOption('replace');
  await page.getByLabel('Embedding API key replacement', { exact: true }).fill('private-api-key');
  await page.getByRole('button', { name: 'Add header', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('x-extra');
  await page.getByLabel('Value', { exact: true }).fill('private-header');
  await page.getByRole('button', { name: 'Add header', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
  expect(draft.edits).toEqual(
    expect.arrayContaining([
      { path: 'extractors.ytdlp.options.timeout', action: 'set', value: 10 },
      {
        path: 'extractors.ytdlp.options.cookies_file',
        action: 'set',
        value: { action: 'replace', value: 'private-cookies-path' },
      },
      { path: 'semantic_search.embedding_model', action: 'set', value: 'small-model' },
      { path: 'semantic_search.dimensions', action: 'set', value: 128 },
      { path: 'semantic_search.semantic_weight', action: 'set', value: 0.65 },
      {
        path: 'semantic_search.headers',
        action: 'set',
        value: {
          'x-token': { action: 'keep', ref: 'header-ref' },
          'x-extra': { action: 'replace', value: 'private-header' },
        },
      },
    ]),
  );
  expect(draft.edits).toHaveLength(7);
  expect(
    await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage })),
  ).not.toContain('private-');
});

test('semantic validation retains drafts and respects locked extractor options', async ({
  page,
}) => {
  const state = await extractionSettingsAPI(page);
  const timeout = state.values.find((field) => field.path === 'extractors.ytdlp.options.timeout')!;
  timeout.editable = false;
  timeout.source = {
    kind: 'environment',
    name: 'HISTER__EXTRACTORS__YTDLP__OPTIONS__TIMEOUT',
    locked: true,
    web_selected: true,
  };
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      status: 422,
      json: {
        errors: [
          {
            path: 'semantic_search.dimensions',
            code: 'invalid_value',
            message: 'Use positive embedding dimensions.',
          },
        ],
      },
    }),
  );
  await page.goto('/settings?tab=extractors');
  await expect(
    page.getByRole('spinbutton', { name: 'Timeout (seconds)', exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Enable Ytdlp', exact: true })).toBeEnabled();
  await page.getByRole('tab', { name: 'Semantic search', exact: true }).click();
  await page.getByRole('spinbutton', { name: 'Embedding dimensions', exact: true }).fill('0');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByText('Use positive embedding dimensions.')).toBeVisible();
  await expect(
    page.getByRole('spinbutton', { name: 'Embedding dimensions', exact: true }),
  ).toHaveValue('0');
});

test('category badges count unsaved fields and clear after restoring, discarding or saving', async ({
  page,
  isMobile,
}) => {
  await crawlerSettingsAPI(page);
  await page.route('**/api/settings/apply', (route) => route.fulfill({ json: { saved: true } }));
  await page.goto('/settings');
  const app = page.getByRole('tab', { name: 'App', exact: true });
  const appBadge = page.getByRole('button', { name: 'Unsaved items in App', exact: true });
  const crawler = page.getByRole('tab', { name: 'Crawler', exact: true });
  const crawlerBadge = page.getByRole('button', { name: 'Unsaved items in Crawler', exact: true });
  const server = page.getByRole('tab', { name: 'Server', exact: true });
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  await expect(app).toHaveText('App');
  await expect(appBadge).toHaveCount(0);
  await expect(crawler).toHaveText('Crawler');
  await expect(crawlerBadge).toHaveCount(0);
  await title.fill('Draft title');
  await title.fill('Revised draft title');
  await expect(appBadge.getByText('1', { exact: true })).toBeVisible();
  await page.getByRole('spinbutton', { name: 'Snippet length', exact: true }).fill('420');
  await expect(appBadge.getByText('2', { exact: true })).toBeVisible();
  if (!isMobile) {
    await appBadge.getByText('2', { exact: true }).hover();
    await expect(page.getByRole('tooltip')).toHaveText('Number of unsaved items: 2');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).not.toBeVisible();
  }
  await app.focus();
  await expect(app).toHaveAccessibleDescription('Number of unsaved items: 2');
  await expect(page.getByRole('tooltip')).not.toBeVisible();
  await crawler.click();
  await page.getByRole('spinbutton', { name: 'Crawler timeout', exact: true }).fill('10');
  await expect(appBadge.getByText('2', { exact: true })).toBeVisible();
  await expect(crawlerBadge.getByText('1', { exact: true })).toBeVisible();
  await expect(server).toHaveText('Server');
  await app.click();
  await title.fill('Hister');
  await expect(appBadge.getByText('1', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Undo Snippet length', exact: true }).click();
  await expect(app).toHaveText('App');
  await expect(appBadge).toHaveCount(0);
  await page.getByRole('button', { name: 'Reset Show suggestions', exact: true }).click();
  await expect(appBadge.getByText('1', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(app).toHaveText('App');
  await expect(appBadge).toHaveCount(0);
  await expect(crawler).toHaveText('Crawler');
  await expect(crawlerBadge).toHaveCount(0);
  await title.fill('Saved title');
  await expect(appBadge.getByText('1', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
  await expect(app).toHaveText('App');
  await expect(appBadge).toHaveCount(0);
});

test('unsaved count tooltips open from badges without opening from category tabs', async ({
  page,
  isMobile,
}) => {
  await settingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Draft title');
  const app = page.getByRole('tab', { name: 'App', exact: true });
  const tooltip = page.getByRole('tooltip');
  await app.focus();
  await expect(app).toBeFocused();
  await expect(tooltip).not.toBeVisible();
  await expect(app).toHaveAccessibleDescription('Number of unsaved items: 1');
  if (!isMobile) {
    await app.hover({ position: { x: 8, y: 8 } });
    await expect(tooltip).not.toBeVisible();
    await page.mouse.move(0, 0);
  }
  const badge = page.getByRole('button', { name: 'Unsaved items in App', exact: true });
  await page.keyboard.press('Tab');
  await expect(badge).toBeFocused();
  await expect(tooltip).toHaveText('Number of unsaved items: 1');
  await page.keyboard.press('Escape');
  await expect(tooltip).not.toBeVisible();
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).focus();
  if (!isMobile) {
    await badge.hover();
    await expect(tooltip).toHaveText('Number of unsaved items: 1');
    await page.mouse.move(0, 0);
    await expect(tooltip).not.toBeVisible();
  }
  await app.focus();
  await page.keyboard.press(isMobile ? 'ArrowRight' : 'ArrowDown');
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toBeFocused();
  await page.keyboard.press(isMobile ? 'ArrowLeft' : 'ArrowUp');
  await expect(app).toBeFocused();
  await expect(tooltip).not.toBeVisible();
});

test('settings tab selection is restored after a reload', async ({ page }) => {
  await settingsAPI(page);
  await page.goto('/settings?source=bookmark#preferences');
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await expect(page).toHaveURL(
    'http://127.0.0.1:4174/settings?source=bookmark&tab=server#preferences',
  );
  await page.reload();
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Server');
});

test('settings tab URLs open directly and unknown tabs fall back to App', async ({ page }) => {
  await settingsAPI(page);
  for (const [tab, label] of [
    ['server', 'Server'],
    ['app', 'App'],
    ['missing-category', 'App'],
  ]) {
    await page.goto(`/settings?tab=${tab}`);
    await expect(page.getByRole('tab', { name: label, exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    await expect(page.getByRole('tabpanel')).toHaveAccessibleName(label);
  }
});

test('settings tab history preserves the draft without a navigation warning', async ({ page }) => {
  await settingsAPI(page);
  const warnings: string[] = [];
  page.on('dialog', async (dialog) => {
    warnings.push(dialog.message());
    await dialog.dismiss();
  });
  let reads = 0;
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/settings') reads++;
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Unsaved draft');
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await expect(page).toHaveURL(/\/settings\?tab=server$/);
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await page.goBack();
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Server');
  await page.goBack();
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('App');
  await page.goForward();
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Server');
  await page.goForward();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Unsaved draft',
  );
  await expect(page.getByText('1 unsaved change', { exact: true })).toBeVisible();
  expect(warnings).toEqual([]);
  expect(reads).toBe(1);
});

test('authorized navigation edits typed App fields and saves one draft across tabs', async ({
  page,
}) => {
  const { state, publicConfig } = await settingsAPI(page);
  const saves: { revision: string; edits: unknown[] }[] = [];
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
      edits: expect.arrayContaining([
        { path: 'app.title', action: 'set', value: 'My archive' },
        { path: 'app.snippet_length', action: 'set', value: 420 },
        { path: 'app.color_scheme', action: 'set', value: 'dark' },
        { path: 'app.show_suggestions', action: 'set', value: false },
      ]),
    },
  ]);
  expect(saves[0].edits).toHaveLength(4);
  await expect(page.getByRole('banner')).toContainText('My archive');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});

test('settings rows align controls on desktop and stack them on mobile', async ({
  page,
  isMobile,
}) => {
  const { state } = await settingsAPI(page);
  const suggestions = state.fields.find((field) => field.path === 'app.show_suggestions')!;
  state.fields.push({
    ...suggestions,
    path: 'app.show_history',
    label: 'Show history',
    description: 'Show history description.',
  });
  state.values.push({ ...state.values[0], path: 'app.show_history', value: false });
  await page.goto('/settings');

  const fields = [
    ['Title', 'textbox'],
    ['Subtitle', 'textbox'],
    ['Color scheme', 'combobox'],
    ['Snippet length', 'spinbutton'],
    ['Show suggestions', 'switch'],
    ['Show history', 'switch'],
  ] as const;
  const boxes = [];
  for (const [name, role] of fields) {
    const control = page.getByRole(role, { name, exact: true });
    await control.scrollIntoViewIfNeeded();
    const row = page.getByRole('region', { name, exact: true });
    const labelBox = (await row.getByText(name, { exact: true }).boundingBox())!;
    const descriptionBox = (await row
      .getByText(`${name} description.`, { exact: true })
      .boundingBox())!;
    const controlBox = (await control.boundingBox())!;
    if (isMobile) {
      expect(controlBox.y).toBeGreaterThanOrEqual(descriptionBox.y + descriptionBox.height);
      expect(controlBox.x).toBeCloseTo(labelBox.x, 0);
    } else {
      expect(controlBox.x).toBeGreaterThan(descriptionBox.x + descriptionBox.width);
      expect(Math.abs(controlBox.y - labelBox.y)).toBeLessThan(16);
    }
    boxes.push(controlBox);
  }
  expect(boxes[0].width).toBeCloseTo(boxes[1].width, 0);
  expect(boxes[0].width).toBeCloseTo(boxes[2].width, 0);
  if (!isMobile) {
    for (const box of boxes) {
      expect(box.x + box.width).toBeCloseTo(boxes[0].x + boxes[0].width, 0);
    }
  }
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  await title.fill('Aligned draft');
  const edited = (await title.boundingBox())!;
  expect(edited.x).toBeCloseTo(boxes[0].x, 0);
  expect(edited.width).toBeCloseTo(boxes[0].width, 0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('fixed settings show readable values without editing controls and keep secrets hidden', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  for (const value of state.values) value.editable = false;
  state.values.find((value) => value.path === 'app.subtitle')!.source.locked = true;
  state.values.find((value) => value.path === 'app.snippet_length')!.value = 0;
  state.values.find((value) => value.path === 'app.show_suggestions')!.value = false;
  for (const [label, path, secretPresent] of [
    ['Access token', 'app.access_token', true],
    ['Optional token', 'app.optional_token', false],
  ] as const) {
    state.fields.push({
      ...state.fields[0],
      label,
      path,
      schema: { type: 'string', control: 'text', collection: 'none', sensitivity: 'secret' },
    });
    state.values.push({
      ...state.values[0],
      path,
      value: secretPresent ? 'never-render-this-secret' : undefined,
      secret_present: secretPresent,
    });
  }
  await page.goto('/settings');
  for (const [label, text] of [
    ['Title', 'Hister'],
    ['Subtitle', 'Empty'],
    ['Snippet length', '0'],
    ['Color scheme', 'automatic'],
    ['Show suggestions', 'Disabled'],
    ['Access token', 'Value stored (hidden)'],
    ['Optional token', 'No stored value'],
  ]) {
    const row = page.getByRole('region', { name: label, exact: true });
    await expect(row.getByText(text, { exact: true })).toBeVisible();
    await expect(row.getByRole('button', { name: /^(Clear|Reset|Undo) / })).toHaveCount(0);
  }
  for (const role of ['textbox', 'combobox', 'spinbutton', 'switch'] as const) {
    await expect(page.getByRole('tabpanel').getByRole(role)).toHaveCount(0);
  }
  await expect(page.getByRole('tabpanel')).not.toContainText('never-render-this-secret');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
});

test('long descriptions and reference values fit within settings rows', async ({
  page,
  isMobile,
}) => {
  const { state } = await settingsAPI(page);
  const value = `/srv/archive/${'long_directory_name_'.repeat(20)}`;
  const description = 'The directory containing the documents available to this Hister instance.';
  state.fields.push({
    ...state.fields[0],
    path: 'app.directory',
    label: 'Document storage directory',
    description,
    schema: { type: 'string', control: 'none', collection: 'none', sensitivity: 'none' },
  });
  state.values.push({ ...state.values[0], path: 'app.directory', value, editable: false });
  await page.setViewportSize({ width: isMobile ? 375 : 1024, height: 800 });
  await page.goto('/settings');
  const row = page.getByRole('region', { name: 'Document storage directory', exact: true });
  const reference = row.getByText(value, { exact: true });
  await reference.scrollIntoViewIfNeeded();
  const rowBox = (await row.boundingBox())!;
  for (const text of [reference, row.getByText(description, { exact: true })]) {
    expect(await text.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    const box = (await text.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(rowBox.x);
    expect(box.x + box.width).toBeLessThanOrEqual(rowBox.x + rowBox.width);
  }
});

test('the save bar counts edited fields and discards the draft from any tab without saving', async ({
  page,
  isMobile,
}) => {
  await settingsAPI(page);
  const requests: string[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/settings')) {
      requests.push(`${request.method()} ${new URL(request.url()).pathname}`);
    }
  });
  await page.goto('/settings');
  const discard = page.getByRole('button', { name: 'Discard changes', exact: true });
  const save = page.getByRole('button', { name: 'Save and apply', exact: true });
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  const snippet = page.getByRole('spinbutton', { name: 'Snippet length', exact: true });
  await expect(discard).toBeDisabled();
  await expect(page.getByText('No unsaved changes', { exact: true })).toBeVisible();
  await title.fill('Draft title');
  await title.fill('Revised draft title');
  await expect(page.getByText('1 unsaved change', { exact: true })).toBeVisible();
  const countBox = (await page.getByText('1 unsaved change', { exact: true }).boundingBox())!;
  const discardBox = (await discard.boundingBox())!;
  const saveBox = (await save.boundingBox())!;
  expect(discardBox.x + discardBox.width).toBeLessThanOrEqual(saveBox.x);
  if (isMobile) {
    expect(countBox.y + countBox.height).toBeLessThanOrEqual(discardBox.y);
  } else {
    expect(countBox.x + countBox.width).toBeLessThanOrEqual(discardBox.x);
  }
  await expect(discard).toBeInViewport();
  await expect(save).toBeInViewport();
  await snippet.fill('420');
  await page.getByRole('button', { name: 'Reset Show suggestions', exact: true }).click();
  await expect(page.getByText('3 unsaved changes', { exact: true })).toBeVisible();
  await expect(
    page
      .getByRole('region', { name: 'Show suggestions', exact: true })
      .getByText('Unsaved', { exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Undo Title', exact: true }).click();
  await expect(page.getByText('2 unsaved changes', { exact: true })).toBeVisible();
  await expect(
    page.getByRole('region', { name: 'Title', exact: true }).getByText('Unsaved', { exact: true }),
  ).toHaveCount(0);
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await discard.click();
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByText('No unsaved changes', { exact: true })).toBeVisible();
  await expect(discard).toBeDisabled();
  await expect(save).toBeDisabled();
  await expect(
    page.getByRole('group', { name: 'Settings actions' }).getByRole('status'),
  ).toHaveText('Unsaved changes discarded.');
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(title).toHaveValue('Hister');
  await expect(snippet).toHaveValue('300');
  await expect(page.getByRole('switch', { name: 'Show suggestions', exact: true })).toBeChecked();
  await expect(page.getByText('Unsaved', { exact: true })).toHaveCount(0);
  expect(requests).toEqual(['GET /api/settings']);
  page.on('dialog', (dialog) => dialog.dismiss());
  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await expect(page).toHaveURL(/\/rules$/);
});

test('typing the loaded value again removes the pending change and navigation warning', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  state.values[0].value = 'Saved archive';
  Object.assign(state.values[0].source, { kind: 'web', web_selected: true });
  await page.goto('/settings');
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  const unsaved = page
    .getByRole('region', { name: 'Title', exact: true })
    .getByText('Unsaved', { exact: true });
  await expect(unsaved).toHaveCount(0);
  await title.fill('Draft archive');
  await expect(page.getByText('1 unsaved change', { exact: true })).toBeVisible();
  await expect(unsaved).toBeVisible();
  await title.fill('Saved archive');
  await expect(page.getByText('No unsaved changes', { exact: true })).toBeVisible();
  await expect(unsaved).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Discard changes', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Undo Title', exact: true })).toHaveCount(0);
  await expect(title).not.toHaveAccessibleDescription(/Unsaved change/);
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(title).toHaveValue('Saved archive');
  page.on('dialog', (dialog) => dialog.dismiss());
  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await expect(page).toHaveURL(/\/rules$/);
});

test('restoring typed values leaves only unrelated edits and explicit resets in the draft', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  state.values.find((value) => value.path === 'app.show_suggestions')!.value = false;
  let saved: unknown;
  await page.route('**/api/settings/apply', (route) => {
    saved = route.request().postDataJSON();
    state.values[0].value = 'Keep this draft';
    return route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Keep this draft');
  const subtitle = page.getByRole('textbox', { name: 'Subtitle', exact: true });
  const snippet = page.getByRole('spinbutton', { name: 'Snippet length', exact: true });
  const color = page.getByRole('combobox', { name: 'Color scheme', exact: true });
  const suggestions = page.getByRole('switch', { name: 'Show suggestions', exact: true });
  for (const [label, edit, restore] of [
    ['Subtitle', () => subtitle.fill('Temporary subtitle'), () => subtitle.fill('')],
    ['Snippet length', () => snippet.fill('420'), () => snippet.fill('0300')],
    ['Color scheme', () => color.selectOption('dark'), () => color.selectOption('automatic')],
    ['Show suggestions', () => suggestions.check(), () => suggestions.uncheck()],
  ] as const) {
    await test.step(`Restore ${label}`, async () => {
      await edit();
      await expect(page.getByText('2 unsaved changes', { exact: true })).toBeVisible();
      await restore();
      await expect(page.getByText('1 unsaved change', { exact: true })).toBeVisible();
      await expect(page.getByRole('button', { name: `Undo ${label}`, exact: true })).toHaveCount(0);
    });
  }
  // A reset removes the stored selection even when its value already equals the default.
  await page.getByRole('button', { name: 'Reset Show suggestions', exact: true }).click();
  await expect(page.getByText('2 unsaved changes', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
  expect(saved).toEqual({
    revision: 'revision-1',
    edits: [
      { path: 'app.title', action: 'set', value: 'Keep this draft' },
      { path: 'app.show_suggestions', action: 'reset' },
    ],
  });
});

test('the draft cannot be discarded while saving and the count clears after success', async ({
  page,
  isMobile,
}) => {
  const { state } = await settingsAPI(page);
  let finishValidation = () => {};
  const validationGate = new Promise<void>((resolve) => {
    finishValidation = resolve;
  });
  let finishApply = () => {};
  const applyGate = new Promise<void>((resolve) => {
    finishApply = resolve;
  });
  let finishRefresh = () => {};
  const refreshGate = new Promise<void>((resolve) => {
    finishRefresh = resolve;
  });
  let applied = false;
  await page.route('**/api/settings', async (route) => {
    if (applied) await refreshGate;
    await route.fulfill({ json: state });
  });
  await page.route('**/api/settings/validate', async (route) => {
    await validationGate;
    await route.fulfill({ json: { revision: state.revision, values: state.values } });
  });
  await page.route('**/api/settings/apply', async (route) => {
    await applyGate;
    state.values[0].value = 'Saved draft';
    applied = true;
    return route.fulfill({ json: { saved: true } });
  });
  if (isMobile) await page.setViewportSize({ width: 320, height: 800 });
  await page.goto('/settings');
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  const save = page.getByRole('button', { name: 'Save and apply', exact: true });
  const discard = page.getByRole('button', { name: 'Discard changes', exact: true });
  const status = page.getByRole('group', { name: 'Settings actions' }).getByRole('status');
  await title.fill('Saved draft');
  try {
    await save.click();
    await expect(status).toHaveText('Validating settings…');
    await expect(save).toBeDisabled();
    await expect(discard).toBeDisabled();
    await expect(title).toBeDisabled();
    await expect(page.getByText('Unsaved', { exact: true })).toBeVisible();
    finishValidation();
    await expect(status).toHaveText('Applying settings. Service may be briefly interrupted…');
    await expect(status).toBeInViewport();
    await expect(save).toBeInViewport();
    await expect(discard).toBeInViewport();
    const saveBox = (await save.boundingBox())!;
    const discardBox = (await discard.boundingBox())!;
    expect(discardBox.y).toBe(saveBox.y);
    finishApply();
    await expect(status).toHaveText('Reconnecting and reading active settings…');
    await expect(save).toBeDisabled();
    await expect(discard).toBeDisabled();
    await expect(status).toBeInViewport();
  } finally {
    finishValidation();
    finishApply();
    finishRefresh();
  }
  await expect(status).toHaveText('Settings saved and applied.');
  await expect(status).toBeInViewport();
  await expect(page.getByRole('status')).toHaveCount(1);
  await expect(title).toHaveValue('Saved draft');
  await expect(page.getByText('No unsaved changes', { exact: true })).toBeVisible();
  await expect(discard).toBeDisabled();
  await expect(page.getByText('Unsaved', { exact: true })).toHaveCount(0);
});

test('related settings are grouped while drafts and unrecognized fields remain available', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  state.fields.push({
    ...state.fields[0],
    path: 'app.future_option',
    label: 'Future option',
    group: undefined,
  });
  state.values.push({ ...state.values[0], path: 'app.future_option', value: 'Available' });
  await page.goto('/settings');
  await expect(page.getByRole('heading', { level: 3 })).toHaveText([
    'Appearance',
    'Search behavior',
    'Other settings',
  ]);
  const appearance = page.getByRole('region', { name: 'Appearance', exact: true });
  const search = page.getByRole('region', { name: 'Search behavior', exact: true });
  const other = page.getByRole('region', { name: 'Other settings', exact: true });
  for (const [group, fieldName] of [
    [appearance, 'Title'],
    [search, 'Snippet length'],
    [other, 'Future option'],
  ] as const) {
    const heading = group.getByRole('heading', { level: 3 });
    await heading.scrollIntoViewIfNeeded();
    const headingBox = (await heading.boundingBox())!;
    const groupBox = (await group.boundingBox())!;
    const row = group.getByRole('region', { name: fieldName, exact: true });
    const labelBox = (await row.getByText(fieldName, { exact: true }).boundingBox())!;
    const rowBox = (await row.boundingBox())!;
    expect(labelBox.x).toBeCloseTo(headingBox.x, 0);
    expect(rowBox.x).toBeGreaterThanOrEqual(groupBox.x);
    expect(rowBox.x + rowBox.width).toBeLessThanOrEqual(groupBox.x + groupBox.width);
  }
  await expect(
    appearance.getByRole('combobox', { name: 'Color scheme', exact: true }),
  ).toBeVisible();
  await expect(
    search.getByRole('switch', { name: 'Show suggestions', exact: true }),
  ).toBeAttached();
  await expect(other.getByRole('textbox', { name: 'Future option', exact: true })).toHaveValue(
    'Available',
  );
  await expect(page.getByRole('textbox', { name: 'Future option', exact: true })).toHaveCount(1);
  await appearance.getByRole('textbox', { name: 'Title', exact: true }).fill('Grouped archive');
  await search.getByRole('spinbutton', { name: 'Snippet length', exact: true }).fill('420');
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(appearance.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Grouped archive',
  );
  await expect(search.getByRole('spinbutton', { name: 'Snippet length', exact: true })).toHaveValue(
    '420',
  );
});

test('settings groups follow server labels and display order', async ({ page }) => {
  const { state } = await settingsAPI(page);
  const [title, subtitle, snippet, colorScheme, suggestions, address] = state.fields;
  state.fields = [
    Object.assign(snippet, { group: 'Result tuning' }),
    Object.assign(suggestions, { group: 'Result tuning' }),
    Object.assign(title, { group: 'Branding' }),
    Object.assign(subtitle, { group: 'Branding' }),
    Object.assign(colorScheme, { group: 'Branding' }),
    Object.assign(
      { ...title, path: 'app.extra_display', label: 'Extra display option' },
      { group: 'Branding' },
    ),
    address,
  ];
  state.values.push({ ...state.values[0], path: 'app.extra_display', value: 'From the server' });
  await page.goto('/settings');
  await expect(page.getByRole('heading', { level: 3 })).toHaveText(['Result tuning', 'Branding']);
  const results = page.getByRole('region', { name: 'Result tuning', exact: true });
  const branding = page.getByRole('region', { name: 'Branding', exact: true });
  await expect(
    results.getByRole('spinbutton', { name: 'Snippet length', exact: true }),
  ).toHaveValue('300');
  await expect(branding.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
  await expect(
    branding.getByRole('textbox', { name: 'Extra display option', exact: true }),
  ).toHaveValue('From the server');
});

test('categories without defined groups keep all their fields visible', async ({ page }) => {
  const { state } = await settingsAPI(page);
  state.fields.push({
    ...state.fields[0],
    category: 'custom',
    group: undefined,
    path: 'custom.option',
    label: 'Custom option',
  });
  state.values.push({ ...state.values[0], path: 'custom.option', value: 'Available' });
  await page.goto('/settings');
  await page.getByRole('tab', { name: 'custom', exact: true }).click();
  await expect(page.getByRole('tabpanel').getByRole('heading', { level: 3 })).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Custom option', exact: true })).toHaveValue(
    'Available',
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

test('source tooltips show explicit locks and overridden web selections on click', async ({
  page,
  isMobile,
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
  const info = region.getByRole('button', { name: 'Source information for Subtitle' });
  const tooltip = page.getByRole('tooltip');
  await expect(info).toBeVisible();
  await expect(info).toHaveText('Locked');
  await expect(page.getByText('Source: Environment', { exact: false })).not.toBeVisible();
  if (isMobile) await info.tap();
  else await info.click();
  await expect(tooltip).toContainText('Source: Environment (HISTER__APP__SUBTITLE)');
  await expect(tooltip).toContainText('Set by environment: HISTER__APP__SUBTITLE');
  await expect(tooltip).toContainText('A stored web selection is overridden by this source.');
  await expect(info).toHaveAccessibleDescription(/Source: Environment/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('heading', { name: 'Settings', exact: true }).click();
  await expect(tooltip).not.toBeVisible();
  await expect(region.getByRole('textbox')).toHaveCount(0);
  await expect(region.getByText('Managed title', { exact: true })).toBeVisible();
  await expect(region.getByText('Locked', { exact: true })).toBeVisible();
  await expect(region.getByRole('button', { name: /^(Clear|Reset|Undo) / })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  const address = page.getByRole('region', { name: 'Listen address', exact: true });
  const addressInfo = address.getByRole('button', {
    name: 'Source information for Listen address',
  });
  await expect(addressInfo).toHaveText('Read only');
  await addressInfo.click();
  await expect(tooltip).toContainText('Source: Built in default');
  await expect(tooltip).toContainText('Connection changes require confirmation');
  await expect(address.getByText('Locked', { exact: true })).toHaveCount(0);
  await expect(address.getByText('127.0.0.1:4433', { exact: true })).toBeVisible();
  await expect(address.getByRole('textbox')).toHaveCount(0);
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
  await address.getByRole('button', { name: 'Source information for Listen address' }).click();
  await expect(tooltip).toContainText('Source: Main configuration (/etc/hister/config.yml)');
});

test('source tooltips remain readable on hover and dismiss with Escape', async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, 'Hover requires a mouse pointer.');
  await settingsAPI(page);
  await page.goto('/settings');
  const info = page.getByRole('button', { name: 'Source information for Title', exact: true });
  const tooltip = page.getByRole('tooltip');
  await info.hover();
  await expect(tooltip).toContainText('Source: Built in default');
  await tooltip.hover();
  await expect(tooltip).toBeVisible();
  await info.click();
  await expect(tooltip).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(tooltip).not.toBeVisible();
  await page.getByRole('heading', { name: 'Settings', exact: true }).hover();
  await info.hover();
  await expect(tooltip).toBeVisible();
  await page.getByRole('heading', { name: 'Settings', exact: true }).hover();
  await expect(tooltip).not.toBeVisible();
});

test('source tooltips support keyboard access while draft feedback stays visible', async ({
  page,
}) => {
  await settingsAPI(page);
  await page.goto('/settings');
  const input = page.getByRole('textbox', { name: 'Title', exact: true });
  const info = page.getByRole('button', { name: 'Source information for Title', exact: true });
  const tooltip = page.getByRole('tooltip');
  await input.fill('Draft title');
  await expect(input).toHaveAccessibleDescription(/Current value: Hister\. Unsaved change\./);
  await page.keyboard.press('Shift+Tab');
  await expect(info).toBeFocused();
  await expect(tooltip).toContainText('Source: Built in default');
  await expect(info).toHaveAccessibleDescription('Source: Built in default');
  await page.keyboard.press('Escape');
  await expect(tooltip).not.toBeVisible();
  await page.keyboard.press('Enter');
  await expect(tooltip).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(input).toBeFocused();
  await expect(tooltip).not.toBeVisible();
  await expect(input).toHaveValue('Draft title');
  await expect(page.getByText('Current value: Hister. Unsaved change.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeEnabled();
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
  const reset = page.getByRole('button', { name: 'Reset Title', exact: true });
  const clear = page.getByRole('button', { name: 'Clear Subtitle', exact: true });
  await expect(reset).toHaveText('Reset');
  await expect(clear).toHaveText('Clear');
  await reset.click();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
  const undo = page.getByRole('button', { name: 'Undo Title', exact: true });
  await expect(undo).toHaveText('Undo');
  await undo.press('Enter');
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Saved title',
  );
  await reset.click();
  await clear.click();
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

test('validation errors preserve the draft and clear when it is discarded', async ({ page }) => {
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
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'App', exact: true })).toBeVisible();
  await expect(page.getByRole('spinbutton', { name: 'Snippet length', exact: true })).toHaveValue(
    '300',
  );
  await expect(
    page.getByRole('spinbutton', { name: 'Snippet length', exact: true }),
  ).toHaveAttribute('aria-invalid', 'false');
  await expect(page.getByText('Must be at least 1.', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
});

test('unapplied file settings can be reloaded while preserving an unrelated draft', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  const title = {
    ...state.values[0],
    value: 'Title from disk',
    editable: false,
    source: { kind: 'file', name: '/etc/hister/local.yml', locked: true, web_selected: false },
  };
  state.pending = { status: 'changed', values: [title] };
  let reloads = 0;
  await page.route('**/api/reload', async (route) => {
    expect(route.request().method()).toBe('POST');
    reloads++;
    state.values[0] = title;
    state.revision = 'revision-2';
    state.pending = null;
    await route.fulfill({ status: 200, body: 'OK' });
  });
  let saved: any;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await expect(page.getByRole('alert').locator('p')).toHaveText(
    'The local configuration file (local.yml) has changed and needs to be reloaded.',
  );
  await expect(page.getByRole('alert')).toHaveAccessibleName('Configuration changes not applied');
  await expect(
    page.getByRole('heading', { name: 'Configuration changes not applied', exact: true }),
  ).toBeVisible();
  expect((await page.getByRole('tabpanel').boundingBox())!.height).toBeGreaterThan(300);
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
  const preview = page.getByRole('region', { name: 'Pending Title', exact: true });
  await expect(preview).toContainText('Configured on disk, not applied');
  await expect(preview).toContainText('Title from disk');
  await page.getByRole('textbox', { name: 'Subtitle', exact: true }).fill('Keep my subtitle');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Reload configuration', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Configuration reloaded');
  expect(reloads).toBe(1);
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Title', exact: true })).toContainText(
    'Title from disk',
  );
  await expect(preview).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Subtitle', exact: true })).toHaveValue(
    'Keep my subtitle',
  );
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saved).toEqual({
    revision: 'revision-2',
    edits: [{ path: 'app.subtitle', action: 'set', value: 'Keep my subtitle' }],
  });
});

test('configuration reload requires resolving changed and newly locked draft fields', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  state.pending = { status: 'changed', values: [] };
  await page.route('**/api/reload', async (route) => {
    state.values[0] = {
      ...state.values[0],
      value: 'Locked file title',
      editable: false,
      source: { kind: 'file', locked: true, web_selected: false },
    };
    state.values[1] = {
      ...state.values[1],
      value: 'Another web selection',
      source: { kind: 'web', locked: false, web_selected: true },
    };
    state.revision = 'revision-2';
    state.pending = null;
    await route.fulfill({ status: 200, body: 'OK' });
  });
  let saved: any;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('My title draft');
  await page.getByRole('textbox', { name: 'Subtitle', exact: true }).fill('My subtitle draft');
  await page.getByRole('button', { name: 'Reload configuration', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Configuration reloaded');
  const title = page.getByRole('region', { name: 'Title', exact: true });
  const subtitle = page.getByRole('region', { name: 'Subtitle', exact: true });
  await expect(title.getByRole('alert')).toContainText('no longer editable');
  await expect(title).toContainText('My title draft');
  await expect(title).toContainText('Locked file title');
  await expect(title.getByRole('button', { name: 'Keep my edit', exact: true })).toHaveCount(0);
  await expect(subtitle.getByRole('alert')).toContainText('changed');
  await expect(subtitle.getByRole('textbox')).toHaveValue('My subtitle draft');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await title.getByRole('button', { name: 'Use current value', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await subtitle.getByRole('button', { name: 'Keep my edit', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saved).toEqual({
    revision: 'revision-2',
    edits: [{ path: 'app.subtitle', action: 'set', value: 'My subtitle draft' }],
  });
});

test('refresh detects invalid and repaired configuration without losing the draft', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Keep this draft');
  for (const status of ['invalid', 'unreadable'] as const) {
    state.pending = { status };
    await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText(
      status === 'invalid' ? 'invalid settings' : 'cannot be read',
    );
    await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
      'Keep this draft',
    );
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
    await expect(
      page.getByRole('button', { name: 'Reload configuration', exact: true }),
    ).toHaveCount(0);
  }
  state.pending = null;
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Keep this draft',
  );
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeEnabled();
});

test('a file change discovered during save shows pending values without applying them', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  let reloads = 0;
  await page.route('**/api/reload', (route) => {
    reloads++;
    return route.fulfill({ status: 200 });
  });
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({ status: 409, json: { code: 'sources_changed', error: 'Sources changed.' } }),
  );
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Subtitle', exact: true }).fill('My draft');
  state.pending = {
    status: 'changed',
    values: [
      {
        ...state.values[0],
        value: 'New file title',
        editable: false,
        source: { kind: 'file', locked: true, web_selected: false },
      },
    ],
  };
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Pending Title', exact: true })).toContainText(
    'New file title',
  );
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
  await expect(page.getByRole('textbox', { name: 'Subtitle', exact: true })).toHaveValue(
    'My draft',
  );
  await expect(
    page.getByRole('button', { name: 'Reload configuration', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  expect(reloads).toBe(0);
});

for (const outcome of ['invalid configuration', 'lost response'] as const) {
  test(`configuration reload recovers from ${outcome} without replaying or losing the draft`, async ({
    page,
  }) => {
    const { state } = await settingsAPI(page, { authMode: 'token', authenticated: true });
    state.pending = { status: 'changed', values: [] };
    await page.route('**/api/config', (route) =>
      route.fulfill({
        headers: { 'X-CSRF-Token': 'csrf-current' },
        json: {
          authMode: 'token',
          authenticated: true,
          canConfigure: true,
          title: 'Hister',
          colorScheme: 'automatic',
        },
      }),
    );
    let reloads = 0;
    await page.route('**/api/reload', (route) => {
      expect(route.request().headers()['x-csrf-token']).toBe('csrf-current');
      reloads++;
      if (outcome === 'invalid configuration') {
        state.pending = { status: 'invalid' };
        return route.fulfill({ status: 400, body: 'private-configuration-detail' });
      }
      state.pending = null;
      state.values[0] = {
        ...state.values[0],
        value: 'Reloaded title',
        editable: false,
        source: { kind: 'file', locked: true, web_selected: false },
      };
      state.revision = 'revision-2';
      return route.abort('failed');
    });
    await page.goto('/settings');
    await page.getByRole('textbox', { name: 'Subtitle', exact: true }).fill('Private draft');
    await page.getByRole('button', { name: 'Reload configuration', exact: true }).click();
    if (outcome === 'invalid configuration') {
      await expect(page.getByRole('alert').filter({ hasText: 'invalid settings' })).toBeVisible();
      await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Hister');
    } else {
      await expect(
        page.getByRole('alert').filter({ hasText: 'reload response was lost' }),
      ).toBeVisible();
      await expect(page.getByRole('region', { name: 'Title', exact: true })).toContainText(
        'Reloaded title',
      );
    }
    await expect(page.getByRole('textbox', { name: 'Subtitle', exact: true })).toHaveValue(
      'Private draft',
    );
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
    await expect(
      page.getByRole('button', { name: 'Reload configuration', exact: true }),
    ).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText('private-configuration-detail');
    expect(reloads).toBe(1);
    expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
      'Private draft',
    );
    if (outcome === 'lost response') {
      await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeEnabled();
    }
  });
}

test('stale revisions retain the draft and require conflict resolution after refresh', async ({
  page,
}) => {
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
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Keep this draft',
  );
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Use current value', exact: true }).click();
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
  await expect(page.getByRole('alert').filter({ hasText: 'response was lost' })).toBeVisible();
  await expect.poll(() => readsAfterSave).toBeGreaterThan(0);
  expect(saves).toBe(1);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Saved despite disconnect',
  );
});

test('refresh retains conflicts found while recovering from a lost save response', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  await page.route('**/api/settings/apply', (route) => {
    state.values[0].value = 'External title';
    state.revision = 'revision-2';
    return route.abort('failed');
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('My draft');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  const title = page.getByRole('region', { name: 'Title', exact: true });
  await expect(title).toContainText('Current value: External title.');
  for (let attempt = 0; attempt < 2; attempt++) {
    await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
    await expect(title.getByRole('alert')).toContainText('active setting changed');
    await expect(title.getByRole('textbox')).toHaveValue('My draft');
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  }
  await title.getByRole('button', { name: 'Keep my edit', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeEnabled();
});

test('discarding a stale draft preserves the reload requirement without claiming the draft remains', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      status: 409,
      json: { code: 'stale_revision', error: 'Read current settings.' },
    }),
  );
  await page.goto('/settings');
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  const save = page.getByRole('button', { name: 'Save and apply', exact: true });
  const discard = page.getByRole('button', { name: 'Discard changes', exact: true });
  await title.fill('Stale draft');
  await save.click();
  await expect(page.getByRole('alert')).toContainText('Your draft is still here.');
  await discard.click();
  await expect(title).toHaveValue('Hister');
  await expect(page.getByRole('alert')).toContainText('Refresh settings before saving again.');
  await expect(page.getByRole('alert')).not.toContainText('Your draft is still here.');
  await title.fill('Another draft');
  await expect(save).toBeDisabled();
  await expect(page.getByRole('alert')).toContainText('Your draft is still here.');
  await discard.click();
  state.values[0].value = 'Current server title';
  state.revision = 'revision-2';
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(title).toHaveValue('Current server title');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await title.fill('Fresh draft');
  await expect(save).toBeEnabled();
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
  isMobile,
}) => {
  await settingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Unsaved');
  await page.getByRole('tab', { name: 'App', exact: true }).focus();
  const next = isMobile ? 'ArrowRight' : 'ArrowDown';
  const previous = isMobile ? 'ArrowLeft' : 'ArrowUp';
  await page.keyboard.press(next);
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toBeFocused();
  await expect(page.getByRole('tabpanel')).toHaveAccessibleName('Server');
  await expect(page).toHaveURL(/\/settings\?tab=server$/);
  await page.keyboard.press(next);
  await expect(page.getByRole('tab', { name: 'App', exact: true })).toBeFocused();
  await page.keyboard.press(previous);
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toBeFocused();
  await page.keyboard.press('Home');
  const navigation = page.getByRole('tablist', { name: 'Configuration categories' });
  await expect(navigation).toHaveAttribute(
    'aria-orientation',
    isMobile ? 'horizontal' : 'vertical',
  );
  await page.keyboard.press(isMobile ? 'ArrowDown' : 'ArrowRight');
  await expect(page.getByRole('tab', { name: 'App', exact: true })).toBeFocused();
  await page.setViewportSize({ width: isMobile ? 1280 : 375, height: 800 });
  await expect(navigation).toHaveAttribute(
    'aria-orientation',
    isMobile ? 'vertical' : 'horizontal',
  );
  await page.keyboard.press(isMobile ? 'ArrowDown' : 'ArrowRight');
  await expect(page.getByRole('tab', { name: 'Server', exact: true })).toBeFocused();
  await page.keyboard.press('Home');
  await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue('Unsaved');
  await expect(page).toHaveURL(/\/settings$/);
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

test('category navigation fits beside desktop settings and above mobile settings', async ({
  page,
  isMobile,
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
  const navigation = page.getByRole('tablist', { name: 'Configuration categories' });
  const tabs = navigation.getByRole('tab');
  await expect(tabs).toHaveCount(8);
  const navigationBox = (await navigation.boundingBox())!;
  const panelBox = (await page.getByRole('tabpanel').boundingBox())!;
  const appBox = (await tabs.first().boundingBox())!;
  const serverBox = (await tabs.nth(1).boundingBox())!;
  if (isMobile) {
    expect(navigationBox.y + navigationBox.height).toBeLessThanOrEqual(panelBox.y + 1);
    expect(serverBox.x).toBeGreaterThan(appBox.x);
    expect(serverBox.y).toBe(appBox.y);
  } else {
    expect(navigationBox.x + navigationBox.width).toBeLessThanOrEqual(panelBox.x + 1);
    expect(navigationBox.width).toBeLessThan(300);
    expect(serverBox.y).toBeGreaterThan(appBox.y);
    expect(serverBox.x).toBe(appBox.x);
    for (const tab of await tabs.all()) await expect(tab).toBeInViewport();
  }
  await page
    .getByRole('textbox', { name: 'Title', exact: true })
    .fill('Draft across all categories');
  await page.getByRole('tab', { name: 'App', exact: true }).focus();
  await page.keyboard.press('End');
  await expect(tabs.last()).toBeInViewport();
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

async function indexerSettingsAPI(page: Page) {
  const { state } = await settingsAPI(page);
  const text = { type: 'string', control: 'text', collection: 'none', sensitivity: 'none' };
  const toggle = { type: 'boolean', control: 'switch', collection: 'none', sensitivity: 'none' };
  const list = {
    type: 'list',
    control: 'list',
    collection: 'replace',
    sensitivity: 'none',
    items: text,
  };
  for (const field of [
    {
      path: 'indexer.languages',
      label: 'Languages',
      schema: {
        ...list,
        items: {
          ...text,
          control: 'select',
          choices: ['en', 'hu', 'de'],
          choice_labels: { en: 'English', hu: 'Hungarian', de: 'German' },
        },
      },
      default: [],
    },
    {
      path: 'indexer.max_file_size_mb',
      label: 'Maximum file size',
      schema: {
        type: 'integer',
        control: 'number',
        collection: 'none',
        sensitivity: 'none',
        min: 1,
      },
      default: 1,
    },
    {
      path: 'indexer.directories',
      label: 'Watched directories',
      default: [],
      schema: {
        ...list,
        items: {
          type: 'object',
          control: 'object',
          collection: 'none',
          sensitivity: 'none',
          properties: [
            {
              name: 'path',
              label: 'Path',
              description:
                'Existing absolute server path or ~/ path. Directories must not overlap.',
              required: true,
              schema: text,
            },
            { name: 'label', label: 'Label', schema: text },
            { name: 'user', label: 'Owner', schema: text },
            { name: 'filetypes', label: 'File types', schema: list },
            { name: 'patterns', label: 'Include patterns', schema: list },
            { name: 'excludes', label: 'Exclude patterns', schema: list },
            { name: 'include_hidden', label: 'Include hidden files', schema: toggle },
            { name: 'delete_on_remove', label: 'Delete from index when removed', schema: toggle },
          ],
        },
      },
    },
  ]) {
    state.fields.push({
      ...field,
      category: 'indexer',
      description: 'Indexer setting.',
      effect: 'reindex',
      requires_confirmation: false,
    });
    state.values.push({
      path: field.path,
      value: field.default,
      editable: true,
      secret_present: false,
      source: { kind: 'default', locked: false, web_selected: false },
    });
  }
  return state;
}

test('watched directory fields indicate which parameters are required or optional', async ({
  page,
}) => {
  const state = await indexerSettingsAPI(page);
  state.values.find((field) => field.path === 'indexer.directories')!.value = [
    { path: '/srv/notes' },
  ];
  await page.goto('/settings?tab=indexer');
  for (const name of ['Watched directories 1', 'Watched directories 2']) {
    const row = page.getByRole('group', { name, exact: true });
    if (name.endsWith('1')) await row.getByRole('button', { name: 'Edit', exact: true }).click();
    else await page.getByRole('button', { name: 'Add directory', exact: true }).click();
    await row.getByText('Filters and options', { exact: true }).click();
    await expect(row.getByText('Required', { exact: true })).toBeVisible();
    await expect(row.getByText('Optional', { exact: true })).toHaveCount(7);
    for (const marker of await row.getByText('Optional', { exact: true }).all()) {
      await expect(marker).toBeVisible();
    }
    await expect(
      row.getByRole('textbox', { name: 'Path', exact: true }),
    ).toHaveAccessibleDescription(
      'Required Existing absolute server path or ~/ path. Directories must not overlap.',
    );
    for (const label of ['Label', 'Owner', 'File types', 'Include patterns', 'Exclude patterns']) {
      await expect(
        row.getByRole('textbox', { name: label, exact: true }),
      ).toHaveAccessibleDescription('Optional');
    }
    for (const label of ['Include hidden files', 'Delete from index when removed']) {
      await expect(
        row.getByRole('switch', { name: label, exact: true }),
      ).toHaveAccessibleDescription('Optional');
    }
    await row.getByRole('button', { name: 'Cancel', exact: true }).click();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('Indexer controls save languages and structured directories in the shared draft', async ({
  page,
}) => {
  await indexerSettingsAPI(page);
  let saved: any;
  await page.route('**/api/settings/apply', (route) => {
    saved = route.request().postDataJSON();
    return route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Indexer draft');
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  const languages = page.getByRole('listbox', { name: 'Languages', exact: true });
  await expect(languages.getByRole('option')).toHaveText(['English', 'Hungarian', 'German']);
  await languages.selectOption([{ label: 'English' }, { label: 'Hungarian' }]);
  await page.getByRole('spinbutton', { name: 'Maximum file size', exact: true }).fill('8');
  await page.getByRole('button', { name: 'Add directory', exact: true }).click();
  const row = page.getByRole('group', { name: 'Watched directories 1', exact: true });
  await row.getByRole('textbox', { name: 'Path', exact: true }).fill('/srv/notes');
  await row.getByRole('textbox', { name: 'Owner', exact: true }).fill('alice');
  await row.getByRole('textbox', { name: 'Label', exact: true }).fill('notes');
  await row.getByText('Filters and options', { exact: true }).click();
  await row.getByRole('textbox', { name: 'File types', exact: true }).fill('txt\nmd');
  await row.getByRole('textbox', { name: 'Include patterns', exact: true }).fill('note*');
  await row.getByRole('textbox', { name: 'Exclude patterns', exact: true }).fill('secret*');
  await row.getByRole('switch', { name: 'Include hidden files', exact: true }).check();
  await row.getByRole('switch', { name: 'Delete from index when removed', exact: true }).check();
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await expect(row.getByRole('textbox', { name: 'Path', exact: true })).toHaveValue('/srv/notes');
  await row.getByRole('button', { name: 'Add directory', exact: true }).click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(saved.edits).toEqual(
    expect.arrayContaining([
      { path: 'app.title', action: 'set', value: 'Indexer draft' },
      { path: 'indexer.languages', action: 'set', value: ['en', 'hu'] },
      { path: 'indexer.max_file_size_mb', action: 'set', value: 8 },
      {
        path: 'indexer.directories',
        action: 'set',
        value: [
          {
            path: '/srv/notes',
            label: 'notes',
            user: 'alice',
            filetypes: ['txt', 'md'],
            patterns: ['note*'],
            excludes: ['secret*'],
            include_hidden: true,
            delete_on_remove: true,
          },
        ],
      },
    ]),
  );
});

test('Indexer collections retain invalid drafts and support reset, clear and source locks', async ({
  page,
}) => {
  const state = await indexerSettingsAPI(page);
  const directories = state.values.find((field) => field.path === 'indexer.directories')!;
  directories.value = [{ path: '/srv/original', delete_on_remove: true }];
  Object.assign(directories.source, { kind: 'web', web_selected: true });
  let draft: any;
  await page.route('**/api/settings/validate', (route) => {
    draft = route.request().postDataJSON();
    return route.fulfill({
      status: 422,
      json: {
        errors: [
          {
            path: 'indexer.directories',
            code: 'invalid_value',
            message: 'Directory 1: Choose an existing directory.',
          },
        ],
      },
    });
  });
  await page.goto('/settings');
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await page
    .getByRole('group', { name: 'Watched directories 1', exact: true })
    .getByRole('button', { name: 'Edit', exact: true })
    .click();
  await page.getByRole('textbox', { name: 'Path', exact: true }).fill('/missing');
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(
    page.getByText('Directory 1: Choose an existing directory.', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Path', exact: true })).toHaveValue('/missing');
  await page.getByRole('button', { name: 'Reset Watched directories', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Path', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  expect(draft.edits).toEqual([{ path: 'indexer.directories', action: 'reset' }]);
  await page.getByRole('button', { name: 'Undo Watched directories', exact: true }).click();
  await page
    .getByRole('button', { name: 'Remove Watched directories item 1', exact: true })
    .click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  expect(draft.edits).toEqual([{ path: 'indexer.directories', action: 'set', value: [] }]);
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  Object.assign(directories.source, { kind: 'file', locked: true });
  directories.editable = false;
  await page.reload();
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Add directory', exact: true })).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Path', exact: true })).toHaveCount(0);
  await expect(
    page.getByRole('region', { name: 'Watched directories', exact: true }),
  ).toContainText('/srv/original');
});

test('locked watched directories show readable details without editing controls', async ({
  page,
}) => {
  const state = await indexerSettingsAPI(page);
  const directories = state.values.find((field) => field.path === 'indexer.directories')!;
  const longPath = `/srv/${'archive'.repeat(20)}/notes`;
  const longPattern = `${'private'.repeat(20)}*`;
  directories.value = [
    {
      path: longPath,
      label: 'Research notes',
      user: 'alice',
      filetypes: ['md', 'txt'],
      patterns: ['note*', 'journal*'],
      excludes: ['secret*', longPattern],
      include_hidden: true,
      delete_on_remove: false,
    },
    {
      path: '/srv/books',
      label: 'Books',
      user: 'bob',
      filetypes: ['pdf'],
      patterns: ['book*'],
      excludes: ['draft*'],
      include_hidden: false,
      delete_on_remove: true,
    },
  ];
  directories.editable = false;
  Object.assign(directories.source, { kind: 'file', locked: true, web_selected: true });
  directories.reason = 'This setting is managed in the main configuration.';
  await page.goto('/settings?tab=indexer');
  const region = page.getByRole('region', { name: 'Watched directories', exact: true });
  const first = region.getByRole('group', { name: 'Watched directories 1', exact: true });
  await expect(first.getByRole('heading', { name: longPath, exact: true })).toBeVisible();
  await expect(first.getByRole('term')).toHaveText([
    'Label',
    'Owner',
    'File types',
    'Include patterns',
    'Exclude patterns',
    'Include hidden files',
    'Delete from index when removed',
  ]);
  await expect(first.getByRole('definition')).toHaveText([
    'Research notes',
    'alice',
    'md txt',
    'note* journal*',
    `secret* ${longPattern}`,
    'Enabled',
    'Disabled',
  ]);
  const second = region.getByRole('group', { name: 'Watched directories 2', exact: true });
  await expect(second.getByRole('heading', { name: '/srv/books', exact: true })).toBeVisible();
  await expect(second.getByRole('definition')).toHaveText([
    'Books',
    'bob',
    'pdf',
    'book*',
    'draft*',
    'Disabled',
    'Enabled',
  ]);
  await expect(region.getByRole('textbox')).toHaveCount(0);
  await expect(region.getByRole('switch')).toHaveCount(0);
  await expect(region.getByRole('button')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await region
    .getByRole('button', {
      name: 'Locked: Source information for Watched directories',
      exact: true,
    })
    .click();
  await expect(page.getByRole('tooltip')).toContainText('Source: Main configuration');
  await expect(page.getByRole('tooltip')).toContainText(
    'This setting is managed in the main configuration.',
  );
  await expect(page.getByRole('tooltip')).toContainText(
    'A stored web selection is overridden by this source.',
  );
});

test('locked watched directories explain empty options and an empty collection', async ({
  page,
}) => {
  const state = await indexerSettingsAPI(page);
  const directories = state.values.find((field) => field.path === 'indexer.directories')!;
  directories.value = [
    { path: '~/Documents', label: '', user: '', include_hidden: false, delete_on_remove: false },
    { path: '/srv/shared', label: '', user: '', filetypes: [], patterns: [], excludes: [] },
  ];
  directories.editable = false;
  Object.assign(directories.source, { kind: 'environment', locked: true });
  await page.goto('/settings?tab=indexer');
  const region = page.getByRole('region', { name: 'Watched directories', exact: true });
  for (const name of ['Watched directories 1', 'Watched directories 2']) {
    await expect(
      region.getByRole('group', { name, exact: true }).getByRole('definition'),
    ).toHaveText([
      'None',
      'Everyone with search access',
      'All file types',
      'All file names',
      'None',
      'Disabled',
      'Disabled',
    ]);
  }
  for (const empty of [[], null]) {
    directories.value = empty;
    await page.reload();
    await expect(
      region.getByText('No watched directories configured.', { exact: true }),
    ).toBeVisible();
    await expect(region.getByRole('group')).toHaveCount(0);
    await expect(region.getByRole('button')).toHaveCount(1);
  }
});

test('language names describe active and locked selections', async ({ page }) => {
  const state = await indexerSettingsAPI(page);
  const languages = state.values.find((field) => field.path === 'indexer.languages')!;
  languages.value = ['en', 'hu'];
  await page.goto('/settings');
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await page.getByRole('listbox', { name: 'Languages', exact: true }).selectOption(['en', 'de']);
  await expect(
    page.getByText('Current value: English, Hungarian. Unsaved change.', { exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  Object.assign(languages.source, { kind: 'file', locked: true });
  languages.editable = false;
  await page.reload();
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Languages', exact: true })).toContainText(
    'English, Hungarian',
  );
  await expect(page.getByRole('listbox', { name: 'Languages', exact: true })).toHaveCount(0);
});

async function crawlerSettingsAPI(page: Page) {
  const { state } = await settingsAPI(page);
  const text = { type: 'string', control: 'text', collection: 'none', sensitivity: 'none' };
  const secret = { ...text, control: 'secret', sensitivity: 'secret' };
  const number = {
    type: 'integer',
    control: 'number',
    collection: 'none',
    sensitivity: 'none',
    min: 0,
  };
  const fields = [
    { path: 'crawler.timeout', label: 'Crawler timeout', schema: number, value: 5 },
    {
      path: 'crawler.backend',
      label: 'Crawler backend',
      schema: { ...text, control: 'select', choices: ['http', 'chromedp', 'bidi'] },
      value: 'http',
    },
    {
      path: 'crawler.backend_options',
      label: 'Crawler backend options',
      schema: {
        type: 'object',
        control: 'object',
        collection: 'replace',
        sensitivity: 'mixed',
        properties: [
          {
            name: 'capture_delay',
            label: 'Capture delay',
            schema: { ...number, type: 'number', max: 86400 },
          },
          { name: 'port', label: 'Browser port', schema: { ...number, min: 1, max: 65535 } },
          { name: 'socket', label: 'Browser socket URL', schema: { ...secret, min_length: 1 } },
        ],
      },
      value: {},
    },
    { path: 'crawler.proxy', label: 'Crawler proxy', schema: secret, value: { action: 'clear' } },
    {
      path: 'crawler.headers',
      label: 'Crawler headers',
      schema: {
        type: 'dictionary',
        control: 'dictionary',
        collection: 'replace',
        sensitivity: 'mixed',
        items: secret,
      },
      value: {
        authorization: { action: 'keep', ref: 'header-reference' },
        'x-remove': { action: 'keep', ref: 'remove-reference' },
      },
    },
    {
      path: 'crawler.cookies',
      label: 'Crawler cookies',
      schema: {
        type: 'list',
        control: 'list',
        collection: 'replace',
        sensitivity: 'mixed',
        items: {
          type: 'object',
          control: 'object',
          collection: 'none',
          sensitivity: 'mixed',
          properties: [
            { name: 'name', label: 'Name', required: true, schema: text },
            { name: 'domain', label: 'Domain', required: true, schema: text },
            { name: 'path', label: 'Path', schema: text },
            { name: 'value', label: 'Value', required: true, schema: secret },
          ],
        },
      },
      value: [
        {
          name: 'session',
          domain: 'example.com',
          path: '/',
          value: { action: 'keep', ref: 'cookie-reference' },
        },
      ],
    },
  ];
  for (const field of fields) {
    state.fields.push({
      ...field,
      category: 'crawler',
      description: `${field.label} description.`,
      effect: 'runtime',
      requires_confirmation: false,
    });
    state.values.push({
      path: field.path,
      value: field.value,
      editable: true,
      secret_present: ['crawler.headers', 'crawler.cookies'].includes(field.path),
      source: { kind: 'web', locked: false, web_selected: true },
    });
  }
  return state;
}

test('collection items switch between summaries and cancellable editors', async ({ page }) => {
  await crawlerSettingsAPI(page);
  const applied: any[] = [];
  await page.route('**/api/settings/apply', async (route) => {
    applied.push(route.request().postDataJSON());
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=crawler');
  const crawler = page.getByRole('tab', { name: 'Crawler', exact: true });
  const crawlerBadge = page.getByRole('button', { name: 'Unsaved items in Crawler', exact: true });
  await expect(crawler).toHaveText('Crawler');
  await expect(crawlerBadge).toHaveCount(0);
  const cookie = page.getByRole('group', { name: 'Crawler cookies 1', exact: true });
  await expect(cookie.getByRole('textbox')).toHaveCount(0);
  await expect(cookie).toContainText('session');
  await expect(cookie).toContainText('example.com');
  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  await cookie.getByLabel('Name', { exact: true }).fill('changed');
  await expect(crawlerBadge.getByText('1', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Add header', exact: true })).toBeDisabled();
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(crawlerBadge.getByText('1', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Continue editing', exact: true }).click();
  await expect(cookie.getByLabel('Name', { exact: true })).toHaveValue('changed');
  await cookie.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(crawler).toHaveText('Crawler');
  await expect(crawlerBadge).toHaveCount(0);
  await expect(cookie).toContainText('session');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();

  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  await cookie.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('replace');
  await cookie.getByLabel('Value replacement', { exact: true }).fill('private-cookie-draft');
  await cookie.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(crawlerBadge.getByText('1', { exact: true })).toBeVisible();
  await expect(cookie.getByRole('textbox')).toHaveCount(0);
  await expect(cookie.getByRole('button', { name: 'Edit', exact: true })).toBeFocused();
  await expect(cookie).toContainText('Unsaved');
  await expect(cookie).not.toContainText('private-cookie-draft');
  await expect(cookie).not.toContainText('cookie-reference');
  expect(applied).toHaveLength(0);
  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  await cookie.getByLabel('Value replacement', { exact: true }).fill('cancelled-value');
  await expect(crawlerBadge.getByText('1', { exact: true })).toBeVisible();
  await cookie.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Remove Crawler cookies item 1', exact: true }).click();
  await expect(cookie).toHaveCount(0);
  await page.getByRole('button', { name: 'Undo removal', exact: true }).click();
  await expect(cookie).toContainText('session');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  await expect(crawler).toHaveText('Crawler');
  await expect(crawlerBadge).toHaveCount(0);
  expect(applied[0].edits[0].value[0].value.value).toBe('private-cookie-draft');
});

test('new collection items validate before collapsing and Cancel removes unfinished rows', async ({
  page,
}) => {
  await crawlerSettingsAPI(page);
  const validated: any[] = [];
  await page.route('**/api/settings/validate', async (route) => {
    const draft = route.request().postDataJSON();
    validated.push(draft);
    const cookies = draft.edits.find((edit: any) => edit.path === 'crawler.cookies')?.value;
    if (cookies?.at(-1)?.domain === 'invalid') {
      await route.fulfill({
        status: 422,
        json: {
          code: 'invalid_settings',
          errors: [
            { path: 'crawler.cookies', code: 'invalid_value', message: 'Check the cookie domain.' },
          ],
        },
      });
    } else await route.fulfill({ json: { revision: 'revision-1' } });
  });
  await page.goto('/settings?tab=crawler');
  await page.getByRole('button', { name: 'Add cookie', exact: true }).click();
  const cookie = page.getByRole('group', { name: 'Crawler cookies 2', exact: true });
  await cookie.getByLabel('Name', { exact: true }).fill('new-session');
  await cookie.getByLabel('Domain', { exact: true }).fill('invalid');
  await cookie.getByLabel('Value', { exact: true }).fill('new-secret');
  await cookie.getByRole('button', { name: 'Add cookie', exact: true }).click();
  await expect(page.getByText('Check the cookie domain.', { exact: true })).toBeVisible();
  await expect(cookie.getByLabel('Value', { exact: true })).toHaveValue('new-secret');
  await cookie.getByLabel('Domain', { exact: true }).fill('valid.example');
  await cookie.getByRole('button', { name: 'Add cookie', exact: true }).click();
  await expect(cookie.getByRole('textbox')).toHaveCount(0);
  expect(validated).toHaveLength(2);
  await page.getByRole('button', { name: 'Add cookie', exact: true }).click();
  const unfinished = page.getByRole('group', { name: 'Crawler cookies 3', exact: true });
  await unfinished.getByLabel('Name', { exact: true }).fill('discard-me');
  await unfinished.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(unfinished).toHaveCount(0);

  await page.getByRole('button', { name: 'Add header', exact: true }).click();
  const header = page.getByRole('group', { name: 'Crawler headers 3', exact: true });
  await header.getByLabel('Name', { exact: true }).fill('AUTHORIZATION');
  await header.getByLabel('Value', { exact: true }).fill('duplicate');
  await header.getByRole('button', { name: 'Add header', exact: true }).click();
  await expect(header.getByRole('alert')).toContainText('Names must be unique');
  expect(validated).toHaveLength(2);
  await header.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(header).toHaveCount(0);
});

test('watched directory editors show filters on demand and collapse into useful summaries', async ({
  page,
}) => {
  await indexerSettingsAPI(page);
  await page.goto('/settings?tab=indexer');
  await page.getByRole('button', { name: 'Add directory', exact: true }).click();
  const directory = page.getByRole('group', { name: 'Watched directories 1', exact: true });
  await directory.getByLabel('Path', { exact: true }).fill('/home/me/notes');
  await directory.getByLabel('Owner', { exact: true }).fill('alice');
  await expect(directory.getByLabel('File types', { exact: true })).toBeHidden();
  await directory.getByText('Filters and options', { exact: true }).click();
  await directory.getByLabel('File types', { exact: true }).fill('md\ntxt');
  await directory.getByRole('button', { name: 'Add directory', exact: true }).click();
  await expect(directory.getByRole('textbox')).toHaveCount(0);
  await expect(directory).toContainText('/home/me/notes');
  await expect(directory).toContainText('alice');
  await expect(directory).toContainText('md, txt');
  await expect(directory).toContainText('Unsaved');
  await directory.getByRole('button', { name: 'Edit', exact: true }).click();
  await directory.getByLabel('Path', { exact: true }).fill('/cancelled');
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await page.getByRole('tab', { name: 'Indexer', exact: true }).click();
  await expect(directory.getByLabel('Path', { exact: true })).toHaveValue('/cancelled');
  await directory.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(directory).toContainText('/home/me/notes');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('open collection editors retain refresh conflicts and can be discarded after a source lock', async ({
  page,
}) => {
  const state = await crawlerSettingsAPI(page);
  await page.goto('/settings?tab=crawler');
  const cookie = page.getByRole('group', { name: 'Crawler cookies 1', exact: true });
  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  await cookie.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('replace');
  await cookie.getByLabel('Value replacement', { exact: true }).fill('private-open-draft');
  state.revision = 'revision-2';
  const cookies = state.values.find((value) => value.path === 'crawler.cookies')!;
  (cookies.value as any[])[0].value.ref = 'changed-reference';
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(
    page.getByText(
      'Stored secret references changed. Use the current value and reapply your edits.',
      { exact: true },
    ),
  ).toBeVisible();
  await expect(cookie.getByLabel('Value replacement', { exact: true })).toHaveValue(
    'private-open-draft',
  );
  await expect(cookie.getByRole('button', { name: 'Done', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Use current value', exact: true }).click();
  await expect(cookie.getByLabel('Value replacement', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();

  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  cookies.editable = false;
  cookies.source = { kind: 'file', locked: true, web_selected: true };
  state.revision = 'revision-3';
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(
    page.getByText(
      'This setting is no longer editable. Use the current value to remove your draft.',
      { exact: true },
    ),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Cancel edit', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Continue editing', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Use current value', exact: true })).toHaveCount(0);
});

test('finishing a collection item keeps unrelated validation errors available', async ({
  page,
}) => {
  await crawlerSettingsAPI(page);
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      status: 422,
      json: {
        code: 'invalid_settings',
        errors: [
          { path: 'crawler.timeout', code: 'invalid_value', message: 'Choose a positive timeout.' },
        ],
      },
    }),
  );
  await page.goto('/settings?tab=crawler');
  await page.getByRole('spinbutton', { name: 'Crawler timeout', exact: true }).fill('0');
  await page.getByRole('button', { name: 'Add header', exact: true }).click();
  const header = page.getByRole('group', { name: 'Crawler headers 3', exact: true });
  await header.getByLabel('Name', { exact: true }).fill('x-new');
  await header.getByLabel('Value', { exact: true }).fill('private-value');
  await header.getByRole('button', { name: 'Add header', exact: true }).click();
  await expect(header.getByRole('textbox')).toHaveCount(0);
  await expect(header).toContainText('x-new');
  await expect(page.getByText('Choose a positive timeout.', { exact: true })).toBeVisible();
});

test('crawler controls preserve secret rows and submit one typed draft', async ({ page }) => {
  await crawlerSettingsAPI(page);
  let draft: any;
  await page.route('**/api/settings/apply', async (route) => {
    draft = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=crawler');
  await page.getByRole('spinbutton', { name: 'Crawler timeout', exact: true }).fill('12');
  await page.getByRole('combobox', { name: 'Crawler backend', exact: true }).selectOption('bidi');
  await page.getByRole('spinbutton', { name: 'Capture delay', exact: true }).fill('0.25');
  await page.getByRole('spinbutton', { name: 'Browser port', exact: true }).fill('9223');
  await page.getByRole('button', { name: 'Remove Crawler headers item 2', exact: true }).click();
  await page.getByRole('button', { name: 'Add header', exact: true }).click();
  const header = page.getByRole('group', { name: 'Crawler headers 2', exact: true });
  await header.getByRole('textbox', { name: 'Name', exact: true }).fill('x-new');
  await header.getByLabel('Value', { exact: true }).fill('new-private-header');
  await header.getByRole('button', { name: 'Add header', exact: true }).click();
  const cookie = page.getByRole('group', { name: 'Crawler cookies 1', exact: true });
  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(cookie.getByRole('combobox', { name: 'Value action', exact: true })).toHaveValue(
    'keep',
  );
  await cookie.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('replace');
  await cookie.getByLabel('Value replacement', { exact: true }).fill('new-private-cookie');
  await expect(cookie.getByLabel('Value replacement', { exact: true })).toHaveAttribute(
    'type',
    'password',
  );
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Crawler archive');
  await page.getByRole('tab', { name: 'Crawler', exact: true }).click();
  await expect(header).toContainText('x-new');
  await expect(cookie.getByLabel('Value replacement', { exact: true })).toHaveValue(
    'new-private-cookie',
  );
  await cookie.getByRole('button', { name: 'Done', exact: true }).click();
  expect(
    await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage })),
  ).not.toContain('new-private');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(draft.edits).toEqual(
    expect.arrayContaining([
      { path: 'crawler.timeout', action: 'set', value: 12 },
      { path: 'crawler.backend', action: 'set', value: 'bidi' },
      {
        path: 'crawler.backend_options',
        action: 'set',
        value: { capture_delay: 0.25, port: 9223 },
      },
      {
        path: 'crawler.headers',
        action: 'set',
        value: {
          authorization: { action: 'keep', ref: 'header-reference' },
          'x-new': { action: 'replace', value: 'new-private-header' },
        },
      },
      { path: 'app.title', action: 'set', value: 'Crawler archive' },
    ]),
  );
  expect(
    draft.edits.find((edit: any) => edit.path === 'crawler.cookies').value[0].value,
  ).toMatchObject({ action: 'replace', value: 'new-private-cookie' });
  await expect(page.getByLabel('Value replacement', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('crawler collection Clear and Reset differ and errors retain secret drafts', async ({
  page,
}) => {
  await crawlerSettingsAPI(page);
  const drafts: any[] = [];
  await page.route('**/api/settings/validate', async (route) => {
    drafts.push(route.request().postDataJSON());
    await route.fulfill({
      status: 422,
      json: {
        code: 'invalid_settings',
        errors: [
          { path: 'crawler.cookies', code: 'invalid_value', message: 'Check the cookie domain.' },
        ],
      },
    });
  });
  await page.goto('/settings?tab=crawler');
  await page.getByRole('button', { name: 'Clear Crawler headers', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByText('Check the cookie domain.', { exact: true })).toBeVisible();
  expect(drafts[0].edits).toEqual([{ path: 'crawler.headers', action: 'set', value: {} }]);
  await page.getByRole('button', { name: 'Reset Crawler headers', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect.poll(() => drafts.length).toBe(2);
  expect(drafts[1].edits).toEqual([{ path: 'crawler.headers', action: 'reset' }]);
  const proxy = page.getByRole('region', { name: 'Crawler proxy', exact: true });
  await proxy.getByLabel('Crawler proxy', { exact: true }).fill('http://proxy.example:8080');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect.poll(() => drafts.length).toBe(3);
  await expect(proxy.getByLabel('Crawler proxy', { exact: true })).toHaveValue(
    'http://proxy.example:8080',
  );
});

test('crawler source locks disable complete collections', async ({ page }) => {
  const state = await crawlerSettingsAPI(page);
  for (const path of ['crawler.headers', 'crawler.backend_options']) {
    const value = state.values.find((value) => value.path === path)!;
    value.editable = false;
    value.source = {
      kind: 'environment',
      name: 'HISTER__CRAWLER__HEADERS__AUTHORIZATION',
      locked: true,
      web_selected: true,
    };
    value.reason = 'Controlled by environment';
  }
  await page.goto('/settings?tab=crawler');
  await expect(page.getByRole('combobox', { name: 'Crawler backend', exact: true })).toHaveValue(
    'http',
  );
  await expect(page.getByRole('button', { name: 'Add header', exact: true })).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: 'Clear Crawler headers', exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole('spinbutton', { name: 'Capture delay', exact: true })).toHaveCount(0);
});

test('crawler optional socket accepts a new value and can remove its selection', async ({
  page,
}) => {
  await crawlerSettingsAPI(page);
  await page.goto('/settings?tab=crawler');
  const value = page.getByLabel('Browser socket URL', { exact: true });
  await expect(value).toHaveAttribute('type', 'password');
  await expect(
    page.getByRole('combobox', { name: 'Browser socket URL action', exact: true }),
  ).toHaveCount(0);
  await value.fill('ws://127.0.0.1:9222/session');
  await page.getByRole('button', { name: 'Remove Browser socket URL', exact: true }).click();
  await expect(value).toHaveValue('');
  await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
});

test('new crawler cookies accept a value directly and preserve existing secrets', async ({
  page,
}) => {
  await crawlerSettingsAPI(page);
  let draft: any;
  await page.route('**/api/settings/apply', async (route) => {
    draft = route.request().postDataJSON();
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=crawler');
  await page.getByRole('button', { name: 'Add cookie', exact: true }).click();
  const cookie = page.getByRole('group', { name: 'Crawler cookies 2', exact: true });
  await expect(cookie).toBeVisible();
  await expect(cookie.getByRole('combobox', { name: 'Value action', exact: true })).toHaveCount(0);
  const value = cookie.getByLabel('Value', { exact: true });
  await expect(value).toHaveAttribute('type', 'password');
  await expect(value).toHaveValue('');
  await cookie.getByRole('textbox', { name: 'Name', exact: true }).fill('new-session');
  await cookie.getByRole('textbox', { name: 'Domain', exact: true }).fill('other.example');
  await value.fill('new-cookie-value');
  await expect(cookie.getByRole('combobox')).toHaveCount(0);
  await cookie.getByRole('button', { name: 'Add cookie', exact: true }).click();
  const stored = page.getByRole('group', { name: 'Crawler cookies 1', exact: true });
  await stored.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(stored.getByRole('combobox', { name: 'Value action', exact: true })).toHaveValue(
    'keep',
  );
  await stored.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('replace');
  await stored.getByLabel('Value replacement', { exact: true }).fill('discarded-cookie-value');
  await stored.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('clear');
  await expect(stored.getByLabel('Value replacement', { exact: true })).toHaveCount(0);
  await expect(cookie).toContainText('new-session');
  await stored.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('keep');
  await stored.getByRole('button', { name: 'Done', exact: true }).click();
  await cookie.getByRole('button', { name: 'Edit', exact: true }).click();

  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await page.getByRole('tab', { name: 'Crawler', exact: true }).click();
  await expect(value).toHaveValue('new-cookie-value');
  await cookie.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(draft.edits).toEqual([
    {
      path: 'crawler.cookies',
      action: 'set',
      value: [
        {
          name: 'session',
          domain: 'example.com',
          path: '/',
          value: { action: 'keep', ref: 'cookie-reference' },
        },
        {
          name: 'new-session',
          domain: 'other.example',
          path: '',
          value: { action: 'replace', value: 'new-cookie-value' },
        },
      ],
    },
  ]);
});

test('an empty crawler proxy accepts a URL directly and offers actions only after saving', async ({
  page,
}) => {
  const state = await crawlerSettingsAPI(page);
  const proxy = state.values.find((field) => field.path === 'crawler.proxy')!;
  proxy.source = { kind: 'default', locked: false, web_selected: false };
  const drafts: any[] = [];
  await page.route('**/api/settings/apply', async (route) => {
    const draft = route.request().postDataJSON();
    drafts.push(draft);
    const edit = draft.edits.find((edit: any) => edit.path === 'crawler.proxy');
    state.revision = `revision-${drafts.length + 1}`;
    const stored =
      edit.action === 'set' && edit.value.action === 'replace' && edit.value.value !== '';
    proxy.value = stored ? { action: 'keep', ref: 'saved-proxy-reference' } : { action: 'clear' };
    proxy.secret_present = stored;
    proxy.source = {
      kind: edit.action === 'reset' ? 'default' : 'web',
      locked: false,
      web_selected: edit.action !== 'reset',
    };
    await route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings?tab=crawler');
  const region = page.getByRole('region', { name: 'Crawler proxy', exact: true });
  const input = region.getByLabel('Crawler proxy', { exact: true });
  const action = region.getByRole('combobox', { name: 'Crawler proxy action', exact: true });
  await expect(input).toBeVisible();
  await expect(input).toHaveValue('');
  await expect(action).toHaveCount(0);
  await input.fill('http://proxy.example:8080');
  await expect(action).toHaveCount(0);
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved and applied');
  expect(drafts[0].edits).toEqual([
    {
      path: 'crawler.proxy',
      action: 'set',
      value: { action: 'replace', value: 'http://proxy.example:8080' },
    },
  ]);
  await expect(action).toHaveValue('keep');
  await action.selectOption('clear');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(input).toBeVisible();
  await expect(input).toHaveValue('');
  await expect(action).toHaveCount(0);
  expect(drafts[1].edits).toEqual([
    {
      path: 'crawler.proxy',
      action: 'set',
      value: { action: 'clear', ref: 'saved-proxy-reference' },
    },
  ]);
});

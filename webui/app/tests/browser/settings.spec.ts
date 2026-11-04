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
  const subtitle = state.values.find((value) => value.path === 'app.subtitle')!;
  subtitle.editable = false;
  subtitle.source = { kind: 'environment', locked: true, web_selected: false };
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
  await expect(page.getByRole('status')).toHaveText('Unsaved changes discarded.');
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
}) => {
  const { state } = await settingsAPI(page);
  let finishValidation = () => {};
  const validationGate = new Promise<void>((resolve) => {
    finishValidation = resolve;
  });
  await page.route('**/api/settings/validate', async (route) => {
    await validationGate;
    await route.fulfill({ json: { revision: state.revision, values: state.values } });
  });
  await page.route('**/api/settings/apply', (route) => {
    state.values[0].value = 'Saved draft';
    return route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  const save = page.getByRole('button', { name: 'Save and apply', exact: true });
  const discard = page.getByRole('button', { name: 'Discard changes', exact: true });
  await title.fill('Saved draft');
  try {
    await save.click();
    await expect(page.getByRole('status')).toHaveText('Validating settings…');
    await expect(save).toBeDisabled();
    await expect(discard).toBeDisabled();
    await expect(title).toBeDisabled();
    await expect(page.getByText('Unsaved', { exact: true })).toBeVisible();
  } finally {
    finishValidation();
  }
  await expect(page.getByRole('status')).toHaveText('Settings saved and applied.');
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
  await expect(region.getByRole('textbox')).toBeDisabled();
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
  await expect(page.getByRole('alert')).toContainText('Reload settings before saving again.');
  await expect(page.getByRole('alert')).not.toContainText('Your draft is still here.');
  await title.fill('Another draft');
  await expect(save).toBeDisabled();
  await expect(page.getByRole('alert')).toContainText('Your draft is still here.');
  await discard.click();
  state.values[0].value = 'Current server title';
  state.revision = 'revision-2';
  await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
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

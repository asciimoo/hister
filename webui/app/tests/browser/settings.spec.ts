// SPDX-License-Identifier: AGPL-3.0-or-later
import { expect, test, type Page } from '@playwright/test';
import { mockAPI } from './fixtures';
import type {
  SettingSchema,
  SettingField,
  SettingsDraft,
  SettingsState,
  SettingsReview,
  PendingAccess,
} from '../../src/lib/settings';

test('settings schema definitions are checked by TypeScript', async () => {
  const scalar = { collection: 'none', sensitivity: 'none' } as const;
  const text = { ...scalar, type: 'string', control: 'text' } satisfies SettingSchema;
  const list = {
    ...scalar,
    type: 'list',
    control: 'list',
    collection: 'replace',
    items: text,
  } satisfies SettingSchema;
  const object = {
    ...scalar,
    type: 'object',
    control: 'object',
    properties: [{ name: 'title', label: 'Title', schema: text }],
  } satisfies SettingSchema;
  // @ts-expect-error Lists require an item schema.
  ({ ...scalar, type: 'list', control: 'list', collection: 'replace' }) satisfies SettingSchema;
  // @ts-expect-error Objects require property schemas.
  ({ ...scalar, type: 'object', control: 'object' }) satisfies SettingSchema;
  // @ts-expect-error A switch edits booleans, not strings.
  ({ ...text, control: 'switch' }) satisfies SettingSchema;
  // @ts-expect-error Secret values require a secret control.
  ({ ...text, sensitivity: 'secret' }) satisfies SettingSchema;
  // @ts-expect-error Collections must explicitly replace their contents.
  ({ ...list, collection: 'none' }) satisfies SettingSchema;
  // @ts-expect-error Scalar settings cannot have item schemas.
  ({ ...text, items: text }) satisfies SettingSchema;
  // @ts-expect-error Select controls require choices.
  ({ ...text, control: 'select' }) satisfies SettingSchema;
  // @ts-expect-error Numeric bounds do not apply to strings.
  ({ ...text, min: 1 }) satisfies SettingSchema;
  // @ts-expect-error Object properties must include their own schema.
  ({ ...object, properties: [{ name: 'title', label: 'Title' }] }) satisfies SettingSchema;

  const ts = await import('typescript');
  const { fileURLToPath } = await import('node:url');
  const options: import('typescript').CompilerOptions = {
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    types: ['svelte', '@sveltejs/kit', '$app/types'],
  };
  const host = ts.createCompilerHost(options);
  const readFile = host.readFile;
  // Match a clean checkout even if obsolete SvelteKit 2 declarations remain locally.
  const legacyTypes = fileURLToPath(new URL('../../.svelte-kit/non-ambient.d.ts', import.meta.url));
  host.readFile = (file) => (file === legacyTypes ? undefined : readFile(file));
  const program = ts.createProgram([fileURLToPath(import.meta.url)], options, host);
  expect(
    ts.formatDiagnostics(ts.getPreEmitDiagnostics(program), {
      getCanonicalFileName: (file) => file,
      getCurrentDirectory: () => process.cwd(),
      getNewLine: () => '\n',
    }),
  ).toBe('');
});

async function settingsAPI(page: Page, config: Record<string, unknown> = {}) {
  const publicConfig: Record<string, unknown> = { canConfigure: true, ...config };
  await mockAPI(page, { config: publicConfig });
  const scalar = { collection: 'none', sensitivity: 'none' } as const;
  const fields = [
    {
      path: 'app.title',
      group: 'Appearance',
      label: 'Title',
      schema: { ...scalar, type: 'string', control: 'text' } satisfies SettingSchema,
      default: 'Hister',
    },
    {
      path: 'app.subtitle',
      group: 'Appearance',
      label: 'Subtitle',
      schema: { ...scalar, type: 'string', control: 'text' } satisfies SettingSchema,
      default: '',
    },
    {
      path: 'app.snippet_length',
      group: 'Search behavior',
      label: 'Snippet length',
      schema: {
        ...scalar,
        type: 'integer',
        control: 'number',
        min: 1,
        max: 10000,
      } satisfies SettingSchema,
      default: 300,
    },
    {
      path: 'app.color_scheme',
      group: 'Appearance',
      label: 'Color scheme',
      schema: {
        ...scalar,
        type: 'string',
        control: 'select',
        choices: ['automatic', 'light', 'dark'],
      } satisfies SettingSchema,
      default: 'automatic',
    },
    {
      path: 'app.show_suggestions',
      group: 'Search behavior',
      label: 'Show suggestions',
      schema: { ...scalar, type: 'boolean', control: 'switch' } satisfies SettingSchema,
      default: false,
    },
    {
      path: 'server.address',
      group: 'Connection',
      label: 'Listen address',
      schema: { ...scalar, type: 'string', control: 'text' } satisfies SettingSchema,
      default: '127.0.0.1:4433',
    },
  ].map((field) => ({
    path: field.path,
    category: field.path.split('.')[0],
    group: field.group,
    label: field.label,
    description: `${field.label} description.`,
    default: field.default,
    schema: field.schema,
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

async function ordinarySettingsAPI(page: Page) {
  const { state, publicConfig } = await settingsAPI(page);
  for (const [path, label] of [
    ['app.display_extractor_config', 'Display extractor configuration'],
    ['app.debug_sql', 'SQL debugging'],
    ['app.profiler', 'Profiler'],
    ['server.metrics', 'Metrics'],
    ['server.expose_version', 'Expose version'],
    ['server.max_batch_body_size', 'Maximum batch size'],
    ['app.search_url', 'Search engine URL'],
  ]) {
    const url = path === 'app.search_url';
    const number = path === 'server.max_batch_body_size';
    state.fields.push({
      path,
      category: path.split('.')[0],
      label,
      description: url
        ? 'Public HTTP or HTTPS destination. Include {query} once. Never include credentials.'
        : `${label} applies after saving.`,
      schema: {
        ...(url
          ? { type: 'string', control: 'url' }
          : number
            ? { type: 'integer', control: 'number', min: 1, max: 8796093022207 }
            : { type: 'boolean', control: 'switch' }),
        sensitivity: 'none',
        collection: 'none',
      },
      effect: 'runtime',
      requires_confirmation: false,
      ...(url ? { default: 'https://google.com/search?q={query}' } : {}),
    });
    state.values.push({
      path,
      value: url ? 'https://existing.example/?q={query}' : number ? 40 : false,
      editable: true,
      secret_present: false,
      source: { kind: 'web', locked: false, web_selected: true },
    });
  }
  return { state, publicConfig };
}

// Keep review failures separate from the pending operation fixtures below.
async function accessSettingsAPI(page: Page, destination = 'http://localhost:5544/settings') {
  const { state, publicConfig } = await settingsAPI(page, {
    authMode: 'user',
    authenticated: true,
  });
  const address = state.fields.find((field) => field.path === 'server.address')!;
  delete address.read_only_reason;
  state.values.find((value) => value.path === address.path)!.editable = true;
  state.fields.push({
    path: 'app.user_handling',
    category: 'app',
    label: 'User handling',
    description: 'Require user accounts to access Hister.',
    schema: { type: 'boolean', control: 'switch', sensitivity: 'none', collection: 'none' },
    default: false,
    effect: 'runtime',
    requires_confirmation: true,
  });
  state.values.push({
    path: 'app.user_handling',
    value: true,
    editable: true,
    source: { kind: 'web', locked: false, web_selected: true },
    secret_present: false,
  });
  const validations: SettingsDraft[] = [];
  const applications: SettingsDraft[] = [];
  await page.route('**/api/settings/validate', async (route) => {
    const draft = route.request().postDataJSON() as SettingsDraft;
    validations.push(draft);
    const changes = draft.edits.map((edit) => {
      const field = state.fields.find((field) => field.path === edit.path)!;
      const active = state.values.find((value) => value.path === edit.path)!;
      const next = edit.action === 'reset' ? field.default : edit.value;
      return {
        path: edit.path,
        before:
          field.schema.sensitivity === 'secret' ? 'Value stored (hidden)' : String(active.value),
        after:
          field.schema.sensitivity === 'secret'
            ? (next as { action: string })?.action === 'replace'
              ? 'Replacement value (hidden)'
              : 'No stored value'
            : String(next),
      };
    });
    const sensitive = changes.some((change) =>
      state.fields.some((field) => field.path === change.path && field.requires_confirmation),
    );
    const consequences = [];
    if (changes.some((change) => change.path === 'app.user_handling' && change.after === 'false'))
      consequences.push(
        'User accounts will no longer be required. Anyone who can reach this instance can use it without signing in.',
      );
    if (changes.some((change) => change.path === 'server.address'))
      consequences.push('The new listener accepts connections beyond this machine.');
    if (changes.some((change) => change.path === 'app.access_token'))
      consequences.push(
        'The instance token will change. Sign in again using the new access policy.',
      );
    await route.fulfill({
      json: {
        revision: state.revision,
        values: state.values,
        ...(sensitive
          ? {
              review: {
                mode: 'access',
                acknowledgment: `review-${validations.length}`,
                changes,
                consequences,
                destination_url: destination,
              },
            }
          : {}),
      },
    });
  });
  await page.route('**/api/settings/apply', async (route) => {
    applications.push(route.request().postDataJSON());
    // Exercise a rejected activation after the review has been accepted.
    await route.fulfill({
      status: 503,
      json: { code: 'unavailable', error: 'Temporary activation is not available yet.' },
    });
  });
  return { state, publicConfig, validations, applications };
}

async function pendingAccessAPI(page: Page) {
  const fixture = await accessSettingsAPI(page, 'http://127.0.0.1:4174/settings');
  const pending: PendingAccess = {
    revision: 'access-revision',
    deadline: new Date(Date.now() + 120_000).toISOString(),
    destination_url: 'http://127.0.0.1:4174/settings',
    confirmation_token: 'private-confirmation-proof',
  };
  const completions: { operation: string; body: unknown }[] = [];
  await page.route('**/api/settings/apply', async (route) => {
    fixture.applications.push(route.request().postDataJSON());
    const { confirmation_token: _, ...status } = pending;
    fixture.state.pending_access = status;
    await route.fulfill({ status: 202, json: pending });
  });
  for (const operation of ['confirm', 'cancel']) {
    await page.route(`**/api/settings/${operation}`, async (route) => {
      completions.push({ operation, body: route.request().postDataJSON() });
      delete fixture.state.pending_access;
      await route.fulfill({
        json: { confirmed: operation === 'confirm', cancelled: operation === 'cancel' },
      });
    });
  }
  return { ...fixture, pending, completions };
}

async function activateAccess(page: Page) {
  await page.goto('/settings');
  await page.getByRole('switch', { name: 'User handling', exact: true }).uncheck();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Confirm sensitive changes' }).click();
}

function addAnonymousSearch(state: SettingsState, enabled = false) {
  const label = 'Allow search without signing in';
  state.fields.push({
    path: 'app.public',
    category: 'app',
    label,
    description: 'Visitors must sign in to search.',
    enabled_description: 'Visitors can search shared documents and open their previews and files.',
    available_when: {
      any_of: ['app.user_handling', 'app.access_token', 'server.proxy_auth_header'],
      message: 'Anyone who can reach this instance can search and make changes.',
      reset_when_unavailable: true,
    },
    schema: { type: 'boolean', control: 'switch', sensitivity: 'none', collection: 'none' },
    default: false,
    effect: 'runtime',
    requires_confirmation: true,
  });
  state.values.push({
    path: 'app.public',
    value: enabled,
    editable: true,
    secret_present: false,
    source: { kind: enabled ? 'web' : 'default', locked: false, web_selected: enabled },
  });
  return label;
}

test('anonymous search follows unsaved credentials and explains who can search', async ({
  page,
}) => {
  const { state, validations } = await accessSettingsAPI(page);
  const label = addAnonymousSearch(state);
  state.values.find((value) => value.path === 'app.user_handling')!.value = false;
  state.fields.push({
    path: 'app.access_token',
    category: 'app',
    label: 'Access token',
    description: '',
    schema: { type: 'string', control: 'secret', sensitivity: 'secret', collection: 'none' },
    effect: 'runtime',
    requires_confirmation: true,
  });
  state.values.push({
    path: 'app.access_token',
    editable: true,
    secret_present: false,
    source: { kind: 'default', locked: false, web_selected: false },
  });
  await page.goto('/settings');
  const toggle = page.getByRole('switch', { name: label, exact: true });
  await expect(
    page.getByText('Anyone who can reach this instance can search and make changes.'),
  ).toBeVisible();
  await expect(toggle).toHaveCount(0);
  const users = page.getByRole('switch', { name: 'User handling' });
  await users.check();
  await expect(toggle).toHaveAccessibleDescription('Visitors must sign in to search.');
  await toggle.check();
  await expect(toggle).toHaveAccessibleDescription(/open their previews and files/);
  await users.uncheck();
  await expect(toggle).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save and apply' })).toBeDisabled();
  await page
    .getByRole('region', { name: 'Access token', exact: true })
    .locator('input')
    .fill('draft-token');
  await expect(toggle).toBeChecked();
  await page.getByRole('button', { name: 'Save and apply' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(validations[0].edits).toEqual(
    expect.arrayContaining([
      {
        path: 'app.access_token',
        action: 'set',
        value: { action: 'replace', value: 'draft-token' },
      },
      { path: 'app.public', action: 'set', value: true },
    ]),
  );
});

test('removing the last authentication method reviews the dependent reset and undo restores it', async ({
  page,
}) => {
  const { state, validations } = await accessSettingsAPI(page);
  const label = addAnonymousSearch(state, true);
  await page.goto('/settings');
  const toggle = page.getByRole('switch', { name: label, exact: true });
  await expect(toggle).toBeChecked();
  await page.getByRole('button', { name: 'Reset User handling' }).click();
  await expect(toggle).toHaveCount(0);
  await page.getByRole('button', { name: 'Save and apply' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText(label);
  expect(validations[0].edits).toEqual([
    { path: 'app.user_handling', action: 'reset' },
    { path: 'app.public', action: 'reset' },
  ]);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('button', { name: 'Undo User handling' }).click();
  await expect(toggle).toBeChecked();
  await expect(page.getByRole('button', { name: 'Save and apply' })).toBeDisabled();
});

test('a dependent reset updates other conditional settings regardless of display order', async ({
  page,
}) => {
  const { state } = await settingsAPI(page);
  state.fields.find((field) => field.path === 'app.subtitle')!.available_when = {
    any_of: ['app.show_suggestions'],
    message: 'Enable suggestions to edit the subtitle.',
  };
  state.fields.find((field) => field.path === 'app.show_suggestions')!.available_when = {
    any_of: ['app.title'],
    message: 'Enter a title to enable suggestions.',
    reset_when_unavailable: true,
  };
  await page.goto('/settings');
  const subtitle = page.getByRole('textbox', { name: 'Subtitle', exact: true });
  await expect(subtitle).toBeVisible();
  await page.getByRole('button', { name: 'Clear Title', exact: true }).click();
  await expect(subtitle).toHaveCount(0);
  await expect(page.getByText('Enable suggestions to edit the subtitle.')).toBeVisible();
  await page.getByRole('button', { name: 'Undo Title', exact: true }).click();
  await expect(subtitle).toBeVisible();
});

for (const operation of ['confirm', 'cancel'] as const) {
  test(`temporary access changes ${operation} only after activation`, async ({ page }) => {
    const { completions } = await pendingAccessAPI(page);
    await activateAccess(page);
    await expect(page.getByRole('heading', { name: 'Confirm working access' })).toBeVisible();
    await expect(page.getByRole('timer')).toContainText(/1:\d\d|2:00/);
    await expect(page.getByText('Settings saved and applied.', { exact: true })).toHaveCount(0);
    expect(completions).toEqual([]);
    const stored = await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    );
    expect(stored).not.toContain('private-confirmation-proof');
    expect(page.url()).not.toContain('private-confirmation-proof');
    await page
      .getByRole('button', {
        name: operation === 'confirm' ? 'Confirm working access' : 'Restore previous settings',
        exact: true,
      })
      .click();
    await expect(page.getByRole('status')).toHaveText(
      operation === 'confirm' ? 'Settings confirmed and saved.' : 'Previous settings restored.',
    );
    expect(completions).toEqual([
      {
        operation,
        body: { revision: 'access-revision', confirmation_token: 'private-confirmation-proof' },
      },
    ]);
  });
}

test('pending access navigation warns and staying preserves the private confirmation code', async ({
  page,
}) => {
  const { publicConfig, completions } = await pendingAccessAPI(page);
  publicConfig.authMode = 'none';
  await activateAccess(page);
  const confirm = page.getByRole('button', { name: 'Confirm working access', exact: true });
  await expect(confirm).toBeEnabled();
  const warnings: string[] = [];
  page.on('dialog', async (dialog) => {
    warnings.push(dialog.message());
    await dialog.dismiss();
  });

  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await expect.poll(() => warnings.length).toBe(1);
  expect(warnings[0]).toContain('confirmation code');
  expect(warnings[0]).toContain('rollback');
  await expect(page).toHaveURL(/\/settings$/);
  await confirm.click();
  await expect(page.getByRole('status')).toHaveText('Settings confirmed and saved.');
  expect(completions).toEqual([
    {
      operation: 'confirm',
      body: { revision: 'access-revision', confirmation_token: 'private-confirmation-proof' },
    },
  ]);

  await page.getByRole('link', { name: 'Rules', exact: true }).click();
  await expect(page).toHaveURL(/\/rules$/);
  expect(warnings).toHaveLength(1);
});

for (const action of ['home', 'reload']) {
  test(`pending access ${action} warns once and staying allows immediate restoration`, async ({
    page,
  }) => {
    const { publicConfig, completions } = await pendingAccessAPI(page);
    publicConfig.authMode = 'none';
    await activateAccess(page);
    const restore = page.getByRole('button', { name: 'Restore previous settings', exact: true });
    await expect(restore).toBeEnabled();
    const warnings: string[] = [];
    page.on('dialog', async (dialog) => {
      warnings.push(dialog.type());
      await dialog.dismiss();
    });

    if (action === 'home')
      await page.getByRole('link', { name: 'Hister logo Hister', exact: true }).click();
    else await page.evaluate(() => window.location.reload());
    await expect.poll(() => warnings).toEqual(['beforeunload']);
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByText(/Without the code, you cannot confirm or restore/)).toBeVisible();
    await restore.click();
    await expect(page.getByRole('status')).toHaveText('Previous settings restored.');
    expect(completions).toEqual([
      {
        operation: 'cancel',
        body: { revision: 'access-revision', confirmation_token: 'private-confirmation-proof' },
      },
    ]);

    await page.getByRole('link', { name: 'Hister logo Hister', exact: true }).click();
    await expect(page).toHaveURL('http://127.0.0.1:4174/');
    expect(warnings).toEqual(['beforeunload']);
  });
}

test('pending access warning allows explicitly leaving without persisting the private code', async ({
  page,
}) => {
  const { publicConfig, completions } = await pendingAccessAPI(page);
  publicConfig.authMode = 'none';
  await activateAccess(page);
  await expect(
    page.getByRole('button', { name: 'Confirm working access', exact: true }),
  ).toBeEnabled();
  const warnings: string[] = [];
  page.on('dialog', async (dialog) => {
    warnings.push(dialog.type());
    await dialog.accept();
  });

  await page.getByRole('link', { name: 'Hister logo Hister', exact: true }).click();
  await expect(page).toHaveURL('http://127.0.0.1:4174/');
  expect(warnings).toEqual(['beforeunload']);
  expect(completions).toEqual([]);
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
  expect(stored).not.toContain('private-confirmation-proof');

  await page.goto('/settings');
  await expect(page.getByLabel('Confirmation code', { exact: true })).toHaveValue('');
  await expect(
    page.getByRole('button', { name: 'Confirm working access', exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole('button', { name: 'Restore previous settings', exact: true }),
  ).toBeDisabled();
});

for (const outcome of ['access confirmation', 'permission denied']) {
  test(`sensitive apply stays locked until it transitions to ${outcome}`, async ({ page }) => {
    await pendingAccessAPI(page);
    let finishApply = () => {};
    const applyGate = new Promise<void>((resolve) => {
      finishApply = resolve;
    });
    await page.route('**/api/settings/apply', async (route) => {
      await applyGate;
      if (outcome === 'permission denied')
        return route.fulfill({ status: 403, json: { error: 'Administrator access revoked.' } });
      return route.fallback();
    });
    try {
      await activateAccess(page);
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(page.getByRole('status')).toHaveText(
        'Applying settings. Service may be briefly interrupted…',
      );
      for (const name of ['Save and apply', 'Discard changes', 'Refresh settings'])
        await expect(page.getByRole('button', { name, exact: true })).toBeDisabled();
      await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toBeDisabled();
    } finally {
      finishApply();
    }
    if (outcome === 'access confirmation') {
      await expect(page.getByRole('heading', { name: 'Confirm working access' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: 'Confirm working access', exact: true }),
      ).toBeEnabled();
    } else {
      await expect(page.getByRole('alert')).toContainText('do not have permission');
      await expect(page.getByRole('heading', { name: 'Confirm working access' })).toHaveCount(0);
    }
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Refresh settings', exact: true })).toHaveCount(
      0,
    );
    await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveCount(0);
  });
}

test('reopened access confirmation requires the original private code when authentication is disabled', async ({
  page,
}) => {
  const { pending, state, publicConfig, completions } = await pendingAccessAPI(page);
  publicConfig.authMode = 'none';
  const { confirmation_token: _, ...status } = pending;
  state.pending_access = status;
  await page.goto('/settings');
  const confirm = page.getByRole('button', { name: 'Confirm working access', exact: true });
  await expect(confirm).toBeDisabled();
  const code = page.getByLabel('Confirmation code', { exact: true });
  await code.pressSequentially('private-confirmation-proof');
  await expect(code).toHaveValue('private-confirmation-proof');
  await confirm.click();
  await expect(page.getByRole('status')).toHaveText('Settings confirmed and saved.');
  expect(completions[0].body).toEqual({
    revision: 'access-revision',
    confirmation_token: 'private-confirmation-proof',
  });
});

test('changed access destinations require explicit navigation without forwarding credentials', async ({
  page,
}) => {
  await page.clock.install();
  const { pending, completions } = await pendingAccessAPI(page);
  pending.destination_url = 'http://localhost:5544/hister/settings';
  const forwarded: string[] = [];
  page.on('request', (request) => {
    if (request.url().startsWith('http://localhost:5544')) forwarded.push(request.url());
  });
  await activateAccess(page);
  const link = page.getByRole('link', { name: /Open new address/ });
  await expect(link).toHaveAttribute('href', pending.destination_url);
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', 'noreferrer');
  await expect(page.getByLabel('Confirmation code', { exact: true })).toHaveAttribute(
    'type',
    'password',
  );
  await expect(
    page.getByRole('button', { name: 'Confirm working access', exact: true }),
  ).toHaveCount(0);
  await page.clock.fastForward(5000);
  expect(forwarded).toEqual([]);
  expect(completions).toEqual([]);
});

test('expired access confirmation reads rollback status without confirming', async ({ page }) => {
  await page.clock.install();
  const { state, completions } = await pendingAccessAPI(page);
  await activateAccess(page);
  await expect(page.getByRole('timer')).toBeVisible();
  // The server, not the browser timer, owns rollback.
  delete state.pending_access;
  await page.clock.fastForward(125_000);
  await expect(page.getByRole('status')).toContainText(
    'No access change is awaiting confirmation.',
  );
  expect(completions).toEqual([]);
  page.on('dialog', (dialog) => dialog.dismiss());
  await page.getByRole('link', { name: 'Hister logo Hister', exact: true }).click();
  await expect(page).toHaveURL('http://127.0.0.1:4174/');
});

test('a lost access confirmation response is checked without resubmitting', async ({ page }) => {
  const { state } = await pendingAccessAPI(page);
  let confirmations = 0;
  await page.route('**/api/settings/confirm', async (route) => {
    confirmations++;
    delete state.pending_access;
    await route.abort('failed');
  });
  await activateAccess(page);
  await page.getByRole('button', { name: 'Confirm working access', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('The response was lost');
  await page.getByRole('button', { name: 'Check status' }).click();
  await expect(page.getByRole('status')).toContainText(
    'No access change is awaiting confirmation.',
  );
  expect(confirmations).toBe(1);
  await expect(page.getByText('Settings confirmed and saved.', { exact: true })).toHaveCount(0);
});

test('a lost access activation response recovers pending status without another apply', async ({
  page,
}) => {
  const { state, pending, applications } = await pendingAccessAPI(page);
  delete pending.confirmation_token;
  pending.authentication = 'credentials';
  await page.route('**/api/settings/apply', async (route) => {
    applications.push(route.request().postDataJSON());
    state.pending_access = pending;
    await route.abort('failed');
  });
  await activateAccess(page);
  await expect(page.getByRole('heading', { name: 'Confirm working access' })).toBeVisible();
  await expect(page.getByRole('timer')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sign in again', exact: true })).toBeVisible();
  expect(applications).toHaveLength(1);
});

test('fresh authentication failures preserve pending access and offer the required OAuth login', async ({
  page,
}) => {
  const { pending } = await pendingAccessAPI(page);
  pending.authentication = 'oauth';
  pending.oauth_provider = 'oidc';
  delete pending.confirmation_token;
  await page.route('**/api/settings/confirm', (route) =>
    route.fulfill({
      status: 403,
      json: {
        code: 'fresh_authentication_required',
        error: 'Sign in again using the temporary configuration before confirming access.',
      },
    }),
  );
  await activateAccess(page);
  await page.getByRole('button', { name: 'Confirm working access', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Sign in again');
  await expect(page.getByRole('timer')).toBeVisible();
  const login = page.getByRole('link', { name: 'Sign in with oidc' });
  await expect(login).toHaveAttribute('href', '/api/oauth?provider=oidc&return_to=%2Fsettings');
  await page.route('**/api/oauth?*', (route) =>
    route.fulfill({ contentType: 'text/plain', body: 'New OAuth flow started' }),
  );
  const warning = page.waitForEvent('dialog').then((dialog) => dialog.accept());
  await login.click();
  await warning;
  await expect(page.getByText('New OAuth flow started')).toBeVisible();
});

test('access status refresh clears login errors and resumes rollback polling', async ({ page }) => {
  await page.clock.install();
  const { pending, state, completions, applications } = await pendingAccessAPI(page);
  pending.authentication = 'credentials';
  delete pending.confirmation_token;
  let confirmations = 0;
  await page.route('**/api/settings/confirm', (route) => {
    confirmations++;
    return route.fulfill({
      status: 403,
      json: {
        code: 'fresh_authentication_required',
        error: 'Sign in again using the temporary configuration before confirming access.',
      },
    });
  });
  await activateAccess(page);
  await page.getByRole('button', { name: 'Confirm working access', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Sign in again');

  await page.getByRole('button', { name: 'Check status', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  // Reading status does not prove fresh authentication or confirm the change.
  await expect(page.getByRole('link', { name: 'Sign in again', exact: true })).toBeVisible();
  await expect(page.getByRole('timer')).toBeVisible();
  delete state.pending_access;
  await page.clock.fastForward(3000);
  await expect(page.getByRole('status')).toContainText(
    'No access change is awaiting confirmation.',
  );
  expect(confirmations).toBe(1);
  expect(completions).toEqual([]);
  expect(applications).toHaveLength(1);
});

for (const denial of ['configuration', 401, 403]) {
  test(`access polling after ${denial} denial resumes only after a complete status read`, async ({
    page,
  }) => {
    await page.clock.install();
    const { state, publicConfig, completions, applications } = await pendingAccessAPI(page);
    await activateAccess(page);
    const refresh = page.getByRole('button', { name: 'Check status', exact: true });
    await expect(refresh).toBeEnabled();
    publicConfig.canConfigure = denial !== 'configuration';
    let status = typeof denial === 'number' ? denial : 200;
    let reads = 0;
    await page.route('**/api/settings', (route) => {
      reads++;
      return route.fulfill({
        status,
        json: status === 200 ? state : { error: 'Settings unavailable.' },
      });
    });
    await refresh.click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Sign in again', exact: true })).toBeVisible();

    // Public configuration can succeed while the protected settings read still fails.
    publicConfig.canConfigure = true;
    status = 503;
    await refresh.click();
    await expect(page.getByRole('alert')).toContainText('Could not read the pending change');
    await expect(refresh).toBeEnabled();
    const readsBefore = reads;
    await page.clock.fastForward(3000);
    expect(reads).toBe(readsBefore);

    status = 200;
    await refresh.click();
    await expect(page.getByRole('alert')).toHaveCount(0);
    await expect(page.getByRole('link', { name: 'Sign in again', exact: true })).toHaveCount(0);
    delete state.pending_access;
    await page.clock.fastForward(3000);
    await expect(page.getByRole('status')).toContainText(
      'No access change is awaiting confirmation.',
    );
    expect(completions).toEqual([]);
    expect(applications).toHaveLength(1);
  });
}

test('sensitive review requires explicit consent for a mixed draft before any apply', async ({
  page,
}) => {
  const { validations, applications } = await accessSettingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Private archive');
  await page.getByRole('switch', { name: 'User handling', exact: true }).uncheck();
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  await page.getByRole('textbox', { name: 'Listen address', exact: true }).fill('0.0.0.0:5544');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeFocused();
  await expect(dialog.getByRole('heading', { name: 'Review sensitive changes' })).toBeInViewport();
  await expect(dialog).toContainText('Private archive');
  await expect(dialog).toContainText('User handling');
  await expect(dialog).toContainText('127.0.0.1:4433');
  await expect(dialog).toContainText('0.0.0.0:5544');
  await expect(dialog).toContainText('without signing in');
  await expect(dialog).toContainText('beyond this machine');
  await expect(dialog).toContainText('http://localhost:5544/settings');
  await expect(dialog).toHaveJSProperty(
    'scrollWidth',
    await dialog.evaluate((node) => node.clientWidth),
  );
  expect(applications).toEqual([]);
  await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Temporary activation is not available yet.');
  expect(applications).toEqual([{ ...validations[0], acknowledgment: 'review-1' }]);
  await expect(page.getByText('Settings saved and applied.', { exact: true })).toHaveCount(0);
});

for (const dismissal of ['Cancel', 'Escape']) {
  test(`sensitive review ${dismissal} keeps the draft and edits require a new review`, async ({
    page,
  }) => {
    const { validations, applications } = await accessSettingsAPI(page);
    await page.goto('/settings');
    await page.getByRole('switch', { name: 'User handling', exact: true }).uncheck();
    const save = page.getByRole('button', { name: 'Save and apply', exact: true });
    await save.click();
    const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
    await expect(dialog).toBeVisible();
    if (dismissal === 'Cancel')
      // Enter must activate the initially focused Cancel action, never apply.
      await page.keyboard.press('Enter');
    else await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(save).toBeFocused();
    await expect(
      page.getByRole('switch', { name: 'User handling', exact: true }),
    ).not.toBeChecked();
    expect(applications).toEqual([]);
    await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Revised after review');
    await save.click();
    await expect(dialog).toContainText('Revised after review');
    expect(validations).toHaveLength(2);
    expect(applications).toEqual([]);
    await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText(
      'Temporary activation is not available yet.',
    );
    expect(applications).toEqual([{ ...validations[1], acknowledgment: 'review-2' }]);
    expect(applications[0].edits).toContainEqual({
      path: 'app.title',
      action: 'set',
      value: 'Revised after review',
    });
  });
}

test('sensitive review includes effective Reset and Clear changes with hidden secrets', async ({
  page,
}) => {
  const { state, validations, applications } = await accessSettingsAPI(page);
  const label = addAnonymousSearch(state, true);
  state.fields.push({
    path: 'app.access_token',
    category: 'app',
    label: 'Instance token',
    description: 'Access token.',
    schema: { type: 'string', control: 'secret', sensitivity: 'secret', collection: 'none' },
    effect: 'runtime',
    requires_confirmation: true,
  });
  state.values.push({
    path: 'app.access_token',
    value: { action: 'keep', ref: 'stored-token-reference' },
    editable: true,
    secret_present: true,
    source: { kind: 'web', locked: false, web_selected: true },
  });
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Reset User handling', exact: true }).click();
  const anonymousSearch = page.getByRole('switch', { name: label, exact: true });
  await expect(anonymousSearch).toBeChecked();
  await page.getByRole('button', { name: 'Clear Instance token', exact: true }).click();
  await expect(anonymousSearch).toHaveCount(0);
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
  await expect(dialog).toContainText('Before: true');
  await expect(dialog).toContainText('After: false');
  await expect(dialog).toContainText('without signing in');
  await expect(dialog).toContainText('Value stored (hidden)');
  await expect(dialog).toContainText('No stored value');
  await expect(dialog).not.toContainText('stored-token-reference');
  expect(validations[0].edits).toEqual([
    { path: 'app.user_handling', action: 'reset' },
    { path: 'app.access_token', action: 'set', value: { action: 'clear' } },
    { path: 'app.public', action: 'reset' },
  ]);
  expect(applications).toEqual([]);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page
    .getByRole('region', { name: 'Instance token', exact: true })
    .locator('input[type="password"]')
    .fill('replacement-private-token');
  await expect(anonymousSearch).toBeChecked();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(dialog).toContainText('Replacement value (hidden)');
  await expect(dialog).not.toContainText('replacement-private-token');
  await expect(dialog).not.toContainText('stored-token-reference');
  const stored = await page.evaluate(() => ({
    local: { ...localStorage },
    session: { ...sessionStorage },
  }));
  expect(JSON.stringify(stored)).not.toMatch(
    /replacement-private-token|stored-token-reference|review-2/,
  );
  await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Temporary activation is not available yet.');
  expect(applications).toEqual([{ ...validations[1], acknowledgment: 'review-2' }]);
});

for (const action of ['Reset', 'Clear']) {
  test(`sensitive review shows the effective listener after ${action}`, async ({ page }) => {
    const { state, applications } = await accessSettingsAPI(page);
    const active = state.values.find((value) => value.path === 'server.address')!;
    active.value = '0.0.0.0:5544';
    active.source = { kind: 'web', locked: false, web_selected: true };
    let validated: SettingsDraft | undefined;
    await page.route('**/api/settings/validate', (route) => {
      validated = route.request().postDataJSON();
      return route.fulfill({
        json: {
          revision: state.revision,
          values: state.values,
          review: {
            acknowledgment: 'local-listener-review',
            changes: [{ path: 'server.address', before: '0.0.0.0:5544', after: '127.0.0.1:4433' }],
            consequences: ['Only connections from this machine will reach the new listener.'],
            destination_url: 'http://localhost:4433/settings',
          },
        },
      });
    });
    await page.goto('/settings?tab=server');
    await page.getByRole('button', { name: `${action} Listen address`, exact: true }).click();
    await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
    await expect(dialog).toContainText('Before: 0.0.0.0:5544');
    await expect(dialog).toContainText('After: 127.0.0.1:4433');
    await expect(dialog).toContainText('Only connections from this machine');
    await expect(dialog).toContainText('http://localhost:4433/settings');
    expect(validated?.edits).toEqual([
      action === 'Reset'
        ? { path: 'server.address', action: 'reset' }
        : { path: 'server.address', action: 'set', value: '' },
    ]);
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    expect(applications).toEqual([]);
  });
}

test('a rejected sensitive acknowledgment retains the draft and requires a fresh review', async ({
  page,
}) => {
  const { state, validations } = await accessSettingsAPI(page);
  const applications: SettingsDraft[] = [];
  await page.route('**/api/settings/apply', (route) => {
    applications.push(route.request().postDataJSON());
    state.revision = 'revision-2';
    return route.fulfill({
      status: 409,
      json: { code: 'stale_revision', error: 'Review the current settings again.' },
    });
  });
  await page.goto('/settings');
  await page.getByRole('switch', { name: 'User handling', exact: true }).uncheck();
  const save = page.getByRole('button', { name: 'Save and apply', exact: true });
  await save.click();
  const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
  await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(save).toBeDisabled();
  await expect(page.getByRole('switch', { name: 'User handling', exact: true })).not.toBeChecked();
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await save.click();
  await expect(dialog).toBeVisible();
  expect(applications).toEqual([{ ...validations[0], acknowledgment: 'review-1' }]);
  expect(validations).toHaveLength(2);
  expect(validations[1].revision).toBe('revision-2');
  await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
  await expect(save).toBeDisabled();
  expect(applications[1]).toEqual({ ...validations[1], acknowledgment: 'review-2' });
});

test('ordinary saves do not open a sensitive review or send an acknowledgment', async ({
  page,
}) => {
  const { applications } = await accessSettingsAPI(page);
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Ordinary edit');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Temporary activation is not available yet.');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(applications).toEqual([
    {
      revision: 'revision-1',
      edits: [{ path: 'app.title', action: 'set', value: 'Ordinary edit' }],
    },
  ]);
});

for (const decision of ['not required', 'required', 'stale'] as const) {
  test(`conditional confirmation ${decision} for disabling diagnostics`, async ({ page }) => {
    const { state } = await ordinarySettingsAPI(page);
    state.fields.find((field) => field.path === 'app.profiler')!.requires_confirmation = true;
    state.values.find((value) => value.path === 'app.profiler')!.value = true;
    const applications: SettingsDraft[] = [];
    await page.route('**/api/settings/validate', (route) =>
      route.fulfill({
        json: {
          revision: decision === 'stale' ? 'old-revision' : state.revision,
          values: state.values,
          requires_confirmation: decision === 'required',
        },
      }),
    );
    await page.route('**/api/settings/apply', (route) => {
      applications.push(route.request().postDataJSON());
      state.values.find((value) => value.path === 'app.profiler')!.value = false;
      return route.fulfill({ json: { saved: true } });
    });
    await page.goto('/settings');
    await page.getByRole('switch', { name: 'Profiler', exact: true }).uncheck();
    await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    if (decision === 'not required') {
      await expect(page.getByText('Settings saved and applied.', { exact: true })).toBeVisible();
      expect(applications).toEqual([
        { revision: 'revision-1', edits: [{ path: 'app.profiler', action: 'set', value: false }] },
      ]);
    } else {
      await expect(page.getByRole('alert')).toContainText(
        decision === 'stale' ? 'Refresh settings before saving again.' : 'No changes were applied.',
      );
      expect(applications).toEqual([]);
    }
  });
}

test('acknowledgment applies diagnostics without an access confirmation step', async ({ page }) => {
  const { state } = await ordinarySettingsAPI(page);
  state.fields.find((field) => field.path === 'app.profiler')!.requires_confirmation = true;
  const applications: SettingsDraft[] = [];
  await page.route('**/api/settings/validate', (route) =>
    route.fulfill({
      json: {
        revision: state.revision,
        values: state.values,
        requires_confirmation: true,
        review: {
          mode: 'acknowledge',
          acknowledgment: 'diagnostics-review',
          changes: [{ path: 'app.profiler', before: 'false', after: 'true' }],
          consequences: ['Runtime profiles may contain private data.'],
        },
      },
    }),
  );
  await page.route('**/api/settings/apply', (route) => {
    applications.push(route.request().postDataJSON());
    state.values.find((value) => value.path === 'app.profiler')!.value = true;
    return route.fulfill({ json: { saved: true } });
  });
  await page.goto('/settings');
  await page.getByRole('switch', { name: 'Profiler', exact: true }).check();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review sensitive changes' });
  await expect(dialog).toContainText('private data');
  expect(applications).toEqual([]);
  await dialog.getByRole('button', { name: 'Confirm sensitive changes', exact: true }).click();
  await expect(page.getByText('Settings saved and applied.', { exact: true })).toBeVisible();
  expect(applications).toEqual([
    {
      revision: 'revision-1',
      edits: [{ path: 'app.profiler', action: 'set', value: true }],
      acknowledgment: 'diagnostics-review',
    },
  ]);
});

for (const missing of ['review', 'acknowledgment', 'changes', 'consequences', 'revision']) {
  test(`sensitive review cannot apply without a matching ${missing}`, async ({ page }) => {
    const { state, applications } = await accessSettingsAPI(page);
    await page.route('**/api/settings/validate', (route) => {
      const review: Partial<SettingsReview> = {
        acknowledgment: 'review-1',
        changes: [{ path: 'app.user_handling', before: 'Enabled', after: 'Disabled' }],
        consequences: ['Anyone who can reach this instance can use it without signing in.'],
      };
      if (missing === 'acknowledgment' || missing === 'changes' || missing === 'consequences')
        delete review[missing];
      return route.fulfill({
        json: {
          revision: missing === 'revision' ? 'stale-revision' : state.revision,
          values: state.values,
          ...(missing === 'review' ? {} : { review }),
        },
      });
    });
    await page.goto('/settings');
    await page.getByRole('switch', { name: 'User handling', exact: true }).uncheck();
    await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText(
      missing === 'revision' ? 'Refresh settings before saving again.' : 'No changes were applied.',
    );
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(
      page.getByRole('switch', { name: 'User handling', exact: true }),
    ).not.toBeChecked();
    expect(applications).toEqual([]);
  });
}

test('ordinary App and Server controls save one draft and refresh public settings', async ({
  page,
}) => {
  const { state, publicConfig } = await ordinarySettingsAPI(page);
  let saved: any;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    Object.assign(publicConfig, {
      searchUrl: 'https://search.example/?q={query}',
      maxBatchBodyBytes: 1048576,
    });
    for (const edit of saved.edits) {
      state.values.find((value) => value.path === edit.path)!.value = edit.value;
    }
    state.revision = 'revision-2';
    await route.fulfill({ json: {} });
  });
  await page.goto('/settings');
  const searchURL = page.getByRole('textbox', { name: 'Search engine URL', exact: true });
  await expect(searchURL).toHaveAttribute('type', 'url');
  await expect(searchURL).toHaveValue('https://existing.example/?q={query}');
  await expect(
    page.getByRole('combobox', { name: 'Search engine URL action', exact: true }),
  ).toHaveCount(0);
  await searchURL.fill('https://search.example/?q={query}');
  for (const name of ['Display extractor configuration', 'SQL debugging', 'Profiler'])
    await page.getByRole('switch', { name, exact: true }).check();
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  const limit = page.getByRole('spinbutton', { name: 'Maximum batch size', exact: true });
  await expect(limit).toHaveAttribute('min', '1');
  await limit.fill('1');
  for (const name of ['Metrics', 'Expose version'])
    await page.getByRole('switch', { name, exact: true }).check();
  await expect(page.locator('body')).toHaveJSProperty(
    'scrollWidth',
    await page.locator('body').evaluate((node) => node.clientWidth),
  );
  const refresh = page.waitForResponse('**/api/config');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  expect((await refresh).ok()).toBe(true);
  await expect(page.getByText('Settings saved and applied.')).toBeVisible();
  expect(saved.edits).toHaveLength(7);
  expect(saved.edits).toEqual(
    expect.arrayContaining([
      {
        path: 'app.search_url',
        action: 'set',
        value: 'https://search.example/?q={query}',
      },
      { path: 'server.max_batch_body_size', action: 'set', value: 1 },
      { path: 'server.metrics', action: 'set', value: true },
    ]),
  );
  await page.getByRole('tab', { name: 'App', exact: true }).click();
  await expect(searchURL).toHaveValue('https://search.example/?q={query}');
});

test('ordinary App and Server controls retain invalid drafts and respect reset and source locks', async ({
  page,
}) => {
  const { state } = await ordinarySettingsAPI(page);
  const metrics = state.values.find((value) => value.path === 'server.metrics')!;
  metrics.editable = false;
  metrics.source = { kind: 'file', name: '/config.yml', locked: true, web_selected: true };
  let draft: any;
  await page.route('**/api/settings/validate', async (route) => {
    draft = route.request().postDataJSON();
    await route.fulfill({
      status: 422,
      json: {
        code: 'invalid_settings',
        errors: [
          {
            path: 'app.search_url',
            code: 'invalid_value',
            message: 'Include {query} exactly once in the URL path or query.',
          },
        ],
      },
    });
  });
  await page.goto('/settings');
  const replacement = page.getByRole('textbox', { name: 'Search engine URL', exact: true });
  await replacement.fill('https://search.example/');
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(
    page.getByText('Include {query} exactly once in the URL path or query.', { exact: true }),
  ).toBeVisible();
  await expect(replacement).toHaveValue('https://search.example/');
  await page.getByRole('button', { name: 'Clear Search engine URL', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  expect(draft.edits).toContainEqual({
    path: 'app.search_url',
    action: 'set',
    value: '',
  });
  await page.getByRole('button', { name: 'Reset Search engine URL', exact: true }).click();
  await expect(replacement).toHaveValue('https://google.com/search?q={query}');
  await page.getByRole('tab', { name: 'Server', exact: true }).click();
  const region = page.getByRole('region', { name: 'Metrics', exact: true });
  await expect(region.getByRole('switch')).toHaveCount(0);
  await expect(region.getByRole('button', { name: 'Reset Metrics', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reset Maximum batch size', exact: true }).click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  expect(draft.edits).toEqual(
    expect.arrayContaining([
      { path: 'app.search_url', action: 'reset' },
      { path: 'server.max_batch_body_size', action: 'reset' },
    ]),
  );
});

test('a locked search engine URL stays visible without secret controls', async ({ page }) => {
  const { state } = await ordinarySettingsAPI(page);
  const value = state.values.find((value) => value.path === 'app.search_url')!;
  value.editable = false;
  value.source = { kind: 'file', name: '/config.yml', locked: true, web_selected: false };
  await page.goto('/settings');
  const field = page.getByRole('region', { name: 'Search engine URL', exact: true });
  await expect(field).toContainText('https://existing.example/?q={query}');
  await expect(field).toContainText('Locked');
  await expect(field.getByRole('textbox')).toHaveCount(0);
  await expect(field.getByRole('combobox')).toHaveCount(0);
  await expect(
    field.getByRole('button', { name: 'Reset Search engine URL', exact: true }),
  ).toHaveCount(0);
});

async function patternSettingsAPI(page: Page) {
  const { state } = await settingsAPI(page);
  state.fields.push({
    path: 'sensitive_content_patterns',
    category: 'sensitive_content_patterns',
    label: 'Sensitive content patterns',
    description:
      'Named regular expressions that block matching documents during subsequent indexing. Clear disables these checks; Reset restores inherited patterns.',
    schema: {
      type: 'dictionary',
      control: 'dictionary',
      collection: 'replace',
      sensitivity: 'none',
      key_label: 'Pattern name',
      value_label: 'Regular expression',
      max_items: 256,
      items: {
        type: 'string',
        control: 'text',
        collection: 'none',
        sensitivity: 'none',
        min_length: 1,
        max_length: 4096,
      },
    },
    default: { private_key: '-----BEGIN OPENSSH PRIVATE KEY-----' },
    effect: 'runtime',
    requires_confirmation: false,
  });
  state.values.push({
    path: 'sensitive_content_patterns',
    value: { private: 'private-[0-9]+' },
    editable: true,
    source: { kind: 'web', locked: false, web_selected: true },
    secret_present: false,
  });
  return state;
}

test('sensitive patterns use named expression rows and save with ordinary App edits', async ({
  page,
}) => {
  const state = await patternSettingsAPI(page);
  let saved: any;
  await page.route('**/api/settings/apply', async (route) => {
    saved = route.request().postDataJSON();
    for (const edit of saved.edits)
      state.values.find((value) => value.path === edit.path)!.value = edit.value;
    state.revision = 'revision-2';
    await route.fulfill({ json: {} });
  });
  await page.goto('/settings');
  await page.getByRole('textbox', { name: 'Subtitle', exact: true }).fill('Pattern settings');
  await page.getByRole('tab', { name: 'Sensitive content patterns', exact: true }).click();
  const patterns = page.getByRole('region', { name: 'Sensitive content patterns', exact: true });
  await patterns.getByRole('button', { name: 'Add pattern', exact: true }).click();
  const editor = patterns.getByRole('group', { name: 'Sensitive content patterns 2', exact: true });
  await expect(editor.getByRole('textbox', { name: 'Pattern name', exact: true })).toBeFocused();
  await editor.getByRole('textbox', { name: 'Pattern name', exact: true }).fill('account');
  await editor
    .getByRole('textbox', { name: 'Regular expression', exact: true })
    .fill('account-[0-9]+');
  await editor.getByRole('button', { name: 'Add pattern', exact: true }).click();
  await expect(patterns.getByText('account-[0-9]+', { exact: true })).toBeVisible();
  await expect(page.locator('body')).toHaveJSProperty(
    'scrollWidth',
    await page.locator('body').evaluate((node) => node.clientWidth),
  );
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect(page.getByText('Settings saved and applied.')).toBeVisible();
  expect(saved.edits).toEqual(
    expect.arrayContaining([
      { path: 'app.subtitle', action: 'set', value: 'Pattern settings' },
      {
        path: 'sensitive_content_patterns',
        action: 'set',
        value: { private: 'private-[0-9]+', account: 'account-[0-9]+' },
      },
    ]),
  );
});

test('sensitive patterns retain invalid drafts and distinguish clear, reset and source locks', async ({
  page,
}) => {
  const state = await patternSettingsAPI(page);
  let draft: any;
  await page.route('**/api/settings/apply', (route) =>
    route.fulfill({
      status: 422,
      json: { code: 'invalid_draft', error: 'Save failed; draft retained.' },
    }),
  );
  await page.route('**/api/settings/validate', async (route) => {
    draft = route.request().postDataJSON();
    const edit = draft.edits.find((edit: any) => edit.path === 'sensitive_content_patterns');
    if (edit?.value?.broken)
      await route.fulfill({
        status: 422,
        json: {
          code: 'invalid_draft',
          errors: [
            {
              path: 'sensitive_content_patterns',
              code: 'invalid_value',
              message: 'Use valid regular expressions that can be combined.',
            },
          ],
        },
      });
    else await route.fulfill({ json: { revision: state.revision, values: state.values } });
  });
  await page.goto('/settings?tab=sensitive_content_patterns');
  const patterns = page.getByRole('region', { name: 'Sensitive content patterns', exact: true });
  await patterns.getByRole('button', { name: 'Add pattern', exact: true }).click();
  const editor = patterns.getByRole('group', { name: 'Sensitive content patterns 2', exact: true });
  const name = editor.getByRole('textbox', { name: 'Pattern name', exact: true });
  const expression = editor.getByRole('textbox', { name: 'Regular expression', exact: true });
  await name.fill('broken');
  await expression.fill('[');
  await editor.getByRole('button', { name: 'Add pattern', exact: true }).click();
  await expect(
    patterns.getByText('Use valid regular expressions that can be combined.'),
  ).toBeVisible();
  await expect(expression).toHaveValue('[');
  await name.fill('private');
  await editor.getByRole('button', { name: 'Add pattern', exact: true }).click();
  await expect(editor.getByRole('alert')).toContainText('Names must be unique');
  await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
  await patterns
    .getByRole('button', { name: 'Clear Sensitive content patterns', exact: true })
    .click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect
    .poll(() => draft.edits)
    .toEqual([{ path: 'sensitive_content_patterns', action: 'set', value: {} }]);
  await expect(page.getByText('Save failed; draft retained.', { exact: true })).toBeVisible();
  await patterns
    .getByRole('button', { name: 'Reset Sensitive content patterns', exact: true })
    .click();
  await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
  await expect
    .poll(() => draft.edits)
    .toEqual([{ path: 'sensitive_content_patterns', action: 'reset' }]);
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  Object.assign(
    state.values.find((value) => value.path === 'sensitive_content_patterns')!,
    {
      editable: false,
      reason: 'Controlled by the main configuration.',
      source: { kind: 'file', name: '/config.yml', locked: true, web_selected: true },
    },
  );
  state.revision = 'revision-locked';
  await page.getByRole('button', { name: 'Refresh settings', exact: true }).click();
  await expect(patterns.getByRole('button', { name: 'Add pattern', exact: true })).toHaveCount(0);
  await expect(
    patterns.getByRole('button', { name: 'Clear Sensitive content patterns', exact: true }),
  ).toHaveCount(0);
  await patterns
    .getByRole('button', {
      name: 'Locked: Source information for Sensitive content patterns',
      exact: true,
    })
    .click();
  await expect(page.getByRole('tooltip')).toContainText('Main configuration');
});

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

for (const kind of ['shortcut', 'pattern'] as const) {
  test(`${kind} rows have compact accessible icon actions with keyboard editing and undo`, async ({
    page,
  }, testInfo) => {
    if (kind === 'shortcut') await hotkeySettingsAPI(page);
    else await patternSettingsAPI(page);
    const label = kind === 'shortcut' ? 'Web keyboard shortcuts' : 'Sensitive content patterns';
    await page.goto(
      `/settings?tab=${kind === 'shortcut' ? 'hotkeys' : 'sensitive_content_patterns'}`,
    );
    const row = page.getByRole('group', { name: `${label} 1`, exact: true });
    const edit = row.getByRole('button', { name: 'Edit', exact: true });
    const remove = row.getByRole('button', { name: `Remove ${label} item 1`, exact: true });
    await expect(edit).toHaveText('');
    await expect(remove).toHaveText('');
    const title = row.getByText(kind === 'shortcut' ? 'alt+x' : 'private', { exact: true });
    const titleBox = (await title.boundingBox())!;
    const editBox = (await edit.boundingBox())!;
    const removeBox = (await remove.boundingBox())!;
    expect(editBox.x).toBeGreaterThan(titleBox.x + titleBox.width);
    expect(removeBox.x).toBeGreaterThan(editBox.x);
    expect(titleBox.y + titleBox.height / 2).toBeGreaterThanOrEqual(editBox.y);
    expect(titleBox.y + titleBox.height / 2).toBeLessThanOrEqual(editBox.y + editBox.height);
    expect(editBox.width).toBeGreaterThanOrEqual(36);
    expect(editBox.height).toBeGreaterThanOrEqual(36);
    await page.screenshot({ path: testInfo.outputPath(`${kind}-rows.png`) });
    if (!testInfo.project.use.isMobile) {
      await remove.hover();
      await expect(
        page.getByRole('tooltip', { name: `Remove ${kind} from draft`, exact: true }),
      ).toBeVisible();
      await page.mouse.move(0, 0);
    }
    await edit.focus();
    await expect(page.getByRole('tooltip', { name: `Edit ${kind}`, exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.keyboard.press('Enter');
    await expect(row.getByRole('textbox').first()).toBeFocused();
    await row.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(edit).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(remove).toBeFocused();
    await expect(
      page.getByRole('tooltip', { name: `Remove ${kind} from draft`, exact: true }),
    ).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(row).toHaveCount(0);
    await page.getByRole('button', { name: 'Undo removal', exact: true }).click();
    await expect(row).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
  });
}

test('pattern rows wrap long names and expressions without hiding their actions', async ({
  page,
}, testInfo) => {
  const state = await patternSettingsAPI(page);
  const name = 'private_account_identifier_'.repeat(4);
  const expression = `(?i)${'private-credential-'.repeat(12)}[0-9]+`;
  state.values.find((value) => value.path === 'sensitive_content_patterns')!.value = {
    [name]: expression,
  };
  await page.goto('/settings?tab=sensitive_content_patterns');
  const row = page.getByRole('group', { name: 'Sensitive content patterns 1', exact: true });
  await expect(row.getByText(name, { exact: true })).toBeVisible();
  await expect(row.getByText(expression, { exact: true })).toBeVisible();
  await expect(row.getByRole('button', { name: 'Edit', exact: true })).toBeInViewport();
  await expect(
    row.getByRole('button', { name: 'Remove Sensitive content patterns item 1', exact: true }),
  ).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({ path: testInfo.outputPath('long-pattern-row.png') });
});

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
  const ordinary = { collection: 'none', sensitivity: 'none' } as const;
  const secret = {
    type: 'string',
    control: 'secret',
    collection: 'none',
    sensitivity: 'secret',
  } satisfies SettingSchema;
  const definitions: (Pick<SettingField, 'path' | 'label' | 'schema'> & {
    value: unknown;
    effect?: string;
    readOnly?: string;
  })[] = [
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

for (const [name, schema] of Object.entries({
  'unknown type': { type: 'future', control: 'text', collection: 'none', sensitivity: 'none' },
  'mismatched control': {
    type: 'string',
    control: 'switch',
    collection: 'none',
    sensitivity: 'none',
  },
  'unprotected secret': {
    type: 'string',
    control: 'text',
    collection: 'none',
    sensitivity: 'secret',
  },
  'missing list items': {
    type: 'list',
    control: 'list',
    collection: 'replace',
    sensitivity: 'none',
  },
  'missing object properties': {
    type: 'object',
    control: 'object',
    collection: 'none',
    sensitivity: 'none',
  },
})) {
  test(`unsupported settings metadata stays read only: ${name}`, async ({ page }) => {
    const { state } = await settingsAPI(page);
    const path = schema.type === 'list' ? 'indexer.directories' : state.fields[0].path;
    // Exercise an untrusted response without claiming it satisfies SettingSchema.
    await page.route('**/api/settings', (route) =>
      route.fulfill({
        json: {
          ...state,
          fields: [{ ...state.fields[0], path, schema }],
          values: [
            {
              ...state.values[0],
              path,
              value: schema.type === 'list' ? [] : 'stored-value',
              secret_present: true,
            },
          ],
        },
      }),
    );
    await page.goto('/settings');
    const field = page.getByRole('region', { name: 'Title', exact: true });
    for (const role of ['textbox', 'combobox', 'spinbutton', 'switch'] as const)
      await expect(field.getByRole(role)).toHaveCount(0);
    await expect(field).toContainText('Read only');
    await expect(page.getByRole('button', { name: 'Save and apply', exact: true })).toBeDisabled();
    if (schema.sensitivity === 'secret') {
      await expect(field).toContainText('Value stored (hidden)');
      await expect(field).not.toContainText('stored-value');
    }
  });
}

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
      schema: { type: 'string', control: 'secret', collection: 'none', sensitivity: 'secret' },
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
    schema: { type: 'string', control: 'text', collection: 'none', sensitivity: 'none' },
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
  const text = {
    type: 'string',
    control: 'text',
    collection: 'none',
    sensitivity: 'none',
  } satisfies SettingSchema;
  const toggle = {
    type: 'boolean',
    control: 'switch',
    collection: 'none',
    sensitivity: 'none',
  } satisfies SettingSchema;
  const list = {
    type: 'list',
    control: 'list',
    collection: 'replace',
    sensitivity: 'none',
    items: text,
  } satisfies SettingSchema;
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
  ] satisfies Pick<SettingField, 'path' | 'label' | 'schema' | 'default'>[]) {
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
  const text = {
    type: 'string',
    control: 'text',
    collection: 'none',
    sensitivity: 'none',
  } satisfies SettingSchema;
  const secret = { ...text, control: 'secret', sensitivity: 'secret' } satisfies SettingSchema;
  const number = {
    type: 'integer',
    control: 'number',
    collection: 'none',
    sensitivity: 'none',
    min: 0,
  } satisfies SettingSchema;
  const fields: (Pick<SettingField, 'path' | 'label' | 'schema'> & { value: unknown })[] = [
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

for (const type of ['list', 'dictionary'] as const) {
  test(`unrecognized ${type} collections use schema summaries and keep secret drafts hidden`, async ({
    page,
  }) => {
    const state = await crawlerSettingsAPI(page);
    const field = state.fields.find((field) => field.path === 'crawler.cookies')!;
    const active = state.values.find((value) => value.path === field.path)!;
    field.path = active.path = 'crawler.custom_entries';
    field.label = 'Custom entries';
    const schema = field.schema;
    if (schema.type !== 'list' || schema.items.type !== 'object')
      throw new Error('Expected cookie item schema.');
    if (type === 'dictionary') field.schema = { ...schema, type, control: type };
    schema.items.properties.find((property) => property.name === 'name')!.label = 'Display name';
    schema.items.properties.find((property) => property.name === 'domain')!.label = 'Host';
    if (type === 'dictionary') active.value = { primary: (active.value as unknown[])[0] };
    let saved: SettingsDraft | undefined;
    await page.route('**/api/settings/apply', async (route) => {
      saved = route.request().postDataJSON();
      await route.fulfill({ json: { saved: true } });
    });

    await page.goto('/settings?tab=crawler');
    const collection = page.getByRole('group', { name: field.label, exact: true });
    const row = collection.getByRole('group', { name: 'Custom entries 1', exact: true });
    await expect(
      row.getByText(type === 'dictionary' ? 'primary' : 'Custom entries 1', { exact: true }),
    ).toBeVisible();
    await expect(row).toContainText('Display name: session');
    await expect(row).toContainText('Host: example.com');
    await expect(row).toContainText('Value: Value stored');
    await expect(row).not.toContainText('cookie-reference');
    await expect(collection.getByRole('button', { name: 'Add item', exact: true })).toBeVisible();

    await row.getByRole('button', { name: 'Edit', exact: true }).click();
    await row.getByRole('textbox', { name: 'Display name', exact: true }).fill('Updated entry');
    await row.getByRole('combobox', { name: 'Value action', exact: true }).selectOption('replace');
    await row.getByLabel('Value replacement', { exact: true }).fill('private-generic-draft');
    await row.getByRole('button', { name: 'Done', exact: true }).click();
    await expect(row).toContainText('Display name: Updated entry');
    await expect(row).toContainText('Value: New value');
    await expect(row).not.toContainText('private-generic-draft');
    await row.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(row.getByLabel('Value replacement', { exact: true })).toHaveValue(
      'private-generic-draft',
    );
    await row.getByRole('button', { name: 'Cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Save and apply', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Settings saved and applied');
    const item = {
      name: 'Updated entry',
      domain: 'example.com',
      path: '/',
      value: { action: 'replace', ref: 'cookie-reference', value: 'private-generic-draft' },
    };
    expect(saved?.edits).toEqual([
      {
        path: field.path,
        action: 'set',
        value: type === 'dictionary' ? { primary: item } : [item],
      },
    ]);
  });
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

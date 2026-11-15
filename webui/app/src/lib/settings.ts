// SPDX-License-Identifier: AGPL-3.0-or-later
import { apiFetch } from '#lib/api.js';

export interface SettingSchema {
  type: string;
  control: string;
  collection: string;
  sensitivity: string;
  min?: number;
  max?: number;
  max_length?: number;
  min_length?: number;
  max_items?: number;
  choices?: string[];
  choice_labels?: Record<string, string>;
  key_label?: string;
  value_label?: string;
  secret_scope?: string[];
  items?: SettingSchema;
  properties?: {
    name: string;
    label: string;
    description?: string;
    required?: boolean;
    schema: SettingSchema;
  }[];
}

export interface SettingField {
  path: string;
  category: string;
  group?: string;
  label: string;
  description: string;
  schema: SettingSchema;
  default?: unknown;
  effect: string;
  read_only_reason?: string;
  requires_confirmation: boolean;
}

// The open item is separate from the page draft until Add or Done succeeds.
export interface SettingItemEditor {
  path: string;
  index: number | null;
  name: string;
  value: unknown;
}

export interface SettingRemoval {
  path: string;
  value: unknown;
}

export function emptySettingValue(schema: SettingSchema): unknown {
  if (schema.sensitivity === 'secret' || schema.sensitivity === 'url') return { action: 'clear' };
  if (schema.type === 'object')
    return Object.fromEntries(
      (schema.properties ?? []).map((property) => [
        property.name,
        emptySettingValue(property.schema),
      ]),
    );
  if (schema.type === 'list') return [];
  if (schema.type === 'dictionary') return {};
  if (schema.type === 'boolean') return false;
  if (schema.type === 'integer' || schema.type === 'number') return schema.min ?? 0;
  return '';
}

export function choiceLabel(schema: SettingSchema, value: string): string {
  return schema.choice_labels?.[value] ?? value;
}

export interface SettingValue {
  path: string;
  value?: unknown;
  editable: boolean;
  reason?: string;
  secret_present: boolean;
  source: {
    kind: string;
    name?: string;
    locked: boolean;
    reason?: string;
    web_selected: boolean;
  };
}

export interface SettingsState {
  revision: string;
  fields: SettingField[];
  values: SettingValue[];
  pending_access?: PendingAccess | null;
  pending?: {
    status: 'changed' | 'invalid' | 'unreadable';
    values?: SettingValue[];
  } | null;
}

export interface PendingAccess {
  revision: string;
  deadline: string;
  destination_url: string;
  confirmation_token?: string;
  authentication?: 'credentials' | 'password' | 'oauth';
  oauth_provider?: string;
}

export interface SettingsCompletion {
  revision: string;
  confirmation_token?: string;
}

export function sameSetting(a?: SettingValue, b?: SettingValue): boolean {
  return (
    !!a &&
    !!b &&
    JSON.stringify(a.value) === JSON.stringify(b.value) &&
    a.secret_present === b.secret_present &&
    a.editable === b.editable &&
    a.source.kind === b.source.kind &&
    a.source.name === b.source.name &&
    a.source.locked === b.source.locked &&
    a.source.web_selected === b.source.web_selected
  );
}

export async function reloadConfiguration(): Promise<void> {
  const response = await apiFetch('/reload', { method: 'POST', redirectOnForbidden: false });
  if (!response.ok) {
    throw new SettingsError(
      response.status,
      'reload_failed',
      'Could not reload configuration. Check the configuration file and server logs, then refresh settings.',
    );
  }
}

export type SettingEdit =
  { path: string; action: 'set'; value: unknown } | { path: string; action: 'reset' };
export interface SettingsDraft {
  revision: string;
  edits: SettingEdit[];
  acknowledgment?: string;
}

// Contract for sensitive validation: the server must supply redacted changes
// and bind acknowledgments to the actor, revision and complete candidate.
export interface SettingsReview {
  mode?: 'acknowledge' | 'access';
  acknowledgment: string;
  changes: { path: string; before: string; after: string }[];
  consequences: string[];
  destination_url?: string;
}

interface SettingsValidation {
  revision: string;
  values: SettingValue[];
  review?: SettingsReview;
  requires_confirmation?: boolean;
}

export interface SettingError {
  path: string;
  code: string;
  message: string;
}

export class SettingsError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields: SettingError[] = [],
  ) {
    super(message);
  }
}

export async function settingsRequest<T>(
  operation = '',
  draft?: SettingsDraft | SettingsCompletion,
): Promise<T> {
  const response = await apiFetch(`/settings${operation}`, {
    redirectOnForbidden: false,
    cache: 'no-store',
    ...(draft
      ? {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(draft),
        }
      : {}),
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new SettingsError(
      response.status,
      body.code ?? '',
      body.error ?? 'Could not complete the settings request.',
      body.errors ?? [],
    );
  }
  return body as T;
}

export async function readSettingsReview(
  draft: SettingsDraft,
  fields: SettingField[],
): Promise<SettingsReview | undefined> {
  const validation = await settingsRequest<SettingsValidation>('/validate', draft);
  if (validation.revision !== draft.revision)
    throw new SettingsError(
      409,
      'stale_revision',
      'The server validated a different settings revision. No changes were applied.',
    );
  if (validation.review) {
    requireCompleteReview(validation.review, validation.revision === draft.revision);
    return validation.review;
  }
  if (validation.requires_confirmation === false) return;
  if (
    validation.requires_confirmation === true ||
    draft.edits.some((edit) =>
      fields.some((field) => field.path === edit.path && field.requires_confirmation),
    )
  )
    throw new SettingsError(
      422,
      'review_required',
      'The server did not provide a sensitive change review. No changes were applied.',
    );
}

function requireCompleteReview(review: SettingsReview, currentRevision: boolean) {
  if (
    ![
      currentRevision,
      review.acknowledgment,
      review.changes?.length,
      review.consequences?.length,
    ].every(Boolean)
  )
    throw new SettingsError(
      422,
      'invalid_review',
      'The server did not provide a complete review. No changes were applied.',
    );
}

export const categoryLabels: Record<string, string> = {
  app: 'App',
  server: 'Server',
  indexer: 'Indexer',
  crawler: 'Crawler',
  semantic_search: 'Semantic search',
  extractors: 'Extractors',
  hotkeys: 'Hotkeys',
  sensitive_content_patterns: 'Sensitive content patterns',
};

export function groupSettings(fields: SettingField[], category: string) {
  const groups = new Map<string, SettingField[]>();
  const ungrouped: SettingField[] = [];
  for (const field of fields) {
    if (field.category !== category) continue;
    if (!field.group) {
      ungrouped.push(field);
      continue;
    }
    const members = groups.get(field.group) ?? [];
    members.push(field);
    groups.set(field.group, members);
  }
  const result = [...groups].map(([label, fields]) => ({ label, fields }));
  if (ungrouped.length) {
    result.push({ label: groups.size ? 'Other settings' : '', fields: ungrouped });
  }
  return result;
}

export function sourceLabel(source: SettingValue['source']): string {
  return (
    (
      {
        default: 'Built in default',
        web: 'Web selection',
        file: 'Main configuration',
        environment: 'Environment',
        flag: 'Startup flag',
        mixed: 'Multiple sources',
      } as Record<string, string>
    )[source.kind] ?? source.kind
  );
}

export function canEditSetting(field: SettingField, value: SettingValue): boolean {
  return (
    value.editable &&
    !value.source.locked &&
    !field.read_only_reason &&
    supportsControl(field.schema)
  );
}

function supportsControl(schema: SettingSchema): boolean {
  if (!['none', 'secret', 'url', 'mixed'].includes(schema.sensitivity)) return false;
  if (schema.type === 'list' || schema.type === 'dictionary')
    return (
      schema.control === schema.type &&
      schema.collection === 'replace' &&
      !!schema.items &&
      supportsControl(schema.items)
    );
  if (schema.type === 'object')
    return (
      schema.control === 'object' &&
      ['none', 'replace'].includes(schema.collection) &&
      !!schema.properties?.length &&
      schema.properties.every((property) => supportsControl(property.schema))
    );
  return (
    schema.collection === 'none' &&
    ['text', 'number', 'select', 'switch', 'secret', 'url'].includes(schema.control)
  );
}

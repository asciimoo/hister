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
  max_items?: number;
  choices?: string[];
  choice_labels?: Record<string, string>;
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
}

export type SettingEdit =
  { path: string; action: 'set'; value: unknown } | { path: string; action: 'reset' };
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
  draft?: { revision: string; edits: SettingEdit[] },
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
    !field.requires_confirmation &&
    field.schema.sensitivity === 'none' &&
    supportsControl(field.schema)
  );
}

function supportsControl(schema: SettingSchema): boolean {
  if (schema.sensitivity !== 'none') return false;
  if (schema.type === 'list')
    return (
      schema.control === 'list' &&
      schema.collection === 'replace' &&
      !!schema.items &&
      supportsControl(schema.items)
    );
  if (schema.type === 'object')
    return (
      schema.control === 'object' &&
      schema.collection === 'none' &&
      !!schema.properties?.length &&
      schema.properties.every((property) => supportsControl(property.schema))
    );
  return (
    schema.collection === 'none' && ['text', 'number', 'select', 'switch'].includes(schema.control)
  );
}

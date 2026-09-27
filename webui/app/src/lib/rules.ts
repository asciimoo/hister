// SPDX-License-Identifier: AGPL-3.0-or-later

import { apiFetch } from '$lib/api';

export type RuleType = 'allow' | 'skip' | 'priority' | 'versioning';
export type RuleMatchMode = 'domain' | 'url' | 'regex';

interface RuleLists {
  allow: string[];
  skip: string[];
  priority: string[];
  versioning: string[];
}

export interface RulesData extends RuleLists {
  aliases: Record<string, string>;
}

export interface RuleDraft {
  pattern?: string;
  domain?: string;
  exact_url?: string;
  include_subdomains?: boolean;
  url?: string;
}

export interface RulePreview {
  pattern: string;
  matches?: boolean;
}

export async function previewRule(draft: RuleDraft, signal?: AbortSignal): Promise<RulePreview> {
  const response = await apiFetch('/rules/preview', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(draft),
    signal,
  });
  if (!response.ok) throw await responseError(response, 'Failed to check rule');
  return response.json();
}

async function responseError(response: Response, fallback: string): Promise<Error> {
  const body = await response.text();
  return new Error(body.trim() || fallback);
}

function orDefault<T>(value: T | null | undefined, fallback: T): T {
  return value ?? fallback;
}

export async function fetchRules(): Promise<RulesData> {
  const response = await apiFetch('/rules', { headers: { Accept: 'application/json' } });
  if (!response.ok) throw await responseError(response, 'Failed to load rules');
  const data = await response.json();
  return {
    allow: orDefault(data.allow, []),
    skip: orDefault(data.skip, []),
    priority: orDefault(data.priority, []),
    versioning: orDefault(data.versioning, []),
    aliases: orDefault(data.aliases, {}),
  };
}

export async function saveRuleLists(rules: RuleLists): Promise<void> {
  const formData = new URLSearchParams();
  formData.set('allow', rules.allow.join('\n'));
  formData.set('skip', rules.skip.join('\n'));
  formData.set('priority', rules.priority.join('\n'));
  formData.set('versioning', rules.versioning.join('\n'));
  const response = await apiFetch('/rules', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: formData.toString(),
  });
  if (!response.ok) throw await responseError(response, 'Failed to save rules');
}
